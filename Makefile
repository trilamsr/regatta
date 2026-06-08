# Top-level Makefile. Per-feature targets live in Makefile.d/*.mk; this file
# only defines the default goal and glob-includes the per-feature files.
# Glob-include means new gates/checks/targets land as a NEW Makefile.d/*.mk
# file — siblings touching unrelated .mk files never cascade-rebase here
# (memory/feedback_cascade_rebase_root_cause).
.DEFAULT_GOAL := help

include $(wildcard Makefile.d/*.mk)
