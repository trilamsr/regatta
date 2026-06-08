package schemas

import (
	"bufio"
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// RegattaV1CUE is the canonical CUE schema for regatta.yaml v1,
// embedded so internal/config/validate can compile it without a
// filesystem dependency at runtime. Built from regatta.v1.cue plus
// every regatta/*.cue file via alphabetical concat per #970 slice 1
// (option A in spec §3.3); CUE auto-merges same-package files at
// compile time, so order matters only for deterministic build output.
var RegattaV1CUE = mustConcatRegattaV1()

//go:embed regatta.v1.cue regatta/*.cue
var regattaV1FS embed.FS

func mustConcatRegattaV1() string {
	var paths []string
	if err := fs.WalkDir(regattaV1FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".cue") {
			return nil
		}
		paths = append(paths, p)
		return nil
	}); err != nil {
		panic("schemas: walk embed.FS: " + err.Error())
	}
	sort.Strings(paths)
	var b strings.Builder
	for i, p := range paths {
		data, err := regattaV1FS.ReadFile(p)
		if err != nil {
			panic("schemas: read " + p + ": " + err.Error())
		}
		if i == 0 {
			b.Write(data)
		} else {
			b.WriteString(stripPackageClause(string(data)))
		}
		if b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// stripPackageClause drops the leading `package <name>` line from a
// concatenated CUE source so multi-file embed concat does not emit
// duplicate package clauses (CUE rejects them). Per-file package
// declarations stay authoritative for `cue vet ./contracts/schemas/regatta/`
// callers — only the in-process embed concat sees the stripped form.
func stripPackageClause(src string) string {
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var b strings.Builder
	stripped := false
	for sc.Scan() {
		line := sc.Text()
		if !stripped {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "package ") {
				stripped = true
				continue
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
