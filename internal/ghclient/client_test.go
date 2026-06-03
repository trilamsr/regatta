package ghclient_test

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/alarmwebhook"
	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/selfimprove"
)

// TestUnifiedClient_AlarmwebhookHTTPSatisfiesGHClient asserts the prod alarmwebhook HTTP constructor returns ghclient.Client (#710 B3).
func TestUnifiedClient_AlarmwebhookHTTPSatisfiesGHClient(t *testing.T) {
	var _ ghclient.Client = alarmwebhook.NewHTTPGitHubClient("t", "o", "r", "")
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
