package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/trilamsr/regatta/internal/supervisor"
)

// runInstallService is the `regatta install-service` CLI entry point —
// spec §3.8 surface. Thin wrapper; all logic lives in
// internal/supervisor so the path is tested in isolation.
func runInstallService(args []string) int {
	return runInstallServiceTo(os.Stdout, os.Stderr, args)
}

// runInstallServiceTo splits the io.Writer seams off the runInstallService
// entry so tests can drive dry-run + flag parsing without process stdio.
func runInstallServiceTo(out, errW io.Writer, args []string) int {
	fs := flag.NewFlagSet("install-service", flag.ContinueOnError)
	fs.SetOutput(errW)
	user := fs.Bool("user", true, "install per-user agent (default)")
	system := fs.Bool("system", false, "install system unit (requires root)")
	dryRun := fs.Bool("dry-run", false, "render unit + plan, no filesystem writes")
	force := fs.Bool("force", false, "overwrite an existing differing unit (writes .bak)")
	noCron := fs.Bool("no-cron", false, "skip cron block install")
	healthzURL := fs.String("healthz-url", "", "override post-install /healthz polling URL (default http://127.0.0.1:8080/healthz; set when serve --addr binds a non-default port) (#667)")
	envFile := fs.String("env-file", "", "path to env file sourced before `regatta serve` exec. Default macOS user: $HOME/.config/regatta/env; macOS system + Linux: /etc/regatta/env. launchd has no native EnvironmentFile, so the macOS plist wraps the binary in /bin/sh -lc that sources this path (followup to #826).")
	name := fs.String("name", "", "per-target namespace suffix so multiple regatta installs coexist side-by-side (spec #929). Empty preserves the single-target `regatta` namespace. Example: --name myrepo → launchd Label com.regatta.serve.myrepo, systemd UnitName regatta-myrepo.service, paths under <base>/regatta/myrepo/.")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name != "" {
		if err := supervisor.ValidateName(*name); err != nil {
			_, _ = fmt.Fprintln(errW, "install-service:", err)
			return 2
		}
	}
	_ = *user // --user is the documented default; flag tracked for help text + future opt-out
	mode := supervisor.ModeUser
	if *system {
		mode = supervisor.ModeSystem
		if os.Geteuid() != 0 {
			_, _ = fmt.Fprintln(errW, "install-service --system requires root")
			return 1
		}
	}
	opts := supervisor.Options{
		Mode:       mode,
		DryRun:     *dryRun,
		Force:      *force,
		NoCron:     *noCron,
		Name:       *name,
		HealthzURL: *healthzURL,
		EnvFile:    *envFile,
		Out:        out,
		Err:        errW,
	}
	if err := supervisor.Install(opts); err != nil {
		_, _ = fmt.Fprintln(errW, "install-service:", err)
		return 1
	}
	return 0
}
