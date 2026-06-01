package substrate

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
)

// ULID minter — Crockford-base32, 48-bit ms timestamp + 80-bit
// randomness, lexicographically sortable. Wave 1 is single-host
// single-writer (spec §10 #14 / §11); cross-host collision becomes
// a real concern post-W9 (follow-up F6).
//
// Format per https://github.com/ulid/spec:
//   - 10 chars timestamp (50 bits encoded → 48-bit ms used, top 2 zero)
//   - 16 chars random  (80 bits)
//   - Total: 26 chars, sortable lexicographically equals time order.

// crockfordAlphabet — 32 chars, no I L O U (Crockford choice to avoid
// look-alikes). Encoder reads index → char; decoder is not needed
// because we never parse ULIDs at the substrate layer (sqlite stores
// them as opaque TEXT).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// minterState guards monotonic minting against rapid same-ms calls.
// Within one ms, randomness is bumped by 1 rather than re-rolled, so
// ULID monotonicity holds inside a single process. Cross-process
// minted-in-same-ms ULIDs may collide on PK (handled by AppendEvent's
// retry path; see spec §10 #14).
var (
	minterMu      sync.Mutex
	lastMintMs    int64
	lastRandomLo  uint64
	lastRandomHi  uint16
	monotonicMode bool // false until first Mint
)

// Mint returns a Crockford-base32 ULID for time t. Uses crypto/rand
// for randomness. Same-ms calls bump the random portion monotonically
// rather than re-roll — keeps ULIDs strictly increasing inside a
// single process even at sub-ms cadence.
func Mint(t time.Time) string {
	return mintFrom(t, rand.Reader)
}

// mintFrom is Mint with an injectable randomness source for tests.
// Production callers use Mint; the seam exists so tests can pin a
// deterministic ULID for round-trip assertions.
func mintFrom(t time.Time, r io.Reader) string {
	ms := t.UnixMilli()
	if ms < 0 {
		ms = 0
	}

	minterMu.Lock()
	defer minterMu.Unlock()

	var hi uint16
	var lo uint64
	if monotonicMode && ms == lastMintMs {
		// Same-ms collision: bump the 80-bit random monotonically.
		lo = lastRandomLo + 1
		if lo == 0 {
			// 64-bit wrap: bump the high 16 bits. Vanishingly rare.
			hi = lastRandomHi + 1
		} else {
			hi = lastRandomHi
		}
	} else {
		var buf [10]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			// crypto/rand should never fail; if it does (e.g. in
			// fork-without-reseeding scenarios) fall back to a
			// deterministic-but-bad source so the writer doesn't
			// crash silently. Test seam can inject a failing reader
			// to exercise this branch.
			return fmt.Sprintf("000000ULIDFAILURE%010d", ms)
		}
		hi = binary.BigEndian.Uint16(buf[0:2])
		lo = binary.BigEndian.Uint64(buf[2:10])
	}

	lastMintMs = ms
	lastRandomHi = hi
	lastRandomLo = lo
	monotonicMode = true

	return encodeULID(ms, hi, lo)
}

// encodeULID encodes (48-bit ms, 16-bit hi, 64-bit lo) as a 26-char
// Crockford-base32 string. The timestamp portion is the leading 10
// chars; the random portion is the trailing 16 chars.
//
// Encoding is bit-packed 5-bits-per-char from most-significant to
// least-significant. The ms-fits-in-48-bits invariant (year ~10889
// CE) is enforced by the mintFrom caller; truncation here is silent
// (post-year-10889 sortability breaks, not Wave-1's problem).
func encodeULID(ms int64, randHi uint16, randLo uint64) string {
	out := make([]byte, 26)

	// Timestamp: 48 bits → 10 chars (5 bits each), big-endian.
	// mintFrom clamps negative ms to 0, so ms >= 0 here. The cast +
	// mask is intentional (post-year-10889 sortability breaks, not
	// Wave-1's problem).
	ts := uint64(ms) & 0x0000_FFFF_FFFF_FFFF //nolint:gosec // G115: ms is non-negative post-clamp; the mask intentionally truncates to 48 bits
	for i := 9; i >= 0; i-- {
		out[i] = crockfordAlphabet[ts&0x1F]
		ts >>= 5
	}

	// Random: 80 bits (16 hi + 64 lo) → 16 chars. Pack into one
	// 96-bit running value via two 64-bit ints; encode 5 bits at a
	// time from least significant.
	lo := randLo
	hi := uint64(randHi)
	// 16 random chars, indices 10..25. Encode least-significant 5
	// bits per iteration; emit from out[25] down to out[10].
	for i := 25; i >= 10; i-- {
		out[i] = crockfordAlphabet[lo&0x1F]
		// Shift the 80-bit value right by 5 bits.
		lo = (lo >> 5) | ((hi & 0x1F) << 59)
		hi >>= 5
	}

	return string(out)
}
