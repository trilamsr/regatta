package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

type fileFetcher struct {
	key  string
	path string
}

// NewFileFetcher returns a Fetcher bound to one canonical key + path. 0600 perm-gate fails closed.
func NewFileFetcher(key, path string) Fetcher {
	return fileFetcher{key: key, path: path}
}

func (fileFetcher) Name() string { return AdapterFile }

// Get reads path trimmed of trailing newline; 0600 perm-gate fails closed.
func (f fileFetcher) Get(_ context.Context, key string) (Value, error) {
	if key != f.key {
		return Value{}, ErrNotFound
	}
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	info, err := os.Stat(f.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Value{}, ErrNotFound
		}
		return Value{}, fmt.Errorf("file source %s: stat: %w", f.path, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return Value{}, fmt.Errorf("file source %s: perms %#o too open (need 0600 or stricter)", f.path, perm)
	}
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return Value{}, fmt.Errorf("file source %s: read: %w", f.path, err)
	}
	return NewValue(bytes.TrimRight(raw, "\r\n")), nil
}
