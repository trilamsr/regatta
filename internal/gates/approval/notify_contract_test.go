package approval

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
)

// Spec §7 audit-trail conformance: shared suite every Notifier impl runs through (see #198 for deferred invariants).
func TestNotifier_InterfaceContract(t *testing.T) {
	cases := []struct {
		name    string
		factory func(t *testing.T) Notifier
	}{
		{
			name: "stub",
			factory: func(t *testing.T) Notifier {
				return NewStubNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("kind_matches_receipt_channel", func(t *testing.T) {
				n := tc.factory(t)
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

			t.Run("delivered_subset_of_reviewers", func(t *testing.T) {
				n := tc.factory(t)
				req := newRequest()
				rcpt, err := n.Notify(context.Background(), req)
				if err != nil {
					t.Fatalf("Notify: %v", err)
				}
				allowed := make(map[string]struct{}, len(req.Reviewers))
				for _, r := range req.Reviewers {
					allowed[r] = struct{}{}
				}
				for _, got := range rcpt.DeliveredTo {
					if _, ok := allowed[got]; !ok {
						t.Errorf("DeliveredTo=%q is not in req.Reviewers=%v; notifier MUST NOT invent recipients", got, req.Reviewers)
					}
				}
				if len(rcpt.DeliveredTo) > len(req.Reviewers) {
					t.Errorf("DeliveredTo len=%d > Reviewers len=%d; partial fan only", len(rcpt.DeliveredTo), len(req.Reviewers))
				}
			})

			t.Run("concurrent_safe", func(t *testing.T) {
				n := tc.factory(t)
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
