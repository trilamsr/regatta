package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
)

// loadBriefKeyring returns the configured HMAC keyring for brief
// verification + approval-token verify. The verify-side consumes every
// entry; signers must use loadBriefKeyringWithActive to pick the
// active (write) key.
func loadBriefKeyring() map[string][]byte {
	keys, _ := loadBriefKeyringWithActive()
	return keys
}

// loadBriefKeyringWithActive resolves the multi-key keyring + active
// keyID per docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md
// §3.3. Precedence: REGATTA_HMAC_KEYRING (colon-comma list) overrides
// the legacy REGATTA_HMAC_KEY single-key path. Active = last entry in
// keyring order (rotation IS append) unless REGATTA_HMAC_KEY_ID
// overrides — operators recovering under an older key set the explicit
// keyID and sign under it. Empty keyring returns ("", "") and lets the
// brief loader surface the misconfig via brief.rejected logs.
func loadBriefKeyringWithActive() (map[string][]byte, string) {
	if raw := os.Getenv("REGATTA_HMAC_KEYRING"); raw != "" {
		keys, order, err := parseBriefKeyring(raw)
		if err != nil {
			// Boot-time misconfig should fail loud; we surface via empty
			// keyring + brief.rejected. Future PR-C wires log.Fatal at
			// serve entry.
			return map[string][]byte{}, ""
		}
		active := order[len(order)-1]
		if override := os.Getenv("REGATTA_HMAC_KEY_ID"); override != "" {
			if _, ok := keys[override]; ok {
				active = override
			}
		}
		return keys, active
	}

	envName := os.Getenv("REGATTA_HMAC_KEY_ENV")
	if envName == "" {
		envName = "REGATTA_HMAC_KEY"
	}
	v := os.Getenv(envName)
	if v == "" {
		return map[string][]byte{}, ""
	}
	keyID := os.Getenv("REGATTA_HMAC_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	return map[string][]byte{keyID: []byte(v)}, keyID
}

// parseBriefKeyring decodes the colon-comma multi-key env format
// (`k1:hex,k2:hex,...`) into a keyring map plus the keyID order. Hex
// over JSON because kubectl + Docker `--env` accept colons + commas
// unquoted; JSON forces shell-quote drama (spec §5 reviewer note).
// Returns ErrDuplicateKeyID when two entries share a keyID — silent
// overwrite would lose the older verify path mid-rotation.
func parseBriefKeyring(raw string) (map[string][]byte, []string, error) {
	pairs := strings.Split(raw, ",")
	keys := make(map[string][]byte, len(pairs))
	order := make([]string, 0, len(pairs))
	for i, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.IndexByte(pair, ':')
		if idx <= 0 || idx == len(pair)-1 {
			return nil, nil, fmt.Errorf("keyring entry %d: want 'keyID:hex', got %q", i, pair)
		}
		keyID := strings.TrimSpace(pair[:idx])
		hexMat := strings.TrimSpace(pair[idx+1:])
		decoded, err := hex.DecodeString(hexMat)
		if err != nil {
			return nil, nil, fmt.Errorf("keyring entry %s: hex decode: %w", keyID, err)
		}
		if len(decoded) < schemas.MinKeyLen {
			return nil, nil, fmt.Errorf("keyring entry %s: %w (got %d bytes)", keyID, schemas.ErrWeakKey, len(decoded))
		}
		if _, dup := keys[keyID]; dup {
			return nil, nil, fmt.Errorf("keyring entry %s: duplicate keyID", keyID)
		}
		keys[keyID] = decoded
		order = append(order, keyID)
	}
	if len(order) == 0 {
		return nil, nil, fmt.Errorf("keyring is empty")
	}
	return keys, order, nil
}

// approvalKeyring reuses the brief HMAC key for approval-token signing.
// Operators set REGATTA_HMAC_KEY once and both surfaces light up; an
// empty key returns an empty MapKeyring so NewGate's constructor guard
// fires only when the operator has at least one gate defined.
func approvalKeyring() (approvaltoken.Keyring, string) {
	keys, active := loadBriefKeyringWithActive()
	if active == "" {
		active = "k1"
	}
	return approvaltoken.MapKeyring(keys), active
}
