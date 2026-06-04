package approval

import (
	"errors"
	"testing"
	"time"
)

// validBase returns a Config that passes every invariant V1-V11.
// Tests mutate one field and assert the named sentinel fires; that
// shape catches "fix one invariant by breaking another" regressions.
func validBase() Config {
	return Config{
		Name:           "deploy-gate",
		RiskClass:      RiskLow,
		Reviewers:      []string{"alice", "bob"},
		Quorum:         1,
		Timeout:        2 * time.Hour,
		DecisionWindow: 1 * time.Hour,
		OnTimeout:      OnTimeoutFail,
	}
}

func TestConfig_Validate_HappyPath(t *testing.T) {
	if err := validBase().Validate(); err != nil {
		t.Fatalf("happy-path Validate: %v", err)
	}
}

func TestConfig_ValidateAllInvariants(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr error
	}{
		{
			name:    "V1_DecisionWindowZero",
			mutate:  func(c *Config) { c.DecisionWindow = 0 },
			wantErr: ErrInvalidDecisionWindow,
		},
		{
			name:    "V1_DecisionWindowNegative",
			mutate:  func(c *Config) { c.DecisionWindow = -1 * time.Second },
			wantErr: ErrInvalidDecisionWindow,
		},
		{
			name:    "V2_TimeoutZero",
			mutate:  func(c *Config) { c.Timeout = 0; c.DecisionWindow = 0 },
			wantErr: ErrInvalidTimeout,
		},
		{
			name: "V3_WindowExceedsTimeout",
			mutate: func(c *Config) {
				c.Timeout = 1 * time.Hour
				c.DecisionWindow = 2 * time.Hour
			},
			wantErr: ErrWindowExceedsTimeout,
		},
		{
			name: "V4_ChainTierWindowExceedsTimeout",
			mutate: func(c *Config) {
				c.OnTimeout = OnTimeoutEscalate
				c.EscalationChain = []TierConfig{{
					Reviewers:      []string{"oncall"},
					Quorum:         1,
					Timeout:        1 * time.Hour,
					DecisionWindow: 2 * time.Hour,
				}}
			},
			wantErr: ErrWindowExceedsTimeout,
		},
		{
			name: "V5_AutoApproveRequiresLowRisk_high",
			mutate: func(c *Config) {
				c.OnTimeout = OnTimeoutAutoApprove
				c.RiskClass = RiskHigh
			},
			wantErr: ErrAutoApproveRequiresLowRisk,
		},
		{
			name: "V5_AutoApproveRequiresLowRisk_medium",
			mutate: func(c *Config) {
				c.OnTimeout = OnTimeoutAutoApprove
				c.RiskClass = RiskMedium
			},
			wantErr: ErrAutoApproveRequiresLowRisk,
		},
		{
			name:    "V6_InvalidRiskClass",
			mutate:  func(c *Config) { c.RiskClass = "ultra" },
			wantErr: ErrInvalidRiskClass,
		},
		{
			name:    "V7_QuorumZero",
			mutate:  func(c *Config) { c.Quorum = 0 },
			wantErr: ErrQuorumExceedsReviewers,
		},
		{
			name:    "V7_QuorumExceedsReviewers",
			mutate:  func(c *Config) { c.Quorum = 5 },
			wantErr: ErrQuorumExceedsReviewers,
		},
		{
			name:    "V8_InvalidOnTimeout",
			mutate:  func(c *Config) { c.OnTimeout = "panic" },
			wantErr: ErrInvalidOnTimeout,
		},
		{
			name: "V9_EscalateRequiresChain",
			mutate: func(c *Config) {
				c.OnTimeout = OnTimeoutEscalate
				c.EscalationChain = nil
			},
			wantErr: ErrEscalateMissingChain,
		},
		{
			name:    "V10_InvalidReviewerID_space",
			mutate:  func(c *Config) { c.Reviewers = []string{"alice smith"} },
			wantErr: ErrInvalidReviewerID,
		},
		{
			name:    "V10_InvalidReviewerID_unicode",
			mutate:  func(c *Config) { c.Reviewers = []string{"élise"} },
			wantErr: ErrInvalidReviewerID,
		},
		{
			name:    "V10_InvalidReviewerID_emptyString",
			mutate:  func(c *Config) { c.Reviewers = []string{""} },
			wantErr: ErrInvalidReviewerID,
		},
		{
			name:    "V11_InvalidGateName_slash",
			mutate:  func(c *Config) { c.Name = "deploy/gate" },
			wantErr: ErrInvalidGateName,
		},
		{
			name:    "V11_InvalidGateName_empty",
			mutate:  func(c *Config) { c.Name = "" },
			wantErr: ErrInvalidGateName,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := validBase()
			tc.mutate(&c)
			err := c.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate err=%v; want errors.Is(%v)", err, tc.wantErr)
			}
		})
	}
}

// TestConfig_Validate_AccumulatesErrors asserts Validate joins every defect (two sentinels in one error) so a single CI run surfaces every misconfig.
func TestConfig_Validate_AccumulatesErrors(t *testing.T) {
	c := validBase()
	c.Quorum = 99
	c.OnTimeout = "panic"
	err := c.Validate()
	if !errors.Is(err, ErrQuorumExceedsReviewers) {
		t.Errorf("expected ErrQuorumExceedsReviewers in joined err; got %v", err)
	}
	if !errors.Is(err, ErrInvalidOnTimeout) {
		t.Errorf("expected ErrInvalidOnTimeout in joined err; got %v", err)
	}
}

// TestConfig_Validate_RolesCountTowardQuorum asserts roles-only reviewer sets count toward quorum via union of reviewers + role-expansion (opaque names when no resolver).
func TestConfig_Validate_RolesCountTowardQuorum(t *testing.T) {
	c := Config{
		Name:           "g1",
		RiskClass:      RiskLow,
		Reviewers:      nil,
		Roles:          []string{"oncall:server"},
		Quorum:         1,
		Timeout:        2 * time.Hour,
		DecisionWindow: 1 * time.Hour,
		OnTimeout:      OnTimeoutFail,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("roles-only Validate: %v", err)
	}
}

func TestConfig_Validate_EmptyReviewerSetFails(t *testing.T) {
	c := validBase()
	c.Reviewers = nil
	c.Roles = nil
	err := c.Validate()
	if !errors.Is(err, ErrQuorumExceedsReviewers) {
		t.Fatalf("err=%v; want ErrQuorumExceedsReviewers (empty set)", err)
	}
}
