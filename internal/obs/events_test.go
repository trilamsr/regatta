package obs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
)

// Reflection walks the obs package so adding an EventName / AttrKey
// constant automatically enrolls it in the schema gate. A new constant
// that fails the format check fails compile-time-adjacent tests; no
// hand-maintained registry to drift.

// enumerateEventNames returns every exported obs.EventName-typed
// constant declared at package level. New events that do not appear
// here mean the test file is being run against a stale obs build.
func enumerateEventNames(t *testing.T) map[string]obs.EventName {
	t.Helper()
	got := make(map[string]obs.EventName)
	rv := reflect.ValueOf(obs.AllEventNames())
	if rv.Kind() != reflect.Slice {
		t.Fatalf("obs.AllEventNames() must return a slice; got %s", rv.Kind())
	}
	for i := 0; i < rv.Len(); i++ {
		name, ok := rv.Index(i).Interface().(obs.EventName)
		if !ok {
			t.Fatalf("obs.AllEventNames()[%d] is not EventName; got %T", i, rv.Index(i).Interface())
		}
		got[string(name)] = name
	}
	return got
}

func enumerateAttrKeys(t *testing.T) map[string]obs.AttrKey {
	t.Helper()
	got := make(map[string]obs.AttrKey)
	rv := reflect.ValueOf(obs.AllAttrKeys())
	if rv.Kind() != reflect.Slice {
		t.Fatalf("obs.AllAttrKeys() must return a slice; got %s", rv.Kind())
	}
	for i := 0; i < rv.Len(); i++ {
		k, ok := rv.Index(i).Interface().(obs.AttrKey)
		if !ok {
			t.Fatalf("obs.AllAttrKeys()[%d] is not AttrKey; got %T", i, rv.Index(i).Interface())
		}
		got[string(k)] = k
	}
	return got
}

// FROZEN: do not add new entries — use the dotted component.event
// format for new event kinds. This allowlist captures the 17 legacy
// underscore-form kinds whose wire strings are pinned on-disk + in
// dashboards + selfimprove rules; changing them would silently break
// historical event queries.
//
// legacyUnderscoreEventNames are pre-existing wire strings emitted by
// subsystem packages (merge, rejectionrouter, cost/cap, reaper,
// prwatch, etc.) before the dotted convention was adopted. They
// landed in obs.AllEventNames so DB.RecordEvent can fail-closed on
// typos (Bug S4) without breaking dashboards / runbooks / on-disk
// events that already grep for the underscored form. New events MUST
// use the dotted component.event format.
var legacyUnderscoreEventNames = map[string]struct{}{
	"cost_cap_throttled":    {},
	"cost_cap_resumed":      {},
	"merge_intent":          {},
	"merge_completed":       {},
	"merge_executed":        {},
	"merge_failed":          {},
	"merge_recovered":       {},
	"gate_rejected":         {},
	"escalated":             {},
	"labeled":               {},
	"recovered_crashed":     {},
	"reaped":                {},
	"secrets_rotated":       {},
	"agent_pr_opened":       {},
	"agent_pr_head_changed": {},
	"agent_branch_renamed":  {},
	"agent_pr_dirty":        {},
}

// TestLegacyUnderscoreEventNames_Frozen pins the legacy allowlist size; new entries fail CI.
func TestLegacyUnderscoreEventNames_Frozen(t *testing.T) {
	if got, want := len(legacyUnderscoreEventNames), 17; got != want {
		t.Fatalf("legacyUnderscoreEventNames is FROZEN; got %d entries, want %d — new events must use dotted component.event format", got, want)
	}
}

func TestEventName_AllNonEmpty(t *testing.T) {
	events := enumerateEventNames(t)
	if len(events) == 0 {
		t.Fatalf("no EventName constants enumerated; obs package empty")
	}
	for s := range events {
		if s == "" {
			t.Errorf("EventName constant has empty value")
			continue
		}
		if _, legacy := legacyUnderscoreEventNames[s]; legacy {
			continue
		}
		// Dotted format: every event is component.event so operators
		// can grep by component prefix without enumerating events.
		if !strings.Contains(s, ".") {
			t.Errorf("EventName %q missing dotted component.event format", s)
		}
		parts := strings.Split(s, ".")
		if len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
			t.Errorf("EventName %q malformed; want non-empty component and event halves", s)
		}
	}
}

func TestAttrKey_AllNonEmpty(t *testing.T) {
	keys := enumerateAttrKeys(t)
	if len(keys) == 0 {
		t.Fatalf("no AttrKey constants enumerated; obs package empty")
	}
	for s := range keys {
		if s == "" {
			t.Errorf("AttrKey constant has empty value")
			continue
		}
		// snake_case lowercase keys keep operator greps consistent
		// with the slog JSON wire shape documented in spec §3.2.
		if strings.ToLower(s) != s {
			t.Errorf("AttrKey %q not lowercase", s)
		}
		if strings.ContainsAny(s, " \t\n") {
			t.Errorf("AttrKey %q contains whitespace", s)
		}
	}
}

func TestEventName_NoDuplicates(t *testing.T) {
	seen := make(map[string]int)
	for _, name := range obs.AllEventNames() {
		seen[string(name)]++
	}
	for s, n := range seen {
		if n > 1 {
			t.Errorf("EventName %q declared %d times", s, n)
		}
	}
}

func TestAttrKey_NoDuplicates(t *testing.T) {
	seen := make(map[string]int)
	for _, k := range obs.AllAttrKeys() {
		seen[string(k)]++
	}
	for s, n := range seen {
		if n > 1 {
			t.Errorf("AttrKey %q declared %d times", s, n)
		}
	}
}

// Spec §3.1 enumerates the canonical event vocabulary. Any reshuffle
// of constant names that drops a wire value fails this test before
// silently breaking dashboards that grep on the JSON event field.
func TestEventName_SpecVocabulary(t *testing.T) {
	want := []string{
		// Orchestrator
		"tick.started", "tick.completed",
		// Scheduler edge eval
		"edge.fired", "edge.skipped", "edge.default_fallback",
		"edge.eval_error", "edge.mark_failed",
		"edge.eval_skipped_no_journal", "edge.journal_load_failed",
		"edge.multiple_defaults_per_from",
		// Scheduler materialization
		"scheduler.materialize_failure",
		// Spawner
		"spawn.started", "spawn.completed", "spawn.failed",
		// Reaper
		"reap.candidate_detected", "reap.killed", "reap.skipped",
		// Gates
		"gate.verdict",
		// Brief loader
		"brief.loaded", "brief.rejected",
		"brief.materialise_failed", "brief.edges_materialised",
		// Adapter sync
		"adaptersync.synced", "adaptersync.failed",
		// Planner
		"planner.prompt_fallback",
	}
	got := enumerateEventNames(t)
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("spec §3.1 event %q missing from obs.EventName universe", w)
		}
	}
}

// Spec §3.3 closes the top-level attribute key set. New keys land in
// attrs.* — this test fails when a callsite tries to introduce a new
// top-level key without first declaring it as an AttrKey constant.
func TestAttrKey_SpecVocabulary(t *testing.T) {
	want := []string{
		"work_item_id", "agent_id", "lane",
		"event", "duration_ms", "timestamp",
		"program_id", "from_id", "to_id", "edge_id", "journal_sha",
		"gate_id", "verdict", "reason",
		"err",
		"work_items_evaluated",
	}
	got := enumerateAttrKeys(t)
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("spec §3.3 attr key %q missing from obs.AttrKey universe", w)
		}
	}
}
