package alarmwebhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// genAlertname draws printable ASCII alertnames in the shape Prometheus
// recommends: word chars only. A purely-random alertname is rejected
// upstream so the receiver never sees it; the generator pins the
// realistic shape.
func genAlertname() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z][A-Za-z0-9_]{0,32}`)
}

func genSeverity() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"critical", "warning", "info", "page"})
}

func genPayload() *rapid.Generator[[]byte] {
	return rapid.Custom(func(t *rapid.T) []byte {
		name := genAlertname().Draw(t, "alertname")
		sev := genSeverity().Draw(t, "severity")
		p := Payload{
			Status:       "firing",
			ExternalURL:  "https://alertmanager.example.com",
			CommonLabels: map[string]string{"alertname": name, "severity": sev},
			Alerts: []Alert{{
				Status: "firing",
				Labels: map[string]string{
					"alertname": name,
					"severity":  sev,
				},
				Annotations: map[string]string{
					"summary":        "summary " + name,
					"description":    "desc " + name,
					"threshold":      "0.05",
					"current_value":  "0.10",
					"dashboard_url":  "https://grafana.example.com/d/" + name,
					"replay_command": "regatta replay --substrate-fixture " + name,
				},
				StartsAt: "2026-06-02T00:00:00Z",
			}},
		}
		buf, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return buf
	})
}

func postRapid(rt *rapid.T, h http.Handler, body []byte) *httptest.ResponseRecorder {
	rt.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set(WebhookAuthHeader, "property-test-token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// TestAlarmWebhook_PropertyTest_NoDoubleIssueOnReplay asserts no random AlertManager payload produces two CreateIssue calls when replayed twice.
func TestAlarmWebhook_PropertyTest_NoDoubleIssueOnReplay(t *testing.T) {
	t.Setenv(WebhookAuthEnv, "property-test-token")
	rapid.Check(t, func(rt *rapid.T) {
		body := genPayload().Draw(rt, "payload")
		fake := &fakeGitHub{}
		h := &Handler{Client: fake}

		rr1 := postRapid(rt, h, body)
		if rr1.Code/100 != 2 {
			rt.Fatalf("first firing status %d body=%s", rr1.Code, rr1.Body.String())
		}
		if len(fake.Created) != 1 {
			rt.Fatalf("first firing: creates=%d want 1", len(fake.Created))
		}

		rr2 := postRapid(rt, h, body)
		if rr2.Code/100 != 2 {
			rt.Fatalf("second firing status %d body=%s", rr2.Code, rr2.Body.String())
		}
		if len(fake.Created) != 1 {
			rt.Fatalf("second firing: creates=%d want still 1 (replay must dedup)", len(fake.Created))
		}
		if len(fake.Comments) != 1 {
			rt.Fatalf("second firing: comments=%d want 1", len(fake.Comments))
		}
		if !strings.Contains(fake.Comments[0].Body, "Refiring") {
			rt.Fatalf("comment body missing refiring marker: %s", fake.Comments[0].Body)
		}
	})
}
