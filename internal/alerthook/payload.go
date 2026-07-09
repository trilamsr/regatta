// Package alerthook receives AlertManager webhook payloads and routes
// each firing alert to GitHub-issue create or comment-on-existing per the
// PHASE-AUTONOMY-W1 dedup rule. Both cmd/regatta-alarm-webhook (sidecar)
// and cmd/regatta serve (in-process via W3 supervisor) import this
// package; the handler stays transport-agnostic.
package alerthook

// Payload mirrors the AlertManager v4 webhook contract documented at
// https://prometheus.io/docs/alerting/latest/configuration/#webhook_config.
// Only the fields the receiver reads land in the struct; the rest stays
// JSON-tolerated via omitempty + extra-field-ignored decoding so an
// upstream-added field never breaks the receiver.
type Payload struct {
	Version           string            `json:"version,omitempty"`
	GroupKey          string            `json:"groupKey,omitempty"`
	TruncatedAlerts   int               `json:"truncatedAlerts,omitempty"`
	Status            string            `json:"status,omitempty"`
	Receiver          string            `json:"receiver,omitempty"`
	GroupLabels       map[string]string `json:"groupLabels,omitempty"`
	CommonLabels      map[string]string `json:"commonLabels,omitempty"`
	CommonAnnotations map[string]string `json:"commonAnnotations,omitempty"`
	ExternalURL       string            `json:"externalURL,omitempty"`
	Alerts            []Alert           `json:"alerts,omitempty"`
}

// Alert is one entry of Payload.Alerts. The receiver dedups on
// labels.alertname; everything else feeds the issue body.
type Alert struct {
	Status       string            `json:"status,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	StartsAt     string            `json:"startsAt,omitempty"`
	EndsAt       string            `json:"endsAt,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
}

// Alertname returns labels.alertname, the dedup key. Empty when the
// upstream payload omits it (defensive — AlertManager always sets it).
func (a Alert) Alertname() string {
	if a.Labels == nil {
		return ""
	}
	return a.Labels["alertname"]
}

// Severity returns labels.severity, the third issue-label component.
// Defaults to "unknown" when the upstream omits it so the receiver
// never emits a label with an empty suffix.
func (a Alert) Severity() string {
	if a.Labels == nil {
		return "unknown"
	}
	if s := a.Labels["severity"]; s != "" {
		return s
	}
	return "unknown"
}
