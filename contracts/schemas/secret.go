package schemas

// Secret wraps a byte slice (HMAC key, API token) so accidental
// logging via fmt or json.Marshal redacts to redactedLiteral instead
// of printing the raw bytes. Use Secret as the env-to-process
// boundary type; convert to []byte only at the HMAC seam.
type Secret []byte

const redactedLiteral = "[REDACTED]"

// Bytes returns the raw key material. Only call at the crypto seam.
func (s Secret) Bytes() []byte { return []byte(s) }

// String implements fmt.Stringer; always returns the redacted form.
func (s Secret) String() string { return redactedLiteral }

// GoString implements fmt.GoStringer; redacts under %#v as well.
func (s Secret) GoString() string { return redactedLiteral }

// MarshalJSON redacts the secret if it is ever encountered by the
// JSON encoder. Signed payloads MUST NOT contain raw key bytes;
// this is a defense-in-depth check.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redactedLiteral + `"`), nil
}

// MarshalText keeps %v / %s output consistent with String.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redactedLiteral), nil
}
