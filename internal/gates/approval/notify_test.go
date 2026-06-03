package approval

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/slogutil"
)

// captureHandler records every slog.Record emitted through the bound
// logger so a test can assert event name + attrs without depending on
// the JSON wire format.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler     { return h }

func (h *captureHandler) findEvent(name obs.EventName) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == string(name) {
			return r, true
		}
	}
	return slog.Record{}, false
}

func newRequest() Request {
	return Request{
		ApprovalID:       "appr-01",
		WorkItemID:       "wi-42",
		GateName:         "deploy-approval",
		Reviewers:        []string{"alice", "bob", "carol"},
		DecisionDeadline: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Tokens:           map[string]string{"alice": "tok-a", "bob": "tok-b", "carol": "tok-c"},
	}
}

// Spec §5.8 audit-trail invariant: stub still emits typed event + correlation attrs.
func TestStubNotifier_RecordsAuditAttrs(t *testing.T) {
	h := &captureHandler{}
	n := NewStubNotifier(slog.New(h))
	req := newRequest()
	if _, err := n.Notify(context.Background(), req); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	rec, ok := h.findEvent(obs.EventApprovalNotifyStub)
	if !ok {
		t.Fatalf("event %q not emitted; records=%+v", obs.EventApprovalNotifyStub, h.records)
	}
	wantAttrs := map[string]string{
		string(obs.KeyApprovalID): req.ApprovalID,
		string(obs.KeyWorkItemID): req.WorkItemID,
		string(obs.KeyGateID):     req.GateName,
	}
	for k, want := range wantAttrs {
		v, ok := slogutil.AttrValue(rec, k)
		if !ok {
			t.Fatalf("attr %q missing", k)
		}
		if v.String() != want {
			t.Errorf("attr %q=%q; want %q", k, v.String(), want)
		}
	}
	cnt, ok := slogutil.AttrValue(rec, string(obs.KeyReviewerCount))
	if !ok {
		t.Fatalf("attr %q missing", obs.KeyReviewerCount)
	}
	if got := cnt.Int64(); got != int64(len(req.Reviewers)) {
		t.Errorf("reviewer_count=%d; want %d", got, len(req.Reviewers))
	}
}

func TestStubNotifier_ReceiptMirrorsReviewers(t *testing.T) {
	n := NewStubNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := newRequest()
	r, err := n.Notify(context.Background(), req)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if !reflect.DeepEqual(r.DeliveredTo, req.Reviewers) {
		t.Errorf("DeliveredTo=%v; want %v", r.DeliveredTo, req.Reviewers)
	}
	if r.Channel != KindStub {
		t.Errorf("Channel=%q; want %q", r.Channel, KindStub)
	}
	if n.Kind() != KindStub {
		t.Errorf("Kind()=%q; want %q", n.Kind(), KindStub)
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	n := NewStubNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := reg.Register(KindStub, n); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register(KindStub, n); err == nil {
		t.Fatalf("duplicate Register: expected error; got nil")
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("nope")
	if !errors.Is(err, ErrUnknownNotifier) {
		t.Fatalf("Get(unknown) err=%v; want ErrUnknownNotifier", err)
	}
}

// Spec §5.8 fail-closed: unknown kind must error, never silently default to stub.
func TestRegistry_FailClosed(t *testing.T) {
	reg := NewRegistry()
	if _, err := Build(reg, "missing"); err == nil {
		t.Fatalf("Build on empty registry: expected error; got nil")
	}
	n := NewStubNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := reg.Register(KindStub, n); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := Build(reg, KindStub)
	if err != nil {
		t.Fatalf("Build(stub) after register: %v", err)
	}
	if got.Kind() != KindStub {
		t.Errorf("Build returned Kind=%q; want %q", got.Kind(), KindStub)
	}
}
