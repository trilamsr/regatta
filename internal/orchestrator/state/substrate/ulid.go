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
// real post-W9 (follow-up F6).
//
// Format per https://github.com/ulid/spec: 10-char timestamp (50 bits
// encoded, top 2 zero) + 16-char random (80 bits) = 26 chars.

// crockfordAlphabet — 32 chars, no I L O U (Crockford choice to avoid
// look-alikes). Encoder reads index→char; decoder unused at substrate
// layer (sqlite stores ULIDs as opaque TEXT).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// minterState guards monotonic minting against rapid same-ms calls.
// Within one ms, randomness is bumped by 1 rather than re-rolled so
// ULID monotonicity holds inside a single process. Cross-process
// same-ms ULIDs may collide on PK (handled by AppendEvent's retry
// path; spec §10 #14).
var (
	minterMu      sync.Mutex
	lastMintMs    int64
	lastRandomLo  uint64
	lastRandomHi  uint16
	monotonicMode bool
)

// Mint returns a Crockford-base32 ULID for t using crypto/rand.
// Same-ms calls bump the random portion monotonically rather than
// re-roll, keeping ULIDs strictly increasing at sub-ms cadence.
func Mint(t time.Time) string {
	return mintFrom(t, rand.Reader)
}

// mintFrom is Mint with an injectable randomness source for tests.
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
		lo = lastRandomLo + 1
		if lo == 0 {
			// 64-bit wrap: bump high 16 bits. Vanishingly rare.
			hi = lastRandomHi + 1
		} else {
			hi = lastRandomHi
		}
	} else {
		var buf [10]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			// crypto/rand should never fail; if it does (fork-without-
			// reseed), fall back to a deterministic-but-bad source so
			// the writer does not crash silently. Test seam can inject
			// a failing reader to exercise this branch.
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

// encodeULID packs (48-bit ms, 16-bit hi, 64-bit lo) as a 26-char
// Crockford-base32 string: leading 10 chars timestamp, trailing 16
// chars random, bit-packed 5-bits-per-char MSB→LSB. ms-fits-in-48-bits
// (year ~10889 CE) is enforced upstream; truncation here is silent
// (post-year-10889 sortability breaks, not Wave-1's problem).
func encodeULID(ms int64, randHi uint16, randLo uint64) string {
	out := make([]byte, 26)

	// mintFrom clamps negative ms to 0; cast+mask intentionally
	// truncates to 48 bits.
	ts := uint64(ms) & 0x0000_FFFF_FFFF_FFFF //nolint:gosec // G115: ms non-negative post-clamp; mask intentionally truncates to 48 bits
	for i := 9; i >= 0; i-- {
		out[i] = crockfordAlphabet[ts&0x1F]
		ts >>= 5
	}

	// 80 random bits (16 hi + 64 lo) packed via two 64-bit ints; emit
	// 5 bits/iter LSB→MSB, indices 25..10.
	lo := randLo
	hi := uint64(randHi)
	for i := 25; i >= 10; i-- {
		out[i] = crockfordAlphabet[lo&0x1F]
		lo = (lo >> 5) | ((hi & 0x1F) << 59)
		hi >>= 5
	}

	return string(out)
}
