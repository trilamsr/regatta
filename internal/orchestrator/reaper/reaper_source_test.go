package reaper

import "os"

// reaperSource reads reaper.go from disk so source-order pin tests can assert call ordering without depending on call-count timing.
func reaperSource() string {
	b, err := os.ReadFile("reaper.go")
	if err != nil {
		return ""
	}
	return string(b)
}
