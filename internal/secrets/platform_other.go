//go:build !darwin && !linux

package secrets

// platformAdapters on non-darwin/non-linux returns nothing — env is
// the only chain entry. Windows + BSD operators run regatta with the
// env-var pattern documented in §8 of the spec.
func platformAdapters() []Fetcher {
	return nil
}

// newNamedKeychainFetcher is unsupported off darwin; returns an
// always-ErrUnsupported fetcher so the routedFetcher falls through
// to the Default chain (#934).
func newNamedKeychainFetcher(_ string) Fetcher { return unsupportedFetcher{adapter: AdapterKeychain} }

// newNamedPassFetcher is unsupported off linux; returns an
// always-ErrUnsupported fetcher so the routedFetcher falls through
// to the Default chain (#934).
func newNamedPassFetcher(_ string) Fetcher { return unsupportedFetcher{adapter: AdapterPass} }
