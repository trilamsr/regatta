package adapter

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// parseMarkdownItem extracts a WorkItem from a single markdown file.
// The format is documented on MarkdownCatalogConfig.
func parseMarkdownItem(data []byte) (schemas.WorkItem, error) {
	body, fm, err := splitFrontmatter(data)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	var item schemas.WorkItem
	if v, ok := fm["id"]; ok {
		item.ID = schemas.WorkItemID(v)
	}
	if v, ok := fm["title"]; ok {
		item.Title = v
	}
	if v, ok := fm["lane"]; ok {
		item.Lane = schemas.LaneID(v)
	}
	if v, ok := fm["status"]; ok {
		item.Status = schemas.Status(v)
	} else {
		item.Status = schemas.StatusPlanned
	}
	if v, ok := fm["dependencies"]; ok {
		for _, dep := range splitList(v) {
			item.Dependencies = append(item.Dependencies, schemas.WorkItemID(dep))
		}
	}
	if v, ok := fm["linked_artifact"]; ok {
		item.LinkedArtifact = v
	}
	if item.ID == "" {
		return schemas.WorkItem{}, fmt.Errorf("markdown_catalog: frontmatter missing required `id`")
	}
	if item.Title == "" {
		return schemas.WorkItem{}, fmt.Errorf("markdown_catalog: frontmatter missing required `title`")
	}
	if !validStatus(item.Status) {
		return schemas.WorkItem{}, fmt.Errorf("markdown_catalog: invalid status %q", item.Status)
	}

	criteria, body, err := parseCriteria(body)
	if err != nil {
		return schemas.WorkItem{}, err
	}
	if len(criteria) == 0 {
		return schemas.WorkItem{}, fmt.Errorf("markdown_catalog: item %s has no acceptance criteria", item.ID)
	}
	item.AcceptanceCriteria = criteria
	item.Body = strings.TrimSpace(body)
	return item, nil
}

// Per-criterion citations (work_item.schema.json:Criterion.citation)
// are not yet round-tripped by this parser. The schema permits a
// citation on each criterion, but the markdown_catalog adapter
// currently exposes only top-level frontmatter `citation:` for the
// whole item. Per-criterion citation lands when PRWatcher writes
// done-transitions; until then UpdateStatus's citation argument
// applies to the item as a whole. Anchor: schemas/work_item.schema.json
// `acceptance_criteria[*].citation`.
var (
	frontmatterRE  = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)
	criterionRE    = regexp.MustCompile(`^\s*-\s*\[(planned|in_progress|done)\]\s+([^\s:]+):\s*(.+?)\s*$`)
	criteriaHeadRE = regexp.MustCompile(`(?i)^##\s+acceptance\s+criteria\s*$`)
)

func splitFrontmatter(data []byte) (body string, fm map[string]string, err error) {
	loc := frontmatterRE.FindSubmatchIndex(data)
	if loc == nil {
		return "", nil, fmt.Errorf("markdown_catalog: missing frontmatter")
	}
	raw := string(data[loc[2]:loc[3]])
	body = string(data[loc[1]:])
	fm = map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return "", nil, fmt.Errorf("markdown_catalog: malformed frontmatter line: %q", line)
		}
		fm[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return body, fm, nil
}

func parseCriteria(body string) ([]schemas.Criterion, string, error) {
	var out []schemas.Criterion
	lines := strings.Split(body, "\n")
	inSection := false
	seen := map[string]bool{}
	var rest bytes.Buffer
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if criteriaHeadRE.MatchString(line) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(strings.TrimSpace(line), "## ") {
			inSection = false
		}
		if !inSection {
			rest.WriteString(line)
			rest.WriteByte('\n')
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := criterionRE.FindStringSubmatch(line)
		if m == nil {
			return nil, "", fmt.Errorf("markdown_catalog: malformed criterion line: %q", line)
		}
		state := schemas.CriterionState(m[1])
		id := m[2]
		text := m[3]
		if seen[id] {
			return nil, "", fmt.Errorf("markdown_catalog: duplicate criterion id %q", id)
		}
		seen[id] = true
		out = append(out, schemas.Criterion{ID: id, Text: text, State: state})
	}
	return out, rest.String(), nil
}

func rewriteStatus(data []byte, status, citation string) ([]byte, bool) {
	loc := frontmatterRE.FindSubmatchIndex(data)
	if loc == nil {
		return data, false
	}
	head := string(data[loc[2]:loc[3]])
	tail := string(data[loc[1]:])

	changed := false
	lines := strings.Split(head, "\n")
	hasStatus := false
	hasCitation := false
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		key, _, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "status":
			hasStatus = true
			next := fmt.Sprintf("status: %s", status)
			if trimmed != next {
				lines[i] = next
				changed = true
			}
		case "citation":
			hasCitation = true
			if citation == "" {
				lines[i] = ""
				changed = true
			} else {
				next := fmt.Sprintf("citation: %s", citation)
				if trimmed != next {
					lines[i] = next
					changed = true
				}
			}
		}
	}
	if !hasStatus {
		lines = append(lines, fmt.Sprintf("status: %s", status))
		changed = true
	}
	if citation != "" && !hasCitation {
		lines = append(lines, fmt.Sprintf("citation: %s", citation))
		changed = true
	}
	if !changed {
		return data, false
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteString("---\n")
	out.WriteString(tail)
	return out.Bytes(), true
}

func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validStatus(s schemas.Status) bool {
	switch s {
	case schemas.StatusPlanned, schemas.StatusInProgress, schemas.StatusDone:
		return true
	}
	return false
}
