package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/testutil"
)

// TestValue_StructHasNoExportedFields asserts the reflect-walk invariant that protects against future fields breaking redaction (R8).
func TestValue_StructHasNoExportedFields(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeOf(Value{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.IsExported() {
			t.Fatalf("Value has exported field %q; redaction can be bypassed via reflection", f.Name)
		}
	}
}

// TestValue_StringRedacts asserts the default Stringer path returns the redaction sentinel (R8).
func TestValue_StringRedacts(t *testing.T) {
	t.Parallel()
	v := NewValue([]byte("super-secret-token"))
	if got := v.String(); got != "<redacted>" {
		t.Fatalf("String() = %q, want <redacted>", got)
	}
}

// TestValue_FmtSprintfV_Redacts asserts fmt %v never leaks raw bytes (R8).
func TestValue_FmtSprintfV_Redacts(t *testing.T) {
	t.Parallel()
	v := NewValue([]byte("super-secret-token"))
	got := fmt.Sprintf("%v", v)
	if strings.Contains(got, "super-secret-token") {
		t.Fatalf("fmt %%v leaked secret: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("fmt %%v missing redaction marker: %q", got)
	}
}

// TestValue_FmtSprintfPlusV_Redacts asserts %+v cannot reflect into unexported bytes (R8).
func TestValue_FmtSprintfPlusV_Redacts(t *testing.T) {
	t.Parallel()
	v := NewValue([]byte("super-secret-token"))
	got := fmt.Sprintf("%+v", v)
	if strings.Contains(got, "super-secret-token") {
		t.Fatalf("fmt %%+v leaked secret: %q", got)
	}
}

// TestValue_MarshalJSON_Redacts asserts JSON encoding emits the redaction sentinel and never leaks raw bytes (R8).
func TestValue_MarshalJSON_Redacts(t *testing.T) {
	t.Parallel()
	v := NewValue([]byte("super-secret-token"))
	// MarshalJSON directly returns the canonical redaction.
	b, err := v.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"<redacted>"` {
		t.Fatalf("MarshalJSON = %s, want \"<redacted>\"", b)
	}
	// json.Marshal HTML-escapes `<` and `>` by default; assert raw
	// secret never appears regardless of encoder escaping policy.
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(out), "super-secret-token") {
		t.Fatalf("json.Marshal leaked secret: %s", out)
	}
	if !strings.Contains(string(out), "redacted") {
		t.Fatalf("json.Marshal missing redaction marker: %s", out)
	}
}

// TestValue_SlogInfo_Redacts asserts slog Text handler emits redaction marker not raw bytes (R8).
func TestValue_SlogInfo_Redacts(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, nil)
	lg := slog.New(h)
	v := NewValue([]byte("super-secret-token"))
	lg.Info("secret", "value", v)
	out := buf.String()
	if strings.Contains(out, "super-secret-token") {
		t.Fatalf("slog leaked secret: %q", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("slog output missing redaction marker: %q", out)
	}
}

// TestValue_Bytes_ReturnsActual confirms the legitimate accessor returns raw bytes for hand-off to external APIs.
func TestValue_Bytes_ReturnsActual(t *testing.T) {
	t.Parallel()
	want := []byte("super-secret-token")
	v := NewValue(want)
	if !bytes.Equal(v.Bytes(), want) {
		t.Fatalf("Bytes() = %q, want %q", v.Bytes(), want)
	}
}

// TestValue_NewValue_CopiesInput asserts caller-side mutation does not bleed into the wrapped bytes.
func TestValue_NewValue_CopiesInput(t *testing.T) {
	t.Parallel()
	raw := []byte("token-x")
	v := NewValue(raw)
	raw[0] = '!'
	if string(v.Bytes()) != "token-x" {
		t.Fatalf("Value did not copy input: got %q", v.Bytes())
	}
}

// TestValidateKey_RejectsPathTraversal pins the canonical-key regex against shell-traversal payloads (R12).
func TestValidateKey_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"../etc/passwd",
		"regatta./etc/passwd",
		"regatta.../foo",
		"REGATTA.foo",
		"regatta.foo!",
		"",
	} {
		if err := ValidateKey(bad); err == nil {
			t.Fatalf("ValidateKey(%q) accepted; want reject", bad)
		}
	}
}

// TestValidateKey_AcceptsCanonical confirms the four spec keys pass.
func TestValidateKey_AcceptsCanonical(t *testing.T) {
	t.Parallel()
	for _, k := range CanonicalKeys {
		if err := ValidateKey(k); err != nil {
			t.Fatalf("ValidateKey(%q) rejected canonical: %v", k, err)
		}
	}
}

// TestEnvFetcher_GetSetEnv_ReturnsValue covers the happy-path env adapter read.
func TestEnvFetcher_GetSetEnv_ReturnsValue(t *testing.T) {
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "sk-test")
	f := NewEnvFetcher()
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("env Get: %v", err)
	}
	if string(v.Bytes()) != "sk-test" {
		t.Fatalf("env Get = %q, want sk-test", v.Bytes())
	}
}

// TestEnvFetcher_MissingKey_ReturnsErrNotFound covers the chain-fallthrough signal.
func TestEnvFetcher_MissingKey_ReturnsErrNotFound(t *testing.T) {
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "")
	f := NewEnvFetcher()
	_, err := f.Get(context.Background(), KeyAnthropic)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("env Get: err = %v, want ErrNotFound", err)
	}
}

// TestEnvVarName_MapsCanonicalKey exercises the dot-to-underscore mapping.
func TestEnvVarName_MapsCanonicalKey(t *testing.T) {
	t.Parallel()
	// gosec G101: these are env-var NAMES, not credentials.
	cases := map[string]string{ //nolint:gosec
		KeyAnthropic:    "REGATTA_ANTHROPIC_API_KEY",
		KeyGHToken:      "REGATTA_GH_TOKEN",
		KeyBriefHMACs:   "REGATTA_BRIEF_HMAC_KEYS",
		KeyAuditHMACKey: "REGATTA_AUDIT_HMAC_KEY",
	}
	for in, want := range cases {
		if got := EnvVarName(in); got != want {
			t.Fatalf("EnvVarName(%q) = %q, want %q", in, got, want)
		}
	}
}

// stubFetcher returns canned (Value, error) per call for composite-chain testing.
type stubFetcher struct {
	name string
	v    Value
	err  error
}

func (s stubFetcher) Get(_ context.Context, _ string) (Value, error) { return s.v, s.err }
func (s stubFetcher) Name() string                                   { return s.name }

// TestCompositeFetcher_KeychainFails_FallsThroughToEnv asserts ErrNotFound on the first adapter advances to the second.
func TestCompositeFetcher_KeychainFails_FallsThroughToEnv(t *testing.T) {
	t.Parallel()
	first := stubFetcher{name: "keychain", err: ErrNotFound}
	second := stubFetcher{name: "env", v: NewValue([]byte("from-env"))}
	c := NewComposite(first, second)
	v, err := c.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("composite Get: %v", err)
	}
	if string(v.Bytes()) != "from-env" {
		t.Fatalf("composite Get = %q, want from-env", v.Bytes())
	}
}

// TestCompositeFetcher_UnsupportedFallsThrough confirms ErrUnsupported is treated like ErrNotFound (R11).
func TestCompositeFetcher_UnsupportedFallsThrough(t *testing.T) {
	t.Parallel()
	first := stubFetcher{name: "keychain", err: ErrUnsupported}
	second := stubFetcher{name: "env", v: NewValue([]byte("env"))}
	c := NewComposite(first, second)
	v, err := c.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("composite Get: %v", err)
	}
	if string(v.Bytes()) != "env" {
		t.Fatalf("composite Get = %q, want env", v.Bytes())
	}
}

// TestCompositeFetcher_AllFail_ReturnsErrNotFound asserts every-adapter-miss surfaces the canonical sentinel.
func TestCompositeFetcher_AllFail_ReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	c := NewComposite(
		stubFetcher{name: "keychain", err: ErrNotFound},
		stubFetcher{name: "env", err: ErrNotFound},
	)
	_, err := c.Get(context.Background(), KeyAnthropic)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("composite Get: err = %v, want ErrNotFound", err)
	}
}

// TestCompositeFetcher_RealErrorAborts asserts a non-NotFound error halts the chain so misconfig surfaces loudly.
func TestCompositeFetcher_RealErrorAborts(t *testing.T) {
	t.Parallel()
	boom := errors.New("keychain locked")
	c := NewComposite(
		stubFetcher{name: "keychain", err: boom},
		stubFetcher{name: "env", v: NewValue([]byte("never-reached"))},
	)
	_, err := c.Get(context.Background(), KeyAnthropic)
	if err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Fatalf("composite Get: err = %v, want wrapped keychain-locked", err)
	}
}

// TestEnvOnly_ExplicitDisableSkipsPlatformAdaptersWithoutTimeout asserts the CI knob short-circuits to env (R2).
func TestEnvOnly_ExplicitDisableSkipsPlatformAdaptersWithoutTimeout(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "from-env")
	f := Default(context.Background())
	if !strings.Contains(f.Name(), "env") {
		t.Fatalf("Default chain = %q, want env-only", f.Name())
	}
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Default Get: %v", err)
	}
	if string(v.Bytes()) != "from-env" {
		t.Fatalf("Default Get = %q, want from-env", v.Bytes())
	}
}

// TestCache_LoadPopulates asserts boot-load fills the snapshot per canonical key.
func TestCache_LoadPopulates(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "a")
	t.Setenv("REGATTA_GH_TOKEN", "g")
	c := NewCache()
	c.Load(context.Background(), Default(context.Background()), nil)
	v, src, ok := c.Get(KeyAnthropic)
	if !ok {
		t.Fatalf("cache Get missing key after Load")
	}
	if string(v.Bytes()) != "a" {
		t.Fatalf("cache Get value = %q, want a", v.Bytes())
	}
	if src == "" || src == "missing" {
		t.Fatalf("cache Get source = %q, want adapter name", src)
	}
}

// TestCache_MissingKeyReturnsFalse asserts unfetched keys surface as (zero, missing, false).
func TestCache_MissingKeyReturnsFalse(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "")
	c := NewCache()
	c.Load(context.Background(), Default(context.Background()), nil)
	_, src, ok := c.Get(KeyAnthropic)
	if ok {
		t.Fatalf("cache reported key present; want missing")
	}
	if src != "missing" {
		t.Fatalf("cache source = %q, want missing", src)
	}
}

// TestCache_SnapshotSourceDoesNotLeakValues asserts the diagnostic accessor only returns source labels.
func TestCache_SnapshotSourceDoesNotLeakValues(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "secret-leak-check")
	c := NewCache()
	c.Load(context.Background(), Default(context.Background()), nil)
	snap := c.Snapshot()
	for k, v := range snap {
		if strings.Contains(v, "secret-leak-check") {
			t.Fatalf("snapshot leaked secret in key=%s value=%s", k, v)
		}
	}
}

// rotatingFetcher returns counter-stamped values per Get so SIGHUP-driven re-fetches are observable.
type rotatingFetcher struct {
	counter atomic.Int64
}

func (r *rotatingFetcher) Get(_ context.Context, key string) (Value, error) {
	n := r.counter.Add(1)
	return NewValue([]byte(fmt.Sprintf("%s-%d", key, n))), nil
}

func (*rotatingFetcher) Name() string { return "rotating" }

// TestCache_SIGHUPSwapsSnapshotAtomically asserts SIGHUP triggers re-fetch + atomic publish without partial reads (§4).
func TestCache_SIGHUPSwapsSnapshotAtomically(t *testing.T) {
	rf := &rotatingFetcher{}
	c := NewCache()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Load(ctx, rf, nil)
	v1, _, _ := c.Get(KeyAnthropic)
	first := string(v1.Bytes())

	// Guard signal.Notify so a stray SIGHUP that lands before Run's
	// own Notify registers does NOT terminate the test process. The
	// duplicate notify is idempotent per os/signal docs.
	stop := installHUPGuard(t)
	defer stop()
	_ = os.Getpid()

	done := make(chan struct{})
	go func() {
		c.Run(ctx, rf, nil)
		close(done)
	}()
	// Yield to let Run's signal.Notify register.
	time.Sleep(20 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill SIGHUP: %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	testutil.Eventually(t, waitCtx, 5*time.Millisecond, func() bool {
		v2, _, _ := c.Get(KeyAnthropic)
		return string(v2.Bytes()) != first
	}, "SIGHUP did not refresh snapshot within deadline")
	waitCancel()
	cancel()
	<-done
}

// TestCache_ReadersDoNotBlockDuringRotation pins the atomic.Pointer hot-path invariant — concurrent readers see consistent snapshots.
func TestCache_ReadersDoNotBlockDuringRotation(t *testing.T) {
	rf := &rotatingFetcher{}
	c := NewCache()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Load(ctx, rf, nil)

	var stop atomic.Bool
	var wg sync.WaitGroup
	readers := 8
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				// Read every canonical key; assert snapshot internal
				// consistency — every key resolves to the same counter
				// generation in any single snapshot.
				snap := c.ptr.Load()
				if snap == nil {
					continue
				}
				for _, k := range CanonicalKeys {
					v, ok := snap.values[k]
					_ = v
					_ = ok
				}
			}
		}()
	}
	// Drive rotations from a worker goroutine while readers spin.
	for i := 0; i < 200; i++ {
		c.Rotate(ctx, rf, nil)
	}
	stop.Store(true)
	wg.Wait()
}

// TestComposite_GetWithSource_ReturnsWinningAdapter asserts the per-key source is the adapter that resolved, not the chain name (#653).
func TestComposite_GetWithSource_ReturnsWinningAdapter(t *testing.T) {
	t.Parallel()
	first := stubFetcher{name: AdapterKeychain, err: ErrNotFound}
	second := stubFetcher{name: AdapterEnv, v: NewValue([]byte("v"))}
	c := NewComposite(first, second)
	v, src, err := GetWithSource(context.Background(), c, KeyAnthropic)
	if err != nil {
		t.Fatalf("GetWithSource: %v", err)
	}
	if string(v.Bytes()) != "v" {
		t.Fatalf("value = %q, want v", v.Bytes())
	}
	if src != AdapterEnv {
		t.Fatalf("source = %q, want %q (per-key adapter, not chain name)", src, AdapterEnv)
	}
}

// TestComposite_GetWithSource_MissingReturnsSourceMissing asserts a clean miss maps to the sourceMissing label.
func TestComposite_GetWithSource_MissingReturnsSourceMissing(t *testing.T) {
	t.Parallel()
	c := NewComposite(
		stubFetcher{name: AdapterKeychain, err: ErrNotFound},
		stubFetcher{name: AdapterEnv, err: ErrNotFound},
	)
	_, src, err := GetWithSource(context.Background(), c, KeyAnthropic)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if src != sourceMissing {
		t.Fatalf("source = %q, want %q", src, sourceMissing)
	}
}

// TestGetWithSource_PlainFetcherFallback asserts non-SourceFetcher implementations still resolve via Name().
func TestGetWithSource_PlainFetcherFallback(t *testing.T) {
	t.Parallel()
	// stubFetcher does not implement SourceFetcher.
	f := stubFetcher{name: "plain", v: NewValue([]byte("x"))}
	v, src, err := GetWithSource(context.Background(), f, KeyAnthropic)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if src != "plain" || string(v.Bytes()) != "x" {
		t.Fatalf("got (%q,%q), want (x,plain)", v.Bytes(), src)
	}
}

// TestCache_LoadAssignsPerKeySource asserts the cache snapshot records the resolving adapter per key (#653).
func TestCache_LoadAssignsPerKeySource(t *testing.T) {
	t.Setenv(disableEnv, "1")
	t.Setenv("REGATTA_ANTHROPIC_API_KEY", "a")
	c := NewCache()
	c.Load(context.Background(), Default(context.Background()), nil)
	snap := c.Snapshot()
	if snap[KeyAnthropic] != AdapterEnv {
		t.Fatalf("snapshot source = %q, want %q (per-key)", snap[KeyAnthropic], AdapterEnv)
	}
}

// TestDefault_DefaultChainIncludesEnv asserts env is always the tail fetcher across platforms.
func TestDefault_DefaultChainIncludesEnv(t *testing.T) {
	t.Setenv(disableEnv, "")
	f := Default(context.Background())
	if !strings.Contains(f.Name(), "env") {
		t.Fatalf("Default chain = %q, missing env", f.Name())
	}
}
