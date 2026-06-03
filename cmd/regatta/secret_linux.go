//go:build linux

package main

import (
	"fmt"

	"github.com/trilamsr/regatta/internal/secrets"
)

// writableAdapter returns the Linux pass setter. pass binary absent
// surfaces here as a clear error during `regatta secret set` — the
// boot-path Fetcher chain still falls through to env transparently.
func writableAdapter() (platformSetter, string, error) {
	f := secrets.NewPassFetcher("")
	s, ok := f.(platformSetter)
	if !ok {
		return nil, "", fmt.Errorf("pass adapter is not writable (build error)")
	}
	return s, "pass", nil
}
