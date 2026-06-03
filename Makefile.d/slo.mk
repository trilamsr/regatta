# SLO compile + parity. Owned by observability wedge.
.PHONY: slo-compile slo-compile-test

slo-compile:  ## Compile every slo/*.yaml -> dashboards/prometheus/rules/ via pinned Sloth (tools/sloth/version). Deterministic; same input = byte-equal output (spec §9 R3).
	bash scripts/slo-compile.sh

slo-compile-test:  ## Assert pin file exists, every slo/*.yaml has a rendered rule, and re-compile is byte-deterministic.
	bash scripts/slo-compile_test.sh
