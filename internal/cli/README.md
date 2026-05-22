# internal/cli/

CLI subcommand wiring lives here once extracted from `cmd/regatta/main.go`.

Activation trigger: `cmd/regatta/main.go` exceeds 600 LOC OR a new
subcommand lands. Until then, subcommands are wired directly in
`main.go` (currently within spec size cap C7).
