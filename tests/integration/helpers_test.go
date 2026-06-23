//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// waitForHealthz polls url until it returns 200 or the deadline elapses.
func waitForHealthz(url string, deadline time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("healthz never 200 at %s: %w", url, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
