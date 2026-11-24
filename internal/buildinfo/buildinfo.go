// Package buildinfo guarda a versão do binário. O goreleaser preenche as três
// variáveis com -ldflags -X; em `go install ...@v0.1.0` (sem ldflags) a versão
// vem do que o próprio Go gravou no binário.
package buildinfo

import (
	"regexp"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info devolve versão, commit e data, completando o que faltar com o
// debug.ReadBuildInfo quando o binário não foi montado pelo goreleaser.
func Info() (version, commit, date string) {
	bi, ok := debug.ReadBuildInfo()
	return resolve(Version, Commit, Date, bi, ok)
}

// V é só a versão, para comparar com a do daemon.
func V() string {
	v, _, _ := Info()
	return v
}

var pseudo = regexp.MustCompile(`-\d{14}-[0-9a-f]{12}`)

func resolve(version, commit, date string, bi *debug.BuildInfo, ok bool) (string, string, string) {
	if version != "dev" || !ok || bi == nil {
		return version, commit, date
	}
	// "(devel)" e pseudo-versões (v0.0.0-2026...-abc123, com ou sem +dirty) são
	// de compilação local ou de commit sem tag: ficam "dev", senão cada rebuild
	// pareceria uma versão nova e a TUI reiniciaria o daemon a todo momento.
	if v := bi.Main.Version; v != "" && v != "(devel)" && !pseudo.MatchString(v) {
		version = v
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
				if len(commit) > 7 {
					commit = commit[:7]
				}
			}
		case "vcs.time":
			if date == "" {
				date = s.Value
			}
		}
	}
	return version, commit, date
}
