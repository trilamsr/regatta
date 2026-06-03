package approvaltoken

import (
	"os"
	"regexp"
	"testing"
)

// TestNoMathRandImport guards: token.go MUST NOT import math/rand.
func TestNoMathRandImport(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("token.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	re := regexp.MustCompile(`"math/rand(/v2)?"`)
	if loc := re.FindIndex(src); loc != nil {
		t.Fatalf("token.go imports math/rand at byte offset %d — must use crypto/rand only", loc[0])
	}
}
