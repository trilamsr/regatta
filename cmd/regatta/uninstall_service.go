package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/trilamsr/regatta/internal/supervisor"
)

// runUninstallService reverses install — idempotent per spec §3.8.
func runUninstallService(args []string) int {
	fs := flag.NewFlagSet("uninstall-service", flag.ContinueOnError)
	user := fs.Bool("user", true, "uninstall per-user agent (default)")
	system := fs.Bool("system", false, "uninstall system unit (requires root)")
	noCron := fs.Bool("no-cron", false, "skip cron block strip")
	name := fs.String("name", "", "per-target namespace suffix; must match the --name passed to install-service (#929). Empty targets the single-target `regatta` install.")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_ = *user
	mode := supervisor.ModeUser
	if *system {
		mode = supervisor.ModeSystem
	}
	opts := supervisor.Options{
		Mode:   mode,
		NoCron: *noCron,
		Name:   *name,
		Out:    os.Stdout,
		Err:    os.Stderr,
	}
	if err := supervisor.Uninstall(opts); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall-service:", err)
		return 1
	}
	return 0
}
