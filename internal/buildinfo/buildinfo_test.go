package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	bi := func(ver string) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main: debug.Module{Version: ver},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef"},
				{Key: "vcs.time", Value: "2026-11-23T10:00:00Z"},
			},
		}
	}
	casos := []struct {
		nome          string
		v, c, d       string
		info          *debug.BuildInfo
		ok            bool
		wantV, wantC  string
		wantDataVazia bool
	}{
		{"goreleaser vence", "1.2.3", "abc1234", "2026-01-01", bi("v9.9.9"), true, "1.2.3", "abc1234", false},
		{"go install usa o módulo", "dev", "", "", bi("v0.1.0"), true, "v0.1.0", "0123456", false},
		{"build local fica dev", "dev", "", "", bi("(devel)"), true, "dev", "0123456", false},
		{"pseudo-versão fica dev", "dev", "", "", bi("v0.0.0-20261124001627-11cae783ec6f+dirty"), true, "dev", "0123456", false},
		{"sem buildinfo", "dev", "", "", nil, false, "dev", "", true},
	}
	for _, c := range casos {
		v, cm, d := resolve(c.v, c.c, c.d, c.info, c.ok)
		if v != c.wantV || cm != c.wantC || (d == "") != c.wantDataVazia {
			t.Errorf("%s: got (%q, %q, %q)", c.nome, v, cm, d)
		}
	}
}
