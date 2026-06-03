//go:build !darwin && !linux

package main

import "fmt"

// writableAdapter on non-darwin/non-linux returns a clear error — env
// is the only resolver, and env is operator-set externally.
func writableAdapter() (platformSetter, string, error) {
	return nil, "", fmt.Errorf("no writable secret store on this platform; set env var instead")
}
