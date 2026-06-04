package ghclient_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/alarmwebhook"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/selfimprove"
)

// TestUnifiedClient_AlarmwebhookHTTPSatisfiesGHClient asserts the prod alarmwebhook HTTP constructor returns ghclient.Client (#710 B3).
func TestUnifiedClient_AlarmwebhookHTTPSatisfiesGHClient(t *testing.T) {
	requireClient(t, alarmwebhook.NewHTTPGitHubClient("t", "o", "r", ""))
}

func requireClient(t *testing.T, c ghclient.Client) {
	t.Helper()
	if c == nil {
		t.Fatal("nil ghclient.Client")
	}
}

// TestUnifiedClient_SelfimproveDetectorAcceptsGHClient asserts NewDetector consumes ghclient.Client, not a package-local seam (#710 B3).
func TestUnifiedClient_SelfimproveDetectorAcceptsGHClient(t *testing.T) {
	var c ghclient.Client = stubGHClient{}
	_ = selfimprove.NewDetector(nil, c, false)
}

type stubGHClient struct{}

func (stubGHClient) ListOpenIssuesByLabel(_ context.Context, _, _ string) ([]ghclient.Issue, error) {
	return nil, nil
}
func (stubGHClient) CreateIssue(_ context.Context, _, _ string, _ []string) (int, error) {
	return 0, nil
}
func (stubGHClient) CommentOnIssue(_ context.Context, _ int, _ string) error { return nil }
func (stubGHClient) ListIssuesByLabelPaginated(_ context.Context, _ string, _ ghclient.ListIssuesOpts) ([]ghclient.Issue, error) {
	return nil, nil
}
func (stubGHClient) GetIssue(_ context.Context, _ int) (ghclient.Issue, error) {
	return ghclient.Issue{}, nil
}
func (stubGHClient) EditIssueBody(_ context.Context, _ int, _ string) error { return nil }

// fakeRunner is a ghclient.Runner that returns fixed stdout/stderr/err for one call (#848).
type fakeRunner struct {
	stdout []byte
	stderr []byte
	err    error
}

func (f fakeRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
	return f.stdout, f.stderr, f.err
}

// TestGHClient_GHCLIStderrRateLimit_WrapsWithRetryAfter asserts HTTP 403 + X-RateLimit-Reset header → wrapped ErrRateLimited (closes #848).
func TestGHClient_GHCLIStderrRateLimit_WrapsWithRetryAfter(t *testing.T) {
	resetAt := time.Now().Add(90 * time.Second).Unix()
	stderr := fmt.Appendf(nil, "gh: HTTP 403: API rate limit exceeded (https://api.github.com/issues)\nX-RateLimit-Reset: %d\n", resetAt)
	c := ghclient.NewGHCLIClientWithRunner("o", "r", fakeRunner{stderr: stderr, err: errors.New("exit status 1")})
	_, err := c.ListIssuesByLabelPaginated(context.Background(), "autonomous", ghclient.ListIssuesOpts{})
	if err == nil || !errors.Is(err, schemas.ErrRateLimited) {
		t.Fatalf("err=%v want wrap of schemas.ErrRateLimited", err)
	}
	var rl *ghclient.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err=%v does not carry *RateLimitedError", err)
	}
	if rl.Hint.RetryAfter <= 0 {
		t.Fatalf("RetryAfter=%v want >0", rl.Hint.RetryAfter)
	}
}

// TestGHClient_GHCLIStderr404_WrapsErrNotFound asserts HTTP 404 stderr → wrapped ErrNotFound (closes #848).
func TestGHClient_GHCLIStderr404_WrapsErrNotFound(t *testing.T) {
	stderr := []byte("gh: HTTP 404: Not Found (https://api.github.com/repos/o/r/issues/99)\n")
	c := ghclient.NewGHCLIClientWithRunner("o", "r", fakeRunner{stderr: stderr, err: errors.New("exit status 1")})
	_, err := c.GetIssue(context.Background(), 99)
	if err == nil || !errors.Is(err, schemas.ErrNotFound) {
		t.Fatalf("err=%v want wrap of schemas.ErrNotFound", err)
	}
}

// TestGHClient_GHCLIStderrNegativeRateLimit_ClampsTo1Second asserts past X-RateLimit-Reset → 1s floor (closes #848).
func TestGHClient_GHCLIStderrNegativeRateLimit_ClampsTo1Second(t *testing.T) {
	resetAt := time.Now().Add(-30 * time.Second).Unix()
	stderr := fmt.Appendf(nil, "gh: HTTP 403\nX-RateLimit-Reset: %d\n", resetAt)
	c := ghclient.NewGHCLIClientWithRunner("o", "r", fakeRunner{stderr: stderr, err: errors.New("exit status 1")})
	_, err := c.ListIssuesByLabelPaginated(context.Background(), "autonomous", ghclient.ListIssuesOpts{})
	if err == nil || !errors.Is(err, schemas.ErrRateLimited) {
		t.Fatalf("err=%v want ErrRateLimited", err)
	}
	var rl *ghclient.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err=%v does not carry *RateLimitedError", err)
	}
	if rl.Hint.RetryAfter < time.Second {
		t.Fatalf("RetryAfter=%v want >=1s floor", rl.Hint.RetryAfter)
	}
}
