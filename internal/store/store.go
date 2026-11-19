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

// migrations rodam em ordem; PRAGMA user_version guarda quantas já rodaram.
var migrations = []string{
	// 1: citação e protobuf original. raw é o waE2E.Message serializado; só
	// mensagens recebidas depois desta versão têm, o history sync antigo não.
	`ALTER TABLE messages ADD COLUMN quote_id TEXT NOT NULL DEFAULT '';
	 ALTER TABLE messages ADD COLUMN quote_sender TEXT NOT NULL DEFAULT '';
	 ALTER TABLE messages ADD COLUMN quote_text TEXT NOT NULL DEFAULT '';
	 ALTER TABLE messages ADD COLUMN raw BLOB;`,
}

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migração %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

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
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SaveMessage grava a mensagem (ignora duplicata) e atualiza o chat.
// incUnread conta como não lida; só vale para mensagem ao vivo de terceiros.
// Devolve true se a mensagem era nova.
func (s *Store) SaveMessage(m ipc.Message, raw []byte, isGroup, incUnread bool) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var q ipc.Quote
	if m.Quote != nil {
		q = *m.Quote
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO messages
		(chat, id, sender, sender_name, from_me, ts, kind, text, quote_id, quote_sender, quote_text, raw)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Chat, m.ID, m.Sender, m.SenderName, m.FromMe, m.TS, m.Kind, m.Text, q.ID, q.Sender, q.Text, raw)
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
		before = 1 << 62
	}
	rows, err := s.db.Query(`SELECT * FROM (
			SELECT `+msgCols+`
			FROM messages WHERE chat = ? AND ts < ?
			ORDER BY ts DESC LIMIT ?
		) ORDER BY ts ASC`, chat, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const msgCols = `id, chat, sender, sender_name, from_me, ts, kind, text, quote_id, quote_sender, quote_text, raw IS NOT NULL`

func scanMessage(r interface{ Scan(...any) error }) (ipc.Message, error) {
	var m ipc.Message
	var q ipc.Quote
	err := r.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &q.ID, &q.Sender, &q.Text, &m.HasRaw)
	if q.ID != "" {
		m.Quote = &q
	}
	return m, err
}

// RecentStickers devolve chat, id e protobuf das figurinhas mais recentes
// que têm o original guardado (as únicas que dá para baixar e reenviar).
func (s *Store) RecentStickers(limit int) ([][3]any, error) {
	rows, err := s.db.Query(`SELECT chat, id, raw FROM messages
		WHERE kind = 'sticker' AND raw IS NOT NULL ORDER BY ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][3]any
	for rows.Next() {
		var chat, id string
		var raw []byte
		if err := rows.Scan(&chat, &id, &raw); err != nil {
			return nil, err
		}
		out = append(out, [3]any{chat, id, raw})
	}
	return out, rows.Err()
}

// Message busca uma mensagem e o protobuf original (nil se for do history
// sync antigo).
func (s *Store) Message(chat, id string) (ipc.Message, []byte, error) {
	var raw []byte
	row := s.db.QueryRow(`SELECT `+msgCols+`, raw FROM messages WHERE chat = ? AND id = ?`, chat, id)
	var m ipc.Message
	var q ipc.Quote
	err := row.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &q.ID, &q.Sender, &q.Text, &m.HasRaw, &raw)
	if q.ID != "" {
		m.Quote = &q
	}
	return m, raw, err
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
