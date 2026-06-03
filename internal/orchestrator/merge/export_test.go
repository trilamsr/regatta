package merge

// ClassifyStderrForTest exposes the internal stderr classifier so
// executor_test.go can pin the gh-output → Outcome map without
// exporting the helper to production callers.
var ClassifyStderrForTest = classifyStderr
