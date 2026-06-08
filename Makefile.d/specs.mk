# Specs index generator. Owned by docs/engineer/specs corpus.
# The generated README.md is gitignored — regenerate locally to browse.
.PHONY: specs-index specs-index-test

specs-index:  ## Regenerate docs/engineer/specs/README.md locally (gitignored; not tracked). Idempotent.
	bash scripts/gen-specs-readme.sh

specs-index-test:  ## Assert specs-index generator: determinism, status grouping, frontmatter title override, stale detection.
	bash scripts/gen-specs-readme_test.sh
