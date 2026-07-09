# Test + benchmark + mutation targets. Hot — touched by every new property test.
.PHONY: go-check go-check-full property-test property-test-full crash-recovery-property-full mutation-test mutation-test-install bench cover

# Property test set — single source of truth for property-test + property-test-full.
PROPERTY_TESTS := TestListSpawnable_PropertyTopologicalReady|TestSubstrate_SupersedesCycleProperty|TestSubstrate_ReplayProtectionProperty|TestSchedulerCrashRecoveryProperty|TestSpendCrashRecoveryProperty|TestReaperCrashRecoveryProperty
PROPERTY_PKGS := ./internal/orchestrator/state/... ./internal/orchestrator/scheduler/... ./internal/cost/spend/... ./internal/gates/approval/...

# Race-detector cost is ~2x test wall-clock + ~2x compile. Apply only to
# packages whose PROD code uses concurrency primitives (sync.*, channels, go
# func, atomic.*). scripts/race-pkgs.txt is the curated allowlist. Nightly
# cross-platform-nightly.yml still runs `-race ./...` across the full tree to
# catch any race that slipped past the allowlist.
go-check:  ## Build and test every Go package, with `-race` only on packages in scripts/race-pkgs.txt. PHASE-S-RELAX: -short during self-host window; full race sweep via `make go-check-full`.
	@bash scripts/go-check.sh

go-check-full:  ## Full race sweep without -short, across ALL packages. Run weekly + before any tag. Nightly cross-platform-nightly.yml uses this. PHASE-S-RELAX restoration target — fold back into `go-check` at end of self-host phase (memory/feedback_gate_relaxation_phase_s).
	go build -buildvcs=false ./...
	go test -race ./...

# Single -p N invocation; peak RSS ~410MB measured locally.
property-test:  ## Run rapid property tests. PHASE-S-RELAX: 50 checks in CI/local; spec-mandated 200 via `make property-test-full`.
	go test -race -run '$(PROPERTY_TESTS)' $(PROPERTY_PKGS) -rapid.checks=50

property-test-full:  ## Full 200-check property sweep. Run weekly + before any tag. PHASE-S-RELAX restoration target — fold back into `property-test` at end of self-host phase (memory/feedback_gate_relaxation_phase_s).
	go test -race -run '$(PROPERTY_TESTS)' $(PROPERTY_PKGS) -rapid.checks=200

crash-recovery-property-full:  ## 2000-case crash-recovery property sweep. Nightly CI target; spec §3.4. ≤90s wallclock budget.
	go test -race -run 'TestSchedulerCrashRecoveryProperty' ./internal/orchestrator/scheduler/... -rapid.checks=2000 -timeout=5m

mutation-test-install:  ## Install pinned gremlins binary into $GOPATH/bin. Idempotent.
	go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

mutation-test: mutation-test-install  ## Run gremlins against cost + scheduler packages (spec §3.2 allowlist). Developer mode (no threshold enforcement); see scripts/mutation/run-gremlins.sh for env knobs.
	NO_THRESHOLD=1 bash scripts/mutation/run-gremlins.sh

bench:  ## Run benchmark corpus (scheduler.Tick, CycleCheck, ListSpawnable, BriefLoader.Sync, schemas.Verify, canon). ~30s total at -benchtime=3x.
	go test -run=^$$ -bench=. -benchmem -benchtime=3x \
		./internal/orchestrator/scheduler/... \
		./internal/orchestrator/state/... \
		./internal/program/... \
		./contracts/schemas/... \
		./internal/canon/...

cover:  ## Print cross-package coverage; useful before declaring "done".
	go test -coverpkg=./... -coverprofile=/tmp/regatta.cover ./...
	go tool cover -func=/tmp/regatta.cover | tail -30
