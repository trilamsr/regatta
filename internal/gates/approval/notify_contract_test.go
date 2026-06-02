package approval

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
)

// Spec §7 audit-trail conformance: shared suite every Notifier impl runs through.
//
// Pins the four invariants closed under #198 so future channel impls
// (slack, pagerduty, email) earn full conformance by appending a row:
//
//  1. len(Receipt.DeliveredTo) == len(req.Reviewers) on nil error.
//  2. Emitted obs event carries the canonical four attrs
//     (approval_id, work_item_id, gate_id, reviewer_count).
//  3. zero-reviewer request returns ErrNoReviewers (fail-closed).
//  4. ctx-cancelled Notify returns ctx.Err() before any side effect.
func TestNotifier_InterfaceContract(t *testing.T) {
	cases := []struct {
		name    string
		factory func(t *testing.T) (Notifier, *captureHandler)
	}{
		{
			name: "stub",
			factory: func(t *testing.T) (Notifier, *captureHandler) {
				h := &captureHandler{}
				return NewStubNotifier(slog.New(h)), h
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("kind_matches_receipt_channel", func(t *testing.T) {
				n, _ := tc.factory(t)
				if n.Kind() == "" {
					t.Fatalf("Kind() empty; spec §5.8 requires a registry-stable identifier")
				}
				rcpt, err := n.Notify(context.Background(), newRequest())
				if err != nil {
					t.Fatalf("Notify: %v", err)
				}
				if rcpt.Channel != n.Kind() {
					t.Errorf("Receipt.Channel=%q; want Kind()=%q (auditor reconciliation breaks otherwise)", rcpt.Channel, n.Kind())
				}
			})

			// #198 invariant 1 — delivered set is the full reviewer set.
			// Partial-fan now MUST surface as a non-nil error so callers
			// cannot silently lose a reviewer. The stricter contract
			// catches mutation "drop one reviewer + return nil".
			t.Run("delivered_equals_reviewers", func(t *testing.T) {
				n, _ := tc.factory(t)
				req := newRequest()
				rcpt, err := n.Notify(context.Background(), req)
				if err != nil {
					t.Fatalf("Notify: %v", err)
				}
				if len(rcpt.DeliveredTo) != len(req.Reviewers) {
					t.Fatalf("DeliveredTo len=%d; want %d (full-fan invariant — channels MUST return non-nil error on partial fan)", len(rcpt.DeliveredTo), len(req.Reviewers))
				}
				got := append([]string(nil), rcpt.DeliveredTo...)
				want := append([]string(nil), req.Reviewers...)
				sortStrings(got)
				sortStrings(want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("DeliveredTo set=%v; want %v", got, want)
				}
			})

			// #198 invariant 2 — canonical four-attr audit set.
			// Asserts ON the attrs, not the event name: channel impls MAY
			// emit a channel-specific event (approval.notify_stub,
			// approval.notify_slack) as long as the four canonical attrs
			// are carried. Catches mutation "drop one attr".
			t.Run("emits_canonical_audit_attrs", func(t *testing.T) {
				n, h := tc.factory(t)
				req := newRequest()
				if _, err := n.Notify(context.Background(), req); err != nil {
					t.Fatalf("Notify: %v", err)
				}
				rec, ok := h.findRecordWithAttrs(
					string(obs.KeyApprovalID),
					string(obs.KeyWorkItemID),
					string(obs.KeyGateID),
					string(obs.KeyReviewerCount),
				)
				if !ok {
					t.Fatalf("no emitted record carries all four canonical attrs (%s, %s, %s, %s); records=%+v",
						obs.KeyApprovalID, obs.KeyWorkItemID, obs.KeyGateID, obs.KeyReviewerCount, h.records)
				}
				wantStrs := map[string]string{
					string(obs.KeyApprovalID): req.ApprovalID,
					string(obs.KeyWorkItemID): req.WorkItemID,
					string(obs.KeyGateID):     req.GateName,
				}
				for k, want := range wantStrs {
					v, ok := attrValue(rec, k)
					if !ok || v.String() != want {
						t.Errorf("attr %q=%q want %q (ok=%v)", k, v.String(), want, ok)
					}
				}
				cnt, ok := attrValue(rec, string(obs.KeyReviewerCount))
				if !ok || cnt.Int64() != int64(len(req.Reviewers)) {
					t.Errorf("attr %q=%d want %d (ok=%v)", obs.KeyReviewerCount, cnt.Int64(), len(req.Reviewers), ok)
				}
			})

			// #198 invariant 3 — zero-reviewer is fail-closed.
			// Catches mutation "silently accept empty reviewer set + return
			// empty Receipt"; an approval pause with no reviewers would
			// stall indefinitely with no audit trail otherwise.
			t.Run("zero_reviewer_returns_ErrNoReviewers", func(t *testing.T) {
				n, _ := tc.factory(t)
				req := newRequest()
				req.Reviewers = nil
				req.Tokens = nil
				_, err := n.Notify(context.Background(), req)
				if !errors.Is(err, ErrNoReviewers) {
					t.Fatalf("Notify(zero-reviewer) err=%v; want ErrNoReviewers (fail-closed)", err)
				}
			})

			// #198 invariant 4 — ctx cancellation surfaces before side
			// effects. Catches mutation "swallow ctx + write audit row
			// anyway"; a cancelled gate-tick must not leave a misleading
			// "we notified" breadcrumb.
			t.Run("ctx_cancelled_returns_ctx_err", func(t *testing.T) {
				n, h := tc.factory(t)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				_, err := n.Notify(ctx, newRequest())
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Notify(cancelled) err=%v; want context.Canceled", err)
				}
				// No audit attr-set may be emitted on a cancelled call —
				// the breadcrumb implies a real delivery attempt.
				if _, ok := h.findRecordWithAttrs(
					string(obs.KeyApprovalID),
					string(obs.KeyWorkItemID),
					string(obs.KeyGateID),
					string(obs.KeyReviewerCount),
				); ok {
					t.Errorf("cancelled Notify emitted an audit record; spec §5.8 fail-closed forbids partial side effects")
				}
			})

			t.Run("concurrent_safe", func(t *testing.T) {
				n, _ := tc.factory(t)
				const goroutines = 8
				const perG = 4
				var wg sync.WaitGroup
				errCh := make(chan error, goroutines*perG)
				for i := 0; i < goroutines; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for j := 0; j < perG; j++ {
							if _, err := n.Notify(context.Background(), newRequest()); err != nil {
								errCh <- err
							}
						}
					}()
				}
				wg.Wait()
				close(errCh)
				for err := range errCh {
					t.Errorf("concurrent Notify: %v", err)
				}
			})
		})
	}
}

// findRecordWithAttrs returns the first captured record whose attr set
// is a superset of keys — channel-neutral attr assertion (event name
// MAY vary per channel; canonical attrs MUST NOT).
func (h *captureHandler) findRecordWithAttrs(keys ...string) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		has := make(map[string]bool, len(keys))
		r.Attrs(func(a slog.Attr) bool {
			has[a.Key] = true
			return true
		})
		ok := true
		for _, k := range keys {
			if !has[k] {
				ok = false
				break
			}
		}
		if ok {
			return r, true
		}
	}
	return slog.Record{}, false
}

// sortStrings is a tiny helper kept local so the contract file is
// self-contained for future channel PRs that copy-clone the pattern.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
