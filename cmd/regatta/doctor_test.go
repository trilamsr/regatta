package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/secrets"
)

// TestDoctor_AllPass_ExitsZero asserts exit 0 when every injected probe returns PASS (#910).
func TestDoctor_AllPass_ExitsZero(t *testing.T) {
	env := allPassEnv()
	var out, errBuf bytes.Buffer
	code := runDoctorTo(&out, &errBuf, []string{}, env)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, errBuf.String(), out.String())
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Fatalf("expected PASS markers in human output; got:\n%s", out.String())
	}
}

// TestDoctor_SecretMissing_ReportsAdapterChain asserts a missing secret surfaces the tried adapter + canonical key hint (#910).
func TestDoctor_SecretMissing_ReportsAdapterChain(t *testing.T) {
	env := allPassEnv()
	env.secretLookup = func(ctx context.Context, key string) (string, error) {
		return "env→keychain", errors.New("not found in env→keychain chain")
	}
	var out, errBuf bytes.Buffer
	code := runDoctorTo(&out, &errBuf, []string{}, env)
	if code != 1 {
		t.Fatalf("expected exit=1 on missing secret, got %d; stdout=%s", code, out.String())
	}
	body := out.String()
	if !strings.Contains(body, secrets.KeyAnthropic) {
		t.Fatalf("missing-secret hint should name canonical key %q; got:\n%s", secrets.KeyAnthropic, body)
	}
	if !strings.Contains(body, "env→keychain") {
		t.Fatalf("missing-secret hint should name the adapter chain; got:\n%s", body)
	}
}

// TestDoctor_JSONOutput_StableSchema pins the JSON envelope shape downstream CI tooling depends on (#910).
func TestDoctor_JSONOutput_StableSchema(t *testing.T) {
	env := allPassEnv()
	var out, errBuf bytes.Buffer
	code := runDoctorTo(&out, &errBuf, []string{"--json"}, env)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	var got struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Hint   string `json:"hint,omitempty"`
			Error  string `json:"error,omitempty"`
		} `json:"checks"`
		Summary struct {
			Pass int `json:"pass"`
			Fail int `json:"fail"`
			Skip int `json:"skip"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json envelope unparseable: %v\nbody=%s", err, out.String())
	}
	if len(got.Checks) == 0 {
		t.Fatalf("json envelope reports zero checks")
	}
	if got.Summary.Pass+got.Summary.Fail+got.Summary.Skip != len(got.Checks) {
		t.Fatalf("summary totals %d/%d/%d do not sum to %d checks",
			got.Summary.Pass, got.Summary.Fail, got.Summary.Skip, len(got.Checks))
	}
	wantNames := map[string]bool{
		"secrets": false, "binaries": false, "gh-auth": false,
		"git-state": false, "config": false, "branch-protection": false,
		"supervisor": false,
	}
	for _, c := range got.Checks {
		if _, ok := wantNames[c.Name]; ok {
			wantNames[c.Name] = true
		}
		switch c.Status {
		case "PASS", "FAIL", "SKIP":
		default:
			t.Fatalf("check %q has unknown status %q", c.Name, c.Status)
		}
	}
	for name, seen := range wantNames {
		if !seen {
			gotNames := make([]string, 0, len(got.Checks))
			for _, c := range got.Checks {
				gotNames = append(gotNames, c.Name)
			}
			t.Fatalf("expected check %q in json envelope; got names=%v", name, gotNames)
		}
	}
}

// TestDoctor_SkipFlag_OmitsCheck asserts --skip <name> drops the named row from JSON output (#910).
func TestDoctor_SkipFlag_OmitsCheck(t *testing.T) {
	env := allPassEnv()
	var out, errBuf bytes.Buffer
	code := runDoctorTo(&out, &errBuf, []string{"--json", "--skip", "branch-protection"}, env)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	var got struct {
		Checks []struct {
			Name string `json:"name"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json envelope unparseable: %v", err)
	}
	for _, c := range got.Checks {
		if c.Name == "branch-protection" {
			t.Fatalf("--skip branch-protection still emitted the row; checks=%v", got.Checks)
		}
	}
}

func allPassEnv() doctorEnv {
	return doctorEnv{
		secretLookup: func(ctx context.Context, key string) (string, error) {
			return "env", nil
		},
		lookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		runCmd: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("ok"), nil
		},
		gitState: func(ctx context.Context) (gitStateReport, error) {
			return gitStateReport{Branch: "main", Clean: true, RemoteReachable: true}, nil
		},
		validateConfig:    func(path string) error { return nil },
		verifyRepoConfig:  func(ctx context.Context) (bool, []string, error) { return true, nil, nil },
		supervisorPresent: func() (bool, string, error) { return false, "no install-service marker", nil },
		toolPins:          []string{"osv-scanner", "gitleaks"},
	}
}
