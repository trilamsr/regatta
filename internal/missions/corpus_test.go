package missions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/schemas"
)

// testHMACKey is the documented test key used by the
// 02_signed_roundtrip fixture. Must stay stable; the test re-signs
// the fixture body and compares against handoff.signature.mac.
var testHMACKey = []byte("regatta-handoff-test-key-32-bytes!")

// TestHandoffCorpus is the contract test for the handoff schema.
// It walks testdata/handoffs/{pass,fail} and asserts the expected
// outcome for each fixture. Adding a fixture file is the way to
// extend the contract -- no registration code.
func TestHandoffCorpus(t *testing.T) {
	walk(t, "testdata/handoffs/pass", func(path string, data []byte) {
		t.Run("pass/"+filepath.Base(path), func(t *testing.T) {
			if _, err := ParseAndValidate(data); err != nil {
				t.Fatalf("expected pass, got error: %v", err)
			}
		})
	})

	walk(t, "testdata/handoffs/fail", func(path string, data []byte) {
		t.Run("fail/"+filepath.Base(path), func(t *testing.T) {
			_, err := ParseAndValidate(data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrSchemaInvalid) && !errors.Is(err, ErrFalsificationMissingForPattern) {
				t.Fatalf("error not in expected family: %v", err)
			}
		})
	})
}

// TestSignedRoundtripFixture verifies that the documented test fixture
// signs and verifies under testHMACKey. If a contributor edits the
// fixture body, the MAC is recomputed here -- preventing fixture rot.
func TestSignedRoundtripFixture(t *testing.T) {
	path := "testdata/handoffs/pass/02_signed_roundtrip.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	// Re-sign on the fly; do not trust the MAC in the file (the file
	// ships with a placeholder MAC for readability).
	sig, err := schemas.Sign(generic, testHMACKey, "k1")
	if err != nil {
		t.Fatal(err)
	}
	generic["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}
	if err := schemas.Verify(generic, map[string][]byte{"k1": testHMACKey}); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	// Tamper detection: flip a byte.
	generic["success_state"] = "failure"
	if err := schemas.Verify(generic, map[string][]byte{"k1": testHMACKey}); !errors.Is(err, schemas.ErrUnverifiable) {
		t.Fatalf("expected ErrUnverifiable after tamper, got %v", err)
	}
}

func walk(t *testing.T, dir string, fn func(path string, data []byte)) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}
