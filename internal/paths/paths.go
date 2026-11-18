// Package paths centraliza onde o zapterm guarda dados e onde fica o socket.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDir devolve ~/.local/share/zapterm (respeitando XDG_DATA_HOME) e cria
// a pasta com permissão 0700: ela guarda a sessão do WhatsApp e as mensagens.
func DataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "zapterm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SessionDB é o banco do whatsmeow (chaves de criptografia, contatos).
func SessionDB() (string, error) { return inData("session.db") }

// MessagesDB é o banco do zapterm (chats e mensagens).
func MessagesDB() (string, error) { return inData("zapterm.db") }

// Log é o arquivo de log do daemon.
func Log() (string, error) { return inData("daemon.log") }

func inData(name string) (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// CacheDir devolve ~/.cache/zapterm (respeitando XDG_CACHE_HOME). Tudo
// aqui pode ser apagado: é baixado de novo quando preciso.
func CacheDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "zapterm")
	return dir, os.MkdirAll(dir, 0o700)
}

// MediaDir guarda fotos, vídeos e documentos baixados.
func MediaDir() (string, error) { return cacheSub("media") }

// AvatarDir guarda fotos de perfil.
func AvatarDir() (string, error) { return cacheSub("avatars") }

func cacheSub(name string) (string, error) {
	base, err := CacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, name)
	return dir, os.MkdirAll(dir, 0o700)
}

// Socket fica em $XDG_RUNTIME_DIR; no Void sem systemd essa variável pode
// faltar, então cai para /tmp/zapterm-<uid>.sock.
func Socket() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "zapterm.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("zapterm-%d.sock", os.Getuid()))
}
