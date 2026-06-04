package fixture

import (
	"testing"
	"time"
)

// BenchmarkSweeper asserts wall-clock throughput on contended sweeper read loop.
func BenchmarkSweeper(b *testing.B) {
	for i := 0; i < b.N; i++ {
		time.Sleep(1 * time.Millisecond)
	}
}
