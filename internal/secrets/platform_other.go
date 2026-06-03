//go:build !darwin && !linux

package secrets

// platformAdapters on non-darwin/non-linux returns nothing — env is
// the only chain entry. Windows + BSD operators run regatta with the
// env-var pattern documented in §8 of the spec.
func platformAdapters() []Fetcher {
	return nil
}
