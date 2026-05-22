# internal/audit/

Tamper-evident audit-log writer + sink integrations (S3 with
Object-Lock, syslog).

Activation trigger: first audit-sink write path lands. Spec
section 4 D3 fact lives in `docs/design.md` §Threat Model §Trust
boundaries.
