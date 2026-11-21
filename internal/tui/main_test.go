package tui

import (
	"os"
	"testing"
)

// Os testes não tocam o cache de quem roda (recentes de emoji, gravações,
// recortes): tudo vai para uma pasta temporária.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zapterm-test-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
