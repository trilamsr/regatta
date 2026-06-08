# Top-level Makefile. Per-feature targets live in Makefile.d/*.mk; this file
# only defines the default goal and glob-includes the per-feature files.
# Glob-include means new gates/checks/targets land as a NEW Makefile.d/*.mk
# file — siblings touching unrelated .mk files never cascade-rebase here
# (memory/feedback_cascade_rebase_root_cause).
#
# Auto-load contract (#987): ANY file ending `.mk` under Makefile.d/
# activates at every `make` invocation. Do NOT place test fixtures,
# scratch experiments, or partial/draft .mk files here — they will run
# their immediate-expansion variables and override targets silently.
# Scratch and experiments go under /tmp; test fixtures go under
# scripts/testdata/ (which is outside the glob).
#
# Include order is ALPHABETICAL (`agent.mk`, `boot-status.mk`, `build.mk`,
# `ci.mk`, ...). Targets resolve lazily so this is irrelevant for
# pure-recipe .mk files, but immediate-expansion variables (`:=`) that
# cross-reference siblings will silently break on reorder — define such
# variables in their own .mk and reference them via `$(VAR)` deferred
# expansion only.
.DEFAULT_GOAL := help

include $(wildcard Makefile.d/*.mk)
