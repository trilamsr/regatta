package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrCycleDetected fires when a CycleCheck would introduce a cycle
// in work_items.depends_on_features.
var ErrCycleDetected = errors.New("state: dependency cycle detected in work_items")

// selectWorkItemsCols is the canonical column list for unaliased
// work_items SELECTs feeding scanWorkItems. Centralising the column
// order keeps GetWorkItem and ListByParent in lockstep with the
// scanner. ListSpawnable joins on agents and uses its own aliased
// column list; the scan order must still match this constant.
const selectWorkItemsCols = `SELECT id, kind, title, lane, status,
	COALESCE(parent_program_id, ''), depends_on_features,
	acceptance_json, source, last_seen_at, created_at, updated_at`

// ListByParent returns every work_items row whose parent_program_id
// equals parentID, in id order.
func (d *DB) ListByParent(ctx context.Context, parentID string) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectWorkItemsCols+` FROM work_items WHERE parent_program_id = ? ORDER BY id`, parentID)
	if err != nil {
		return nil, fmt.Errorf("state: list by parent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// ListSpawnable returns every work_items row whose status is
// 'planned', that has no entry in agents yet, whose legacy
// depends_on_features are either empty or all already 'merged', and
// whose inbound work_item_edges (if any) are satisfied. per spec §2.8
// — the SELECT here is the materialization-eliminator: scheduler.Tick
// consumes the rows directly into the reservation tx.
//
// Edge satisfaction (W4-A): a planned row qualifies when any of:
//   - no inbound work_item_edges at all (v1 fast-path, unchanged);
//   - at least one inbound edge has fired='true';
//   - every inbound edge resolved (no 'pending') AND at least one
//     inbound edge carries on_skip='ignore' — the diamond-join /
//     default-next escape hatch so a downstream node still spawns
//     when every gating predicate evaluated false.
//
// Note: relies on the work_items.depends_on_features NOT NULL
// schema invariant. If a future migration allows NULL, the
// `NOT EXISTS(json_each(NULL))` clause silently treats the row
// as spawnable. Migration authors must add an `IS NOT NULL`
// guard here if they relax the column.
func (d *DB) ListSpawnable(ctx context.Context) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT w.id, w.kind, w.title, w.lane, w.status,
		       COALESCE(w.parent_program_id, ''), w.depends_on_features,
		       w.acceptance_json, w.source, w.last_seen_at,
		       w.created_at, w.updated_at
		FROM work_items w
		LEFT JOIN agents a ON w.id = a.work_item_id
		WHERE w.status = 'planned'
		  AND a.id IS NULL
		  AND (
		    w.depends_on_features = '[]'
		    OR NOT EXISTS (
		      SELECT 1 FROM json_each(w.depends_on_features)
		      WHERE value NOT IN (SELECT id FROM work_items WHERE status = 'merged')
		    )
		  )
		  AND (
		    NOT EXISTS (SELECT 1 FROM work_item_edges WHERE to_id = w.id)
		    OR EXISTS (
		      SELECT 1 FROM work_item_edges
		      WHERE to_id = w.id AND fired = 'true'
		    )
		    OR (
		      NOT EXISTS (
		        SELECT 1 FROM work_item_edges
		        WHERE to_id = w.id AND fired = 'pending'
		      )
		      AND EXISTS (
		        SELECT 1 FROM work_item_edges
		        WHERE to_id = w.id AND on_skip = 'ignore'
		      )
		    )
		  )
		ORDER BY w.id`)
	if err != nil {
		return nil, fmt.Errorf("state: list spawnable: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanWorkItems(rows)
}

// CycleCheck verifies that inserting (or updating) candidate would
// not introduce a dependency cycle. Loads the full work_items
// dependency graph, overlays candidate's depends_on_features, and
// runs Kahn's topological sort: if any node remains undrained, a
// cycle exists. Complexity is O(n + m) across n work_items and m
// edges — the prior DFS-from-candidate impl built a string-keyed
// adjacency map then walked it recursively; the int-indexed CSR
// layout below keeps every hot lookup in a flat slice.
//
// The dep-list JSON is parsed with a hand-rolled scanner instead of
// json.Unmarshal because Unmarshal allocates one new string per
// dep, and 50k-edge graphs paid 50k+ string allocs that dwarfed the
// algorithm itself. The scanner reuses the SQL row's byte buffer
// and looks each dep ID up in idx via `map[string(s)]` — the Go
// compiler turns that byte→string conversion into an
// allocation-free lookup.
//
// Semantic note vs the pre-Kahn implementation: the old DFS started
// at candidate and only flagged cycles reachable from candidate.
// Kahn's scans the whole graph, so it would additionally fail when
// the DB already contained a candidate-disjoint cycle. That case is
// impossible in practice because every insert path runs CycleCheck
// first — the invariant "DB is acyclic" is maintained inductively.
// For any input that satisfies the invariant the boolean result is
// identical. Self-loop is caught because the looping node's indegree
// never reaches zero.
func (d *DB) CycleCheck(ctx context.Context, candidate WorkItem) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, depends_on_features FROM work_items`)
	if err != nil {
		return fmt.Errorf("state: cycle scan: %w", err)
	}
	// First pass: assign every known ID a dense int index and stash
	// the raw deps JSON for the second pass. We can't resolve deps
	// to indices yet because depends_on_features can reference IDs
	// that appear later in the row stream.
	idx := map[string]int{}
	var depsRaw [][]byte
	addNode := func(id string) int {
		if i, ok := idx[id]; ok {
			return i
		}
		i := len(depsRaw)
		idx[id] = i
		depsRaw = append(depsRaw, nil)
		return i
	}
	for rows.Next() {
		var id string
		var deps []byte
		if err := rows.Scan(&id, &deps); err != nil {
			_ = rows.Close()
			return fmt.Errorf("state: cycle scan row: %w", err)
		}
		i := addNode(id)
		// Overlay: when this row IS candidate, drop the persisted
		// deps — candidate.DependsOnFeatures drives validation. nil
		// here signals "use the candidate slice in the second pass".
		if id == candidate.ID {
			depsRaw[i] = nil
		} else {
			// Copy because Scan reuses the row buffer on Next().
			depsRaw[i] = append([]byte(nil), deps...)
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("state: cycle close rows: %w", err)
	}
	// Candidate may be a brand-new ID not yet in the DB.
	candIdx := addNode(candidate.ID)

	n := len(depsRaw)
	indeg := make([]int, n)
	// revHead is a CSR offset array: reverse-edge list of node i
	// lives in revEdges[revHead[i]:revHead[i+1]]. One flat backing
	// slice beats a [][]int because Kahn's hot loop walks it
	// linearly with no per-node bounds check on the outer slice.
	revHead := make([]int, n+1)

	// visitDeps invokes f(depIdx) for every dep of node i that
	// resolves to a known index. Candidate's deps come from the
	// in-memory overlay; everyone else from the cached JSON bytes.
	visitDeps := func(i int, f func(depIdx int)) error {
		if i == candIdx {
			for _, dep := range candidate.DependsOnFeatures {
				if depI, ok := idx[dep]; ok {
					f(depI)
				}
			}
			return nil
		}
		return scanJSONStrings(depsRaw[i], func(s []byte) {
			if depI, ok := idx[string(s)]; ok {
				f(depI)
			}
		})
	}

	// First sweep: indegree counts + per-target reverse-edge counts.
	for i := 0; i < n; i++ {
		if err := visitDeps(i, func(depIdx int) {
			indeg[i]++
			revHead[depIdx+1]++
		}); err != nil {
			return fmt.Errorf("state: cycle decode deps: %w", err)
		}
	}
	for i := 1; i <= n; i++ {
		revHead[i] += revHead[i-1]
	}
	revEdges := make([]int, revHead[n])
	cursor := make([]int, n)
	for i := 0; i < n; i++ {
		// Error path already covered in the first sweep.
		_ = visitDeps(i, func(depIdx int) {
			revEdges[revHead[depIdx]+cursor[depIdx]] = i
			cursor[depIdx]++
		})
	}

	// Kahn's main loop: seed every zero-indegree node, drain its
	// successors. len(queue) at end equals the count of nodes
	// drained, so a shortfall is a cycle.
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, succ := range revEdges[revHead[queue[head]]:revHead[queue[head]+1]] {
			indeg[succ]--
			if indeg[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}
	if len(queue) != n {
		return fmt.Errorf("%w: %s", ErrCycleDetected, candidate.ID)
	}
	return nil
}

// scanJSONStrings walks a JSON array of strings (the shape
// depends_on_features always carries) and invokes f on each unquoted
// element. Avoids json.Unmarshal's per-element string allocation —
// the byte slice handed to f aliases the input buffer and must not
// be retained past f's return. Supports the standard JSON escapes
// (\", \\) that the upstream marshaler might emit; non-string array
// elements return an error.
func scanJSONStrings(raw []byte, f func(s []byte)) error {
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) || raw[i] != '[' {
		// Empty deps store as '[]'; treat anything malformed as a
		// hard error rather than silently dropping edges.
		if i >= len(raw) {
			return nil
		}
		return fmt.Errorf("expected '[', got %q", raw[i])
	}
	i++
	for i < len(raw) {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == ',') {
			i++
		}
		if i >= len(raw) || raw[i] == ']' {
			return nil
		}
		if raw[i] != '"' {
			return fmt.Errorf("expected '\"', got %q at offset %d", raw[i], i)
		}
		i++
		start := i
		hasEscape := false
		for i < len(raw) && raw[i] != '"' {
			if raw[i] == '\\' {
				hasEscape = true
				i += 2
				continue
			}
			i++
		}
		if i >= len(raw) {
			return fmt.Errorf("unterminated string")
		}
		if !hasEscape {
			f(raw[start:i])
		} else {
			// Escapes are rare in our IDs (F-uuid charset); fall
			// back to a small allocation on this path.
			unesc := unescapeJSON(raw[start:i])
			f(unesc)
		}
		i++
	}
	return nil
}

// unescapeJSON handles the two escapes our marshaler can emit for
// dep IDs (\" and \\). Anything else is passed through verbatim —
// IDs in our system never contain unicode escapes.
func unescapeJSON(s []byte) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			out = append(out, s[i+1])
			i++
			continue
		}
		out = append(out, s[i])
	}
	return out
}

// MaxUpdatedAtForBriefChildren returns the largest updated_at across
// all work_items whose parent_program_id == parentID and source ==
// SourceBrief, or the zero time if no such row exists. Used by
// BriefLoader to reject stale brief replays: if a freshly-loaded
// brief's ProducedAt is <= this watermark, the brief was already
// processed (or superseded by a newer one for the same program) and
// re-applying it would silently revert later state. Read-only.
func (d *DB) MaxUpdatedAtForBriefChildren(ctx context.Context, parentID string) (time.Time, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(updated_at), 0) FROM work_items
		WHERE parent_program_id = ? AND source = ?`,
		parentID, string(SourceBrief))
	var ts int64
	if err := row.Scan(&ts); err != nil {
		return time.Time{}, fmt.Errorf("state: max updated_at for brief children of %s: %w", parentID, err)
	}
	if ts == 0 {
		return time.Time{}, nil
	}
	return time.Unix(ts, 0).UTC(), nil
}

// ListArchivedProgramsWithLiveChildren returns the IDs of every
// program whose own row is archived but which still has at least one
// non-archived child via parent_program_id. AdapterSync's reconciler
// calls this every tick to converge stranded children when a prior
// tick crashed between the program-archive write and the child
// cascade. Idempotent — once no live children remain, returns empty.
func (d *DB) ListArchivedProgramsWithLiveChildren(ctx context.Context) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT DISTINCT p.id
		FROM work_items p
		JOIN work_items c ON c.parent_program_id = p.id
		WHERE p.kind = ? AND p.status = ? AND c.status != ?
		ORDER BY p.id`,
		string(KindProgram), string(WorkStatusArchived), string(WorkStatusArchived))
	if err != nil {
		return nil, fmt.Errorf("state: list archived programs w/ live children: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("state: scan archived program id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanWorkItems(rows *sql.Rows) ([]WorkItem, error) {
	var out []WorkItem
	for rows.Next() {
		var w WorkItem
		var depsJSON string
		var lastSeen, created, updated int64
		if err := rows.Scan(&w.ID, &w.Kind, &w.Title, &w.Lane, &w.Status,
			&w.ParentProgramID, &depsJSON, &w.AcceptanceJSON, &w.Source,
			&lastSeen, &created, &updated); err != nil {
			return nil, fmt.Errorf("state: scan work_items: %w", err)
		}
		if err := json.Unmarshal([]byte(depsJSON), &w.DependsOnFeatures); err != nil {
			return nil, fmt.Errorf("state: decode deps for %s: %w", w.ID, err)
		}
		w.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		w.CreatedAt = time.Unix(created, 0).UTC()
		w.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, w)
	}
	return out, rows.Err()
}
