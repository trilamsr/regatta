//go:build darwin

package main

import (
	"fmt"

	"github.com/trilamsr/regatta/internal/secrets"
)

// writableAdapter returns the darwin platform-store setter
// (Keychain). The Fetcher interface is read-only; Set/Delete bind
// here so the CLI gets a concrete writer without leaking platform
// shape into the boot-path interface.
func writableAdapter() (platformSetter, string, error) {
	f := secrets.NewKeychainFetcher("")
	s, ok := f.(platformSetter)
	if !ok {
		return nil, "", fmt.Errorf("keychain adapter is not writable (build error)")
	}
	return s, "keychain", nil
}
