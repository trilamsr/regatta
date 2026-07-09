package githubissues

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/ghclient"
)

// fakeGH is the in-test ghclient.Client; canned issues + recorded calls.
type fakeGH struct {
	mu          sync.Mutex
	listIssues  []ghclient.Issue
	listErr     error
	listCalls   int
	getIssue    map[int]ghclient.Issue
	getErr      error
	editCalls   []editCall
	editErr     error
	commentLog  []commentCall
	commentErr  error
	lastListLabel string
	lastListState string
}

type editCall struct {
	Number int
	Body   string
}
type commentCall struct {
	Number int
	Body   string
}

func (f *fakeGH) ListOpenIssuesByLabel(_ context.Context, _, _ string) ([]ghclient.Issue, error) {
	return nil, nil
}
func (f *fakeGH) CreateIssue(_ context.Context, _, _ string, _ []string) (int, error) {
	return 0, nil
}
func (f *fakeGH) CommentOnIssue(_ context.Context, n int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commentErr != nil {
		return f.commentErr
	}
	f.commentLog = append(f.commentLog, commentCall{Number: n, Body: body})
	return nil
}
func (f *fakeGH) ListIssuesByLabelPaginated(_ context.Context, label string, opts ghclient.ListIssuesOpts) ([]ghclient.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	f.lastListLabel = label
	f.lastListState = opts.State
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]ghclient.Issue, len(f.listIssues))
	copy(out, f.listIssues)
	return out, nil
}
func (f *fakeGH) GetIssue(_ context.Context, n int) (ghclient.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return ghclient.Issue{}, f.getErr
	}
	if iss, ok := f.getIssue[n]; ok {
		return iss, nil
	}
	return ghclient.Issue{}, fmt.Errorf("not found")
}
func (f *fakeGH) EditIssueBody(_ context.Context, n int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.editErr != nil {
		return f.editErr
	}
	f.editCalls = append(f.editCalls, editCall{Number: n, Body: body})
	return nil
}

func loadFixture(t *testing.T, name string) ghclient.Issue {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var iss ghclient.Issue
	if err := json.Unmarshal(data, &iss); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return iss
}

type captureLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureLog) Logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (c *captureLog) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

func newAdapter(t *testing.T, gh ghclient.Client, log *captureLog, now func() time.Time) schemas.WorkItemSource {
	t.Helper()
	cfg := GitHubIssuesConfig{
		Client: gh,
		Repo:   Repo{Owner: "trilamsr", Name: "regatta"},
		Logger: log.Logf,
		Clock:  now,
	}
	a, err := NewGitHubIssues(cfg)
	if err != nil {
		t.Fatalf("NewGitHubIssues: %v", err)
	}
	return a
}

// TestGitHubIssues_List_FiltersToAutonomousLabel skips issues missing the discriminator (#590).
func TestGitHubIssues_List_FiltersToAutonomousLabel(t *testing.T) {
	gh := &fakeGH{listIssues: []ghclient.Issue{
		{Number: 1, Title: "ITEM-1: foo", Body: "## Acceptance criteria\n- [planned] c1: x\n", Labels: []string{"autonomous"}},
		{Number: 2, Title: "ITEM-2: bar", Body: "## Acceptance criteria\n- [planned] c1: x\n", Labels: []string{"bug"}},
	}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != "ITEM-1" {
		t.Fatalf("expected 1 autonomous item; got %+v", items)
	}
}

// TestGitHubIssues_List_ProjectsTitleIDPrefix extracts ID from `^[A-Z]:` title (#590).
func TestGitHubIssues_List_ProjectsTitleIDPrefix(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	items, err := a.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("List: items=%d err=%v", len(items), err)
	}
	if items[0].ID != "ITEM-590" {
		t.Fatalf("ID=%s want ITEM-590", items[0].ID)
	}
	if items[0].Title != "implement adapter listing" {
		t.Fatalf("Title=%q want stripped prefix", items[0].Title)
	}
}

// TestGitHubIssues_List_SkipsBadIDPrefix_WarnsAndRecords records skip payload for bad titles.
func TestGitHubIssues_List_SkipsBadIDPrefix_WarnsAndRecords(t *testing.T) {
	iss := loadFixture(t, "issue-593-bad-title.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	log := &captureLog{}
	a := newAdapter(t, gh, log, time.Now)
	items, err := a.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("expected 0 items; got %d err=%v", len(items), err)
	}
	joined := strings.Join(log.Lines(), "\n")
	if !strings.Contains(joined, "github_issues.skip") || !strings.Contains(joined, string(ReasonBadIDPrefix)) {
		t.Fatalf("missing skip warn: %s", joined)
	}
}

// TestGitHubIssues_List_ExtractsMetadataFields parses lane/deps/linked_artifact from HTML-comment YAML.
func TestGitHubIssues_List_ExtractsMetadataFields(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	items, err := a.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("List: %d err=%v", len(items), err)
	}
	wi := items[0]
	if wi.Lane != "server" {
		t.Fatalf("Lane=%s want server", wi.Lane)
	}
	if wi.LinkedArtifact != "docs/rfc/590.md" {
		t.Fatalf("LinkedArtifact=%s", wi.LinkedArtifact)
	}
	if len(wi.Dependencies) != 2 {
		t.Fatalf("Dependencies=%v want 2", wi.Dependencies)
	}
}

// TestGitHubIssues_List_AcceptanceSectionParsedAsCriteria parses bullets into []Criterion.
func TestGitHubIssues_List_AcceptanceSectionParsedAsCriteria(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	items, _ := a.List(context.Background())
	if len(items[0].AcceptanceCriteria) != 2 {
		t.Fatalf("criteria=%d want 2", len(items[0].AcceptanceCriteria))
	}
	if items[0].AcceptanceCriteria[0].ID != "c1" {
		t.Fatalf("first criterion ID=%s", items[0].AcceptanceCriteria[0].ID)
	}
}

// TestGitHubIssues_List_MissingAcceptance_EmptyCriteria soft-fails to empty criteria, not error.
func TestGitHubIssues_List_MissingAcceptance_EmptyCriteria(t *testing.T) {
	iss := loadFixture(t, "issue-591-no-acceptance.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	items, err := a.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("List: items=%d err=%v", len(items), err)
	}
	if len(items[0].AcceptanceCriteria) != 0 {
		t.Fatalf("expected empty criteria, got %v", items[0].AcceptanceCriteria)
	}
}

// TestGitHubIssues_List_DupIDPrefix_BothSkipped_CommentsOnBothIssues asserts collision skip + comment.
func TestGitHubIssues_List_DupIDPrefix_BothSkipped_CommentsOnBothIssues(t *testing.T) {
	a := loadFixture(t, "issue-592-dup-id.json")
	b := loadFixture(t, "issue-595-dup-id.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{a, b}}
	log := &captureLog{}
	ad := newAdapter(t, gh, log, time.Now)
	items, err := ad.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
	if len(gh.commentLog) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(gh.commentLog))
	}
	// Second List call should not re-comment (in-memory dedup holds).
	_, _ = ad.List(context.Background())
	if len(gh.commentLog) != 2 {
		t.Fatalf("re-list re-commented: got %d total", len(gh.commentLog))
	}
}

// TestGitHubIssues_List_DedupesViaBodyMarker never re-backfills when marker present.
func TestGitHubIssues_List_DedupesViaBodyMarker(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	_, _ = a.List(context.Background())
	_, _ = a.List(context.Background())
	if len(gh.editCalls) != 0 {
		t.Fatalf("expected 0 backfills (marker present); got %d", len(gh.editCalls))
	}
}

// TestGitHubIssues_List_BodyMissingMarker_BackfillsOnce edits body once on first sighting.
func TestGitHubIssues_List_BodyMissingMarker_BackfillsOnce(t *testing.T) {
	iss := ghclient.Issue{
		Number: 700,
		Title:  "ITEM-700: backfill",
		Body:   "no marker yet\n\n## Acceptance criteria\n- [planned] c1: x\n",
		Labels: []string{"autonomous"},
	}
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	_, _ = a.List(context.Background())
	if len(gh.editCalls) != 1 {
		t.Fatalf("expected 1 backfill; got %d", len(gh.editCalls))
	}
	if !strings.Contains(gh.editCalls[0].Body, DedupMarkerPrefix) {
		t.Fatalf("backfilled body lacks marker: %q", gh.editCalls[0].Body)
	}
}

// TestGitHubIssues_List_BackfillEditFails_SkipsAndWarns pins SKIP semantic on marker write failure (#849).
func TestGitHubIssues_List_BackfillEditFails_SkipsAndWarns(t *testing.T) {
	iss := ghclient.Issue{
		Number: 849,
		Title:  "ITEM-849: backfill fail",
		Body:   "no marker yet\n\n## Acceptance criteria\n- [planned] c1: x\n",
		Labels: []string{"autonomous"},
	}
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}, editErr: errors.New("403 permission denied")}
	log := &captureLog{}
	a := newAdapter(t, gh, log, time.Now)
	items, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected SKIP on backfill failure, got %d projected items", len(items))
	}
	joined := strings.Join(log.Lines(), "\n")
	if !strings.Contains(joined, "github_issues.skip") || !strings.Contains(joined, string(ReasonBackfillFailed)) {
		t.Fatalf("missing skip warn payload: %s", joined)
	}
}

// TestGitHubIssues_List_RateLimitWrapsErrRateLimited drives real ghclient.GHCLIClient via Runner fixture so stderr classifier (#848) reaches the adapter (closes #863).
func TestGitHubIssues_List_RateLimitWrapsErrRateLimited(t *testing.T) {
	resetAt := time.Now().Add(90 * time.Second).Unix()
	stderr := []byte(fmt.Sprintf("gh: HTTP 403: API rate limit exceeded\nX-RateLimit-Reset: %d\n", resetAt))
	gh := ghclient.NewGHCLIClientWithRunner("trilamsr", "regatta", rateLimitRunner{stderr: stderr, err: errors.New("exit status 1")})
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	_, err := a.List(context.Background())
	if !errors.Is(err, schemas.ErrRateLimited) {
		t.Fatalf("err=%v want errors.Is(err, ErrRateLimited)", err)
	}
	var rl *ghclient.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err=%v does not unwrap to *RateLimitedError", err)
	}
	if rl.Hint.RetryAfter <= 0 {
		t.Fatalf("RetryAfter=%v want >0", rl.Hint.RetryAfter)
	}
}

// rateLimitRunner is the canned ghclient.Runner returning exit-1 + stderr (#863).
type rateLimitRunner struct {
	stderr []byte
	err    error
}

func (r rateLimitRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
	return nil, r.stderr, r.err
}

// TestGitHubIssues_Get_NotFound_WrapsErrNotFound surfaces ErrNotFound for unknown ID.
func TestGitHubIssues_Get_NotFound_WrapsErrNotFound(t *testing.T) {
	gh := &fakeGH{listIssues: nil}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	_, err := a.Get(context.Background(), "ITEM-DOES-NOT-EXIST")
	if !errors.Is(err, schemas.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

// TestGitHubIssues_Get_IDCollision_WrapsErrPermanent surfaces ErrPermanent on dup-ID Get.
func TestGitHubIssues_Get_IDCollision_WrapsErrPermanent(t *testing.T) {
	a := loadFixture(t, "issue-592-dup-id.json")
	b := loadFixture(t, "issue-595-dup-id.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{a, b}}
	ad := newAdapter(t, gh, &captureLog{}, time.Now)
	_, err := ad.Get(context.Background(), "DUP-1")
	if !errors.Is(err, schemas.ErrPermanent) {
		t.Fatalf("err=%v want ErrPermanent", err)
	}
}

// TestGitHubIssues_Get_CacheMissAfterMinPoll_Refetches refreshes the cache after TTL.
func TestGitHubIssues_Get_CacheMissAfterMinPoll_Refetches(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	gh := &fakeGH{
		listIssues: []ghclient.Issue{iss},
		getIssue:   map[int]ghclient.Issue{590: iss},
	}
	now := time.Unix(1700000000, 0)
	clk := func() time.Time { return now }
	a := newAdapter(t, gh, &captureLog{}, clk)
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	listsAfterPrime := gh.listCalls
	// Within TTL — Get hits cache + GetIssue, no extra List.
	if _, err := a.Get(context.Background(), "ITEM-590"); err != nil {
		t.Fatalf("cache hit Get: %v", err)
	}
	if gh.listCalls != listsAfterPrime {
		t.Fatalf("expected cache hit, but listCalls grew")
	}
	// Advance past TTL → Get re-issues List for the search-by-id rebuild.
	now = now.Add(DefaultMinPoll + time.Second)
	if _, err := a.Get(context.Background(), "ITEM-590"); err != nil {
		t.Fatalf("cache-miss Get: %v", err)
	}
	if gh.listCalls != listsAfterPrime+1 {
		t.Fatalf("expected exactly 1 refetch; listCalls went %d→%d", listsAfterPrime, gh.listCalls)
	}
}

// TestGitHubIssues_Skip_WarnsWithPayloadSchema asserts the §7.6 closed-enum WARN payload (renamed per #841).
func TestGitHubIssues_Skip_WarnsWithPayloadSchema(t *testing.T) {
	iss := loadFixture(t, "issue-593-bad-title.json")
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	log := &captureLog{}
	a := newAdapter(t, gh, log, time.Now)
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	got := strings.Join(log.Lines(), "\n")
	for _, field := range []string{"adapter=github_issues", "repo=trilamsr/regatta", "issue_number=593", "reason=bad_id_prefix", "issue_url=https://github.com/trilamsr/regatta/issues/593"} {
		if !strings.Contains(got, field) {
			t.Fatalf("WARN payload missing %q: %s", field, got)
		}
	}
}

// TestGitHubIssues_UpdateStatus_AlwaysErrAdapterUnsupported returns sentinel for every input.
func TestGitHubIssues_UpdateStatus_AlwaysErrAdapterUnsupported(t *testing.T) {
	gh := &fakeGH{}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	err := a.UpdateStatus(context.Background(), "ITEM-1", schemas.StatusDone, "cite")
	if !errors.Is(err, schemas.ErrAdapterUnsupported) {
		t.Fatalf("err=%v want ErrAdapterUnsupported", err)
	}
}

// TestGitHubIssues_Capabilities_NoWebhookNoBulk_MinPoll30s pins capability flags.
func TestGitHubIssues_Capabilities_NoWebhookNoBulk_MinPoll30s(t *testing.T) {
	gh := &fakeGH{}
	a := newAdapter(t, gh, &captureLog{}, time.Now)
	c := a.Capabilities()
	if c.Webhook || c.BulkUpdate {
		t.Fatalf("Webhook=%v BulkUpdate=%v", c.Webhook, c.BulkUpdate)
	}
	if c.MinPollInterval != DefaultMinPoll {
		t.Fatalf("MinPoll=%v want %v", c.MinPollInterval, DefaultMinPoll)
	}
}

// TestGitHubIssues_List_NeverLogsBodyBytes asserts WARN payloads never include raw body (§8.3).
func TestGitHubIssues_List_NeverLogsBodyBytes(t *testing.T) {
	body := "SECRET_TOKEN_DO_NOT_LOG_ME"
	iss := ghclient.Issue{
		Number: 800,
		Title:  "[non-id-prefix] " + body,
		Body:   body,
		Labels: []string{"autonomous"},
	}
	gh := &fakeGH{listIssues: []ghclient.Issue{iss}}
	log := &captureLog{}
	a := newAdapter(t, gh, log, time.Now)
	_, _ = a.List(context.Background())
	for _, line := range log.Lines() {
		if strings.Contains(line, body) {
			t.Fatalf("WARN leaked body: %q", line)
		}
	}
}

// TestGitHubIssues_GetMidflightBodyEdit_ReturnsErrSourceMutated asserts adapter surfaces mid-flight edits via UpdatedAt drift (#850).
func TestGitHubIssues_GetMidflightBodyEdit_ReturnsErrSourceMutated(t *testing.T) {
	t1 := time.Unix(1700000000, 0)
	t2 := t1.Add(5 * time.Minute)
	listIss := loadFixture(t, "issue-590-valid.json")
	listIss.UpdatedAt = t1
	getIss := listIss
	getIss.UpdatedAt = t2
	getIss.Body = listIss.Body + "\noperator edited mid-flight\n"
	gh := &fakeGH{
		listIssues: []ghclient.Issue{listIss},
		getIssue:   map[int]ghclient.Issue{listIss.Number: getIss},
	}
	now := t1
	a := newAdapter(t, gh, &captureLog{}, func() time.Time { return now })
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	_, err := a.Get(context.Background(), "ITEM-590")
	if !errors.Is(err, schemas.ErrSourceMutated) {
		t.Fatalf("err=%v want ErrSourceMutated", err)
	}
}

// TestGitHubIssues_GetUnchangedUpdatedAt_ReturnsWorkItem asserts no false positive when UpdatedAt is stable (#850).
func TestGitHubIssues_GetUnchangedUpdatedAt_ReturnsWorkItem(t *testing.T) {
	t1 := time.Unix(1700000000, 0)
	iss := loadFixture(t, "issue-590-valid.json")
	iss.UpdatedAt = t1
	gh := &fakeGH{
		listIssues: []ghclient.Issue{iss},
		getIssue:   map[int]ghclient.Issue{iss.Number: iss},
	}
	now := t1
	a := newAdapter(t, gh, &captureLog{}, func() time.Time { return now })
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	wi, err := a.Get(context.Background(), "ITEM-590")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if wi.ID != "ITEM-590" {
		t.Fatalf("ID=%s want ITEM-590", wi.ID)
	}
}

// TestGitHubIssues_List_BodyEditChangesKey_ReprojectsWorkItem asserts dedup key recomputes on body edit.
func TestGitHubIssues_List_BodyEditChangesKey_ReprojectsWorkItem(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	k1 := computeDedupKey("trilamsr", "regatta", iss.Number, iss.Body)
	mutated := iss
	mutated.Body = iss.Body + "\nappended prose changes hash\n"
	k2 := computeDedupKey("trilamsr", "regatta", mutated.Number, mutated.Body)
	if k1 == k2 {
		t.Fatalf("expected dedup key drift on body edit; both = %s", k1)
	}
}

// TestGitHubIssues_PropertyTest_ProjectionDeterministic asserts byte-equal projections across runs.
func TestGitHubIssues_PropertyTest_ProjectionDeterministic(t *testing.T) {
	iss := loadFixture(t, "issue-590-valid.json")
	gh1 := &fakeGH{listIssues: []ghclient.Issue{iss}}
	gh2 := &fakeGH{listIssues: []ghclient.Issue{iss}}
	a1 := newAdapter(t, gh1, &captureLog{}, time.Now)
	a2 := newAdapter(t, gh2, &captureLog{}, time.Now)
	out1, _ := a1.List(context.Background())
	out2, _ := a2.List(context.Background())
	j1, _ := json.Marshal(out1)
	j2, _ := json.Marshal(out2)
	if string(j1) != string(j2) {
		t.Fatalf("projection drift:\n%s\n---\n%s", j1, j2)
	}
}

// FuzzGitHubIssues_ParseMetadata_NoPanic random bytes never panic the metadata extractor.
func FuzzGitHubIssues_ParseMetadata_NoPanic(f *testing.F) {
	f.Add([]byte("<!--regatta\nlane: x\n-->"))
	f.Add([]byte("garbage <!--regatta\n: : :\n-->"))
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _, _ = parseIssueBody(string(body), "")
	})
}

// FuzzGitHubIssues_ParseAcceptance_NoPanic random body bytes never panic the criteria parser.
func FuzzGitHubIssues_ParseAcceptance_NoPanic(f *testing.F) {
	f.Add([]byte("## Acceptance criteria\n- [planned] c1: x"))
	f.Add([]byte("## Acceptance criteria\n\n## Acceptance criteria\n"))
	f.Fuzz(func(_ *testing.T, body []byte) {
		_, _, _ = parseCriteria(string(body))
	})
}

// TestGitHubIssues_NewGitHubIssues_RequiresClientAndRepo fails loud on missing required fields.
func TestGitHubIssues_NewGitHubIssues_RequiresClientAndRepo(t *testing.T) {
	if _, err := NewGitHubIssues(GitHubIssuesConfig{Repo: Repo{Owner: "o", Name: "n"}}); err == nil {
		t.Fatalf("expected error for nil Client")
	}
	if _, err := NewGitHubIssues(GitHubIssuesConfig{Client: &fakeGH{}}); err == nil {
		t.Fatalf("expected error for empty Repo")
	}
}
