package canon

import (
	"os"
	"regexp"
	"testing"
)

// Static guard: approval_token.go MUST NOT import math/rand. A predictable
// jti lets an attacker pre-consume a victim's single-use slot (spec §5.2).
func TestNoMathRandImport(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("approval_token.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	re := regexp.MustCompile(`"math/rand(/v2)?"`)
	if loc := re.FindIndex(src); loc != nil {
		t.Fatalf("approval_token.go imports math/rand at byte offset %d — must use crypto/rand only", loc[0])
	}
}
