package docker_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRootForHardening(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// composeServiceBlocks splits docker-compose.yml service stanzas keyed by
// service name. A "service" is a top-level key under `services:` at the
// 2-space indent. The returned map preserves the raw text per service so
// each hardening assertion greps the right scope without re-parsing YAML.
func composeServiceBlocks(t *testing.T) map[string]string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootForHardening(t), "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	src := string(body)
	servicesIdx := strings.Index(src, "\nservices:\n")
	if servicesIdx < 0 {
		t.Fatalf("docker-compose.yml missing top-level services: block")
	}
	rest := src[servicesIdx+len("\nservices:\n"):]
	if end := strings.Index(rest, "\nnetworks:\n"); end >= 0 {
		rest = rest[:end]
	}
	out := map[string]string{}
	serviceRE := regexp.MustCompile(`(?m)^  ([a-z][a-z0-9_-]*):\s*$`)
	idxs := serviceRE.FindAllStringSubmatchIndex(rest, -1)
	for i, m := range idxs {
		name := rest[m[2]:m[3]]
		start := m[0]
		end := len(rest)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		out[name] = rest[start:end]
	}
	return out
}

// TestCompose_AllServicesHaveMemLimit (R20-D1) gates that every long-running service in docker-compose.yml carries an explicit mem_limit so a runaway child cannot OOM the host.
func TestCompose_AllServicesHaveMemLimit(t *testing.T) {
	blocks := composeServiceBlocks(t)
	skip := map[string]bool{"regatta-init": true}
	for name, body := range blocks {
		if skip[name] {
			continue
		}
		if !strings.Contains(body, "\n    mem_limit:") {
			t.Errorf("service %q: missing mem_limit — runaway child can OOM the host", name)
		}
	}
}

// TestCompose_AllServicesHaveCPUs (R20-D1) gates that every long-running service carries an explicit cpus limit so CPU saturation in one service cannot starve siblings on a single-operator host.
func TestCompose_AllServicesHaveCPUs(t *testing.T) {
	blocks := composeServiceBlocks(t)
	skip := map[string]bool{"regatta-init": true}
	for name, body := range blocks {
		if skip[name] {
			continue
		}
		if !strings.Contains(body, "\n    cpus:") {
			t.Errorf("service %q: missing cpus limit", name)
		}
	}
}

// TestCompose_AlertmanagerHasHealthcheck (R20-D2) gates that alertmanager carries a healthcheck so prometheus's service_healthy dependency is resolvable rather than racing on service_started.
func TestCompose_AlertmanagerHasHealthcheck(t *testing.T) {
	blocks := composeServiceBlocks(t)
	body, ok := blocks["alertmanager"]
	if !ok {
		t.Fatalf("alertmanager service block not found")
	}
	if !strings.Contains(body, "\n    healthcheck:") {
		t.Errorf("alertmanager: missing healthcheck — prometheus depends_on service_healthy is unsatisfiable without it")
	}
}

// TestCompose_PrometheusDependsAlertmanagerHealthy (R20-D2) gates that prometheus waits for alertmanager to report healthy, not merely started, so it cannot boot against an alertmanager whose listener has not bound yet.
func TestCompose_PrometheusDependsAlertmanagerHealthy(t *testing.T) {
	blocks := composeServiceBlocks(t)
	body, ok := blocks["prometheus"]
	if !ok {
		t.Fatalf("prometheus service block not found")
	}
	depIdx := strings.Index(body, "\n    depends_on:")
	if depIdx < 0 {
		t.Fatalf("prometheus: missing depends_on block")
	}
	tail := body[depIdx:]
	if !strings.Contains(tail, "alertmanager:") {
		t.Fatalf("prometheus depends_on: missing alertmanager entry")
	}
	if strings.Contains(tail, "condition: service_started") {
		t.Errorf("prometheus depends_on alertmanager: condition is service_started — race-prone; use service_healthy")
	}
	if !strings.Contains(tail, "condition: service_healthy") {
		t.Errorf("prometheus depends_on alertmanager: missing condition: service_healthy")
	}
}

// TestCompose_PortsBoundToLocalhost (R20-D3) gates that every published port binds to 127.0.0.1 on this single-operator self-host stack so grafana admin:admin and prometheus metrics never leak to LAN.
func TestCompose_PortsBoundToLocalhost(t *testing.T) {
	blocks := composeServiceBlocks(t)
	bareRE := regexp.MustCompile(`(?m)^\s+- "(\d+):\d+"`)
	for name, body := range blocks {
		matches := bareRE.FindAllString(body, -1)
		if len(matches) > 0 {
			t.Errorf("service %q: bare port mappings %v — bind to 127.0.0.1 to keep single-operator stack off LAN", name, matches)
		}
	}
}

// TestCompose_LoggingDriverBounded (R20-D4) gates that every long-running service caps its json-file logs so the docker host's /var/lib/docker disk cannot fill from accumulated logs.
func TestCompose_LoggingDriverBounded(t *testing.T) {
	blocks := composeServiceBlocks(t)
	skip := map[string]bool{"regatta-init": true}
	for name, body := range blocks {
		if skip[name] {
			continue
		}
		if !strings.Contains(body, "\n    logging:") {
			t.Errorf("service %q: missing logging block — json-file driver is unbounded by default", name)
			continue
		}
		if !strings.Contains(body, "max-size:") {
			t.Errorf("service %q: logging missing max-size — unbounded", name)
		}
	}
}
