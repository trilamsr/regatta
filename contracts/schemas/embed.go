package schemas

import _ "embed"

// RegattaV1CUE is the canonical CUE schema for regatta.yaml v1,
// embedded so internal/config/validate can compile it without a
// filesystem dependency at runtime.
//
//go:embed regatta.v1.cue
var RegattaV1CUE string
