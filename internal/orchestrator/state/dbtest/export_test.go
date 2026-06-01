package dbtest

// FatalReporter is the test-only alias for the private fatalReporter interface,
// letting external test packages stub *testing.T to verify AssertLE's failure
// path without aborting the parent test.
type FatalReporter = fatalReporter

// AssertLEReporter exposes the unexported assertLE for tests that need to inject a stub.
func (q *QueryCounter) AssertLEReporter(r FatalReporter, budget int) {
	q.assertLE(r, budget)
}
