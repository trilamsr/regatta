# Specs index generator. Owned by docs/engineer/specs corpus.
.PHONY: specs-index specs-index-check specs-index-test

specs-index:  ## Regenerate docs/engineer/specs/README.md from spec frontmatter. Run after adding/editing a spec; idempotent.
	bash scripts/gen-specs-readme.sh

specs-index-check:  ## Fail if docs/engineer/specs/README.md is stale vs the spec corpus. CI gate.
	bash scripts/gen-specs-readme.sh --check

specs-index-test:  ## Assert specs-index generator: determinism, status grouping, frontmatter title override, stale detection.
	bash scripts/gen-specs-readme_test.sh
