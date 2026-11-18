// Package store guarda chats e mensagens do zapterm num SQLite próprio,
// separado da sessão do whatsmeow. O whatsmeow só entrega histórico no
// pareamento e eventos ao vivo; sem este banco a lista nasceria vazia a cada
// abertura.
package store

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

const schema = `
CREATE TABLE IF NOT EXISTS chats (
	jid       TEXT PRIMARY KEY,
	name      TEXT NOT NULL DEFAULT '',
	is_group  INTEGER NOT NULL DEFAULT 0,
	last_ts   INTEGER NOT NULL DEFAULT 0,
	last_text TEXT NOT NULL DEFAULT '',
	unread    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	chat        TEXT NOT NULL,
	id          TEXT NOT NULL,
	sender      TEXT NOT NULL,
	sender_name TEXT NOT NULL DEFAULT '',
	from_me     INTEGER NOT NULL,
	ts          INTEGER NOT NULL,
	kind        TEXT NOT NULL,
	text        TEXT NOT NULL,
	PRIMARY KEY (chat, id)
);
CREATE INDEX IF NOT EXISTS messages_chat_ts ON messages (chat, ts);
`

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	// WAL deixa a TUI futura ler enquanto o daemon escreve; busy_timeout evita
	// "database is locked" em rajadas de history sync.
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", path))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criando schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SaveMessage grava a mensagem (ignora duplicata) e atualiza o chat.
// incUnread conta como não lida; só vale para mensagem ao vivo de terceiros.
// Devolve true se a mensagem era nova.
func (s *Store) SaveMessage(m ipc.Message, isGroup, incUnread bool) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT OR IGNORE INTO messages
		(chat, id, sender, sender_name, from_me, ts, kind, text)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Chat, m.ID, m.Sender, m.SenderName, m.FromMe, m.TS, m.Kind, m.Text)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, tx.Commit()
	}

	unread := 0
	if incUnread {
		unread = 1
	}
	// Só troca a prévia se a mensagem for mais nova que a atual: o history
	// sync chega fora de ordem.
	_, err = tx.Exec(`INSERT INTO chats (jid, is_group, last_ts, last_text, unread)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET
			last_text = CASE WHEN excluded.last_ts >= chats.last_ts THEN excluded.last_text ELSE chats.last_text END,
			last_ts   = MAX(chats.last_ts, excluded.last_ts),
			unread    = chats.unread + excluded.unread,
			is_group  = excluded.is_group`,
		m.Chat, isGroup, m.TS, preview(m, isGroup), unread)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// UpsertChat registra nome e não lidas vindos do history sync.
func (s *Store) UpsertChat(jid, name string, isGroup bool, lastTS int64, unread int) error {
	_, err := s.db.Exec(`INSERT INTO chats (jid, name, is_group, last_ts, unread)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET
			name     = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
			is_group = excluded.is_group,
			last_ts  = MAX(chats.last_ts, excluded.last_ts),
			unread   = excluded.unread`,
		jid, name, isGroup, lastTS, unread)
	return err
}

func (s *Store) SetChatName(jid, name string) error {
	_, err := s.db.Exec(`UPDATE chats SET name = ? WHERE jid = ?`, name, jid)
	return err
}

func (s *Store) MarkRead(jid string) error {
	_, err := s.db.Exec(`UPDATE chats SET unread = 0 WHERE jid = ?`, jid)
	return err
}

func (s *Store) Chat(jid string) (ipc.Chat, error) {
	var c ipc.Chat
	err := s.db.QueryRow(`SELECT jid, name, is_group, last_ts, last_text, unread
		FROM chats WHERE jid = ?`, jid).
		Scan(&c.JID, &c.Name, &c.IsGroup, &c.LastTS, &c.LastText, &c.Unread)
	return c, err
}

// Chats lista as conversas com atividade. O history sync também manda
// centenas de conversas vazias (contatos com quem você nunca falou ou que
// foram apagadas), sem timestamp; essas ficam de fora.
func (s *Store) Chats() ([]ipc.Chat, error) {
	rows, err := s.db.Query(`SELECT jid, name, is_group, last_ts, last_text, unread
		FROM chats WHERE last_ts > 0 ORDER BY last_ts DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Chat{}
	for rows.Next() {
		var c ipc.Chat
		if err := rows.Scan(&c.JID, &c.Name, &c.IsGroup, &c.LastTS, &c.LastText, &c.Unread); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Messages devolve até limit mensagens anteriores a before (0 = as mais
// recentes), em ordem cronológica.
func (s *Store) Messages(chat string, limit int, before int64) ([]ipc.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if before <= 0 {
		before = 1<<62
	}
	rows, err := s.db.Query(`SELECT * FROM (
			SELECT id, chat, sender, sender_name, from_me, ts, kind, text
			FROM messages WHERE chat = ? AND ts < ?
			ORDER BY ts DESC LIMIT ?
		) ORDER BY ts ASC`, chat, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Message{}
	for rows.Next() {
		var m ipc.Message
		if err := rows.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func preview(m ipc.Message, isGroup bool) string {
	prefix := ""
	if m.FromMe {
		prefix = "você: "
	} else if isGroup && m.SenderName != "" {
		prefix = m.SenderName + ": "
	}
	return prefix + m.Text
}
