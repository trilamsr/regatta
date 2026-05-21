package schemas

import _ "embed"

// WorkItemSchemaJSON is the canonical JSON-Schema document for a WorkItem,
// embedded at build time. Validators must consume this string rather than
// re-reading the file at runtime so a deployed binary cannot drift from the
// schema it was built against.
//
//go:embed work_item.schema.json
var WorkItemSchemaJSON string
