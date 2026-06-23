package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
)

// versionCommit + versionDate are populated by `-ldflags` at release build
// time. Local builds fall through to runtime/debug.ReadBuildInfo so the
// CI/operator audit chain (`regatta version --json | jq .commit`) still
// returns the source commit when ldflags are absent (R-MEGA-3 LIVE-16).
var (
	versionCommit string
	versionDate   string
)

type versionReport struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// versionJSONFlag matches both --json (long form) and -json (single-dash form
// Go's flag package accepts identically). Kept as a const so goconst stays
// quiet and a future flag-suite refactor can pin the literal in one place.
const versionJSONFlag = "--json"

func emitVersion(args []string) {
	asJSON := false
	for _, a := range args {
		if a == versionJSONFlag || a == "-json" {
			asJSON = true
		}
	}
	rep := versionReport{Version: version, Commit: versionCommit, Date: versionDate}
	if rep.Commit == "" || rep.Date == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if rep.Commit == "" && s.Key == "vcs.revision" {
					rep.Commit = s.Value
				}
				if rep.Date == "" && s.Key == "vcs.time" {
					rep.Date = s.Value
				}
			}
		}
	}
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(rep)
		return
	}
	fmt.Println("regatta", rep.Version)
}
