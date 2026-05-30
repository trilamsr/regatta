package schemas

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSecretRedacts(t *testing.T) {
	t.Parallel()
	const want = "[REDACTED]"
	s := Secret("supersecret-key-bytes-3032bytes-")
	if got := s.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := fmt.Sprintf("%v", s); got != want {
		t.Errorf("%%v = %q, want %q", got, want)
	}
	if got := fmt.Sprintf("%#v", s); got != want {
		t.Errorf("%%#v = %q, want %q", got, want)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `"`+want+`"` {
		t.Errorf("json = %s, want %q", raw, `"`+want+`"`)
	}
	if strings.Contains(fmt.Sprintf("%v", s), "supersecret") {
		t.Errorf("raw bytes leaked through %%v")
	}
}

func TestSecretBytesAtCryptoSeam(t *testing.T) {
	t.Parallel()
	want := []byte("supersecret-key-bytes-3032bytes-")
	s := Secret(want)
	if got := s.Bytes(); string(got) != string(want) {
		t.Errorf("Bytes() = %q, want %q", got, want)
	}
}
