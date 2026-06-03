package approvaltoken

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// TestNoMathRandImport guards: token.go MUST NOT import math/rand.
func TestNoMathRandImport(t *testing.T) {
	t.Parallel()
	_, filename, _, _ := runtime.Caller(0)
	tokenPath := filepath.Join(filepath.Dir(filename), "token.go")
	src, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	re := regexp.MustCompile(`"math/rand(/v2)?"`)
	if loc := re.FindIndex(src); loc != nil {
		t.Fatalf("token.go imports math/rand at byte offset %d — must use crypto/rand only", loc[0])
	}
}
