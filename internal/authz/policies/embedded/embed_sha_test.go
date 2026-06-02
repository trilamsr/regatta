package embedded_test

// defaultBundleSHA256Expected pins R11 — any change to the *.rego files in
// regatta/v1/default/ MUST update this constant. The drift assertion is the
// single human-visible signal that authz outcomes changed.
//
// Recompute: run TestDefaultBundleSHA256_Stable and paste the "got" hex.
const defaultBundleSHA256Expected = "4f03e755a903b87857b0abf02b702d0b7dcc0d1fb1eaf3896d3b5ab3a4e7a87b"
