package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/trilamsr/regatta/internal/supervisor"
)

// runInstallService is the `regatta install-service` CLI entry point —
// spec §3.8 surface. Thin wrapper; all logic lives in
// internal/supervisor so the path is tested in isolation.
func runInstallService(args []string) int {
	fs := flag.NewFlagSet("install-service", flag.ContinueOnError)
	user := fs.Bool("user", true, "install per-user agent (default)")
	system := fs.Bool("system", false, "install system unit (requires root)")
	dryRun := fs.Bool("dry-run", false, "render unit + plan, no filesystem writes")
	force := fs.Bool("force", false, "overwrite an existing differing unit (writes .bak)")
	noCron := fs.Bool("no-cron", false, "skip cron block install")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_ = *user // --user is the documented default; flag tracked for help text + future opt-out
	mode := supervisor.ModeUser
	if *system {
		mode = supervisor.ModeSystem
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "install-service --system requires root")
			return 1
		}
	}
	opts := supervisor.Options{
		Mode:   mode,
		DryRun: *dryRun,
		Force:  *force,
		NoCron: *noCron,
		Out:    os.Stdout,
		Err:    os.Stderr,
	}
	if err := supervisor.Install(opts); err != nil {
		fmt.Fprintln(os.Stderr, "install-service:", err)
		return 1
	}
	return 0
}
