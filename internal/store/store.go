// Package store guarda chats e mensagens do zapterm num SQLite próprio,
// separado da sessão do whatsmeow. O whatsmeow só entrega histórico no
// pareamento e eventos ao vivo; sem este banco a lista nasceria vazia a cada
// abertura.
package store

import (
	"database/sql"
	"fmt"
	"strings"

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
	// 2: edição e reações. Uma reação por pessoa por mensagem, como no app.
	`ALTER TABLE messages ADD COLUMN edited INTEGER NOT NULL DEFAULT 0;
	 CREATE TABLE reactions (
		chat   TEXT NOT NULL,
		msg_id TEXT NOT NULL,
		sender TEXT NOT NULL,
		emoji  TEXT NOT NULL,
		ts     INTEGER NOT NULL,
		PRIMARY KEY (chat, msg_id, sender)
	 );`,
	// 3: situação das mensagens enviadas (ver ipc.Message.Status).
	`ALTER TABLE messages ADD COLUMN status INTEGER NOT NULL DEFAULT 0;`,
	// 4: figurinhas favoritas, sincronizadas do celular pelo app state.
	`CREATE TABLE favorite_stickers (
		key          TEXT PRIMARY KEY,
		direct_path  TEXT NOT NULL,
		media_key    BLOB NOT NULL,
		file_enc_sha BLOB,
		file_sha     BLOB,
		mimetype     TEXT NOT NULL DEFAULT 'image/webp',
		width        INTEGER NOT NULL DEFAULT 0,
		height       INTEGER NOT NULL DEFAULT 0,
		file_length  INTEGER NOT NULL DEFAULT 0,
		lottie       INTEGER NOT NULL DEFAULT 0,
		ts           INTEGER NOT NULL
	 );`,
	// 5: favorita cujo arquivo expirou no servidor (403/404/410): não tenta
	// de novo até ser favoritada de novo (link novo).
	`ALTER TABLE favorite_stickers ADD COLUMN gone INTEGER NOT NULL DEFAULT 0;`,
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

// MarkReadUpTo marca como lidas as mensagens até a mais nova de ids (lidas
// em outro aparelho): o contador vira o número de mensagens recebidas
// depois dela. Devolve false se nenhuma das mensagens é conhecida.
func (s *Store) MarkReadUpTo(chat string, ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	args := []any{chat}
	marks := make([]string, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	var last sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(ts) FROM messages WHERE chat = ? AND id IN (`+strings.Join(marks, ",")+`)`,
		args...).Scan(&last); err != nil || !last.Valid {
		return false, err
	}
	_, err := s.db.Exec(`UPDATE chats SET unread = (
			SELECT COUNT(*) FROM messages WHERE chat = ? AND from_me = 0 AND ts > ? AND kind != 'revoked'
		) WHERE jid = ?`, chat, last.Int64, chat)
	return err == nil, err
}

// SetUnread põe o contador direto (marcar como não lida no celular).
func (s *Store) SetUnread(jid string, n int) error {
	_, err := s.db.Exec(`UPDATE chats SET unread = ? WHERE jid = ?`, n, jid)
	return err
}

func (s *Store) MarkRead(jid string) error {
	_, err := s.db.Exec(`UPDATE chats SET unread = 0 WHERE jid = ?`, jid)
	return err
}

func (s *Store) Chat(jid string) (ipc.Chat, error) {
	var c ipc.Chat
	err := s.db.QueryRow(`SELECT `+chatCols+` FROM chats WHERE jid = ?`, jid).
		Scan(&c.JID, &c.Name, &c.IsGroup, &c.LastTS, &c.LastText, &c.Unread, &c.LastStatus)
	return c, err
}

// Chats lista as conversas com atividade. O history sync também manda
// centenas de conversas vazias (contatos com quem você nunca falou ou que
// foram apagadas), sem timestamp; essas ficam de fora.
func (s *Store) Chats() ([]ipc.Chat, error) {
	rows, err := s.db.Query(`SELECT ` + chatCols + ` FROM chats WHERE last_ts > 0 ORDER BY last_ts DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Chat{}
	for rows.Next() {
		var c ipc.Chat
		if err := rows.Scan(&c.JID, &c.Name, &c.IsGroup, &c.LastTS, &c.LastText, &c.Unread, &c.LastStatus); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MediaMessages devolve as fotos e vídeos mais recentes do chat (para o
// perfil).
func (s *Store) MediaMessages(chat string, limit int) ([]ipc.Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages
		WHERE chat = ? AND kind IN ('image', 'video') ORDER BY ts DESC LIMIT ?`, chat, limit)
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

// MessagesAfter devolve até limit mensagens posteriores a after, em ordem
// cronológica (para descer a conversa depois de pular para uma busca).
func (s *Store) MessagesAfter(chat string, limit int, after int64) ([]ipc.Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE chat = ? AND ts > ?
		ORDER BY ts ASC LIMIT ?`, chat, after, limit)
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

// SearchMessages percorre as mensagens da mais nova para a mais antiga e
// devolve as primeiras limit em que match(texto) é verdadeiro. A
// comparação fica em Go para ignorar acento e maiúscula, o que o LIKE do
// SQLite não faz fora do ASCII.
func (s *Store) SearchMessages(match func(string) bool, limit int) ([]ipc.Message, error) {
	// Varre só chat, id e texto; a mensagem completa é lida só nos achados.
	rows, err := s.db.Query(`SELECT chat, id, text FROM messages
		WHERE kind NOT IN ('revoked', 'sticker', 'audio') AND text != '' ORDER BY ts DESC`)
	if err != nil {
		return nil, err
	}
	var keys [][2]string
	for rows.Next() && len(keys) < limit {
		var chat, id, text string
		if err := rows.Scan(&chat, &id, &text); err != nil {
			rows.Close()
			return nil, err
		}
		if match(text) {
			keys = append(keys, [2]string{chat, id})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]ipc.Message, 0, len(keys))
	for _, k := range keys {
		if m, _, err := s.Message(k[0], k[1]); err == nil {
			out = append(out, m)
		}
	}
	return out, nil
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

const msgCols = `id, chat, sender, sender_name, from_me, ts, kind, text, quote_id, quote_sender, quote_text, raw IS NOT NULL, edited, status`

func scanMessage(r interface{ Scan(...any) error }) (ipc.Message, error) {
	var m ipc.Message
	var q ipc.Quote
	err := r.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &q.ID, &q.Sender, &q.Text, &m.HasRaw, &m.Edited, &m.Status)
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
	err := row.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &q.ID, &q.Sender, &q.Text, &m.HasRaw, &m.Edited, &m.Status, &raw)
	if q.ID != "" {
		m.Quote = &q
	}
	return m, raw, err
}

// UpdateMessage troca tipo e texto de uma mensagem (edição ou remoção) e,
// se ela for a última do chat, a prévia da lista também. Devolve false se a
// mensagem não existe no banco.
func (s *Store) UpdateMessage(chat, id, kind, text string, edited bool) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE messages SET kind = ?, text = ?, edited = ? WHERE chat = ? AND id = ?`,
		kind, text, edited, chat, id)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, tx.Commit()
	}
	var m ipc.Message
	var isGroup bool
	err = tx.QueryRow(`SELECT m.sender_name, m.from_me, m.ts, c.is_group FROM messages m
		JOIN chats c ON c.jid = m.chat WHERE m.chat = ? AND m.id = ?`, chat, id).
		Scan(&m.SenderName, &m.FromMe, &m.TS, &isGroup)
	if err == nil {
		m.Chat, m.Text = chat, text
		if _, err := tx.Exec(`UPDATE chats SET last_text = ? WHERE jid = ? AND last_ts = ?`,
			preview(m, isGroup), chat, m.TS); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// chatCols inclui a situação da última mensagem, se for sua (-1 se não).
const chatCols = `jid, name, is_group, last_ts, last_text, unread,
	COALESCE((SELECT m.status FROM messages m WHERE m.chat = chats.jid AND m.ts = chats.last_ts
		AND m.from_me = 1 LIMIT 1), -1)`

// SetStatus sobe a situação de mensagens suas; nunca desce (um "entregue"
// atrasado não apaga um "lida").
func (s *Store) SetStatus(chat string, ids []string, status int) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{status, chat}
	marks := make([]string, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.Exec(`UPDATE messages SET status = MAX(status, ?)
		WHERE chat = ? AND from_me = 1 AND id IN (`+strings.Join(marks, ",")+`)`, args...)
	return err
}

// FavoriteSticker é uma figurinha da biblioteca do celular: o suficiente
// para baixar (e reenviar) sem uma mensagem de origem.
type FavoriteSticker struct {
	Key                 string
	DirectPath          string
	MediaKey            []byte
	FileEncSHA, FileSHA []byte
	Mimetype            string
	Width, Height       int
	FileLength          int64
	Lottie              bool
	TS                  int64
	Gone                bool // expirou no servidor
}

func (s *Store) PutFavorite(f FavoriteSticker) error {
	_, err := s.db.Exec(`INSERT INTO favorite_stickers
		(key, direct_path, media_key, file_enc_sha, file_sha, mimetype, width, height, file_length, lottie, ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET direct_path = excluded.direct_path, media_key = excluded.media_key,
			file_enc_sha = excluded.file_enc_sha, file_sha = excluded.file_sha, mimetype = excluded.mimetype,
			width = excluded.width, height = excluded.height, file_length = excluded.file_length,
			lottie = excluded.lottie, ts = MAX(ts, excluded.ts),
			gone = CASE WHEN excluded.direct_path != favorite_stickers.direct_path THEN 0 ELSE gone END`,
		f.Key, f.DirectPath, f.MediaKey, f.FileEncSHA, f.FileSHA, f.Mimetype, f.Width, f.Height, f.FileLength, f.Lottie, f.TS)
	return err
}

func (s *Store) RemoveFavorite(key string) error {
	_, err := s.db.Exec(`DELETE FROM favorite_stickers WHERE key = ?`, key)
	return err
}

// MarkFavoriteGone marca a favorita como expirada no servidor.
func (s *Store) MarkFavoriteGone(key string) error {
	_, err := s.db.Exec(`UPDATE favorite_stickers SET gone = 1 WHERE key = ?`, key)
	return err
}

// Favorites devolve a biblioteca toda (com as expiradas marcadas em Gone),
// a favoritada mais recentemente primeiro. Manter as expiradas na lista
// deixa as posições estáveis para a paginação do seletor.
func (s *Store) Favorites() ([]FavoriteSticker, error) {
	rows, err := s.db.Query(`SELECT key, direct_path, media_key, file_enc_sha, file_sha, mimetype,
		width, height, file_length, lottie, ts, gone FROM favorite_stickers ORDER BY ts DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FavoriteSticker
	for rows.Next() {
		var f FavoriteSticker
		if err := rows.Scan(&f.Key, &f.DirectPath, &f.MediaKey, &f.FileEncSHA, &f.FileSHA, &f.Mimetype,
			&f.Width, &f.Height, &f.FileLength, &f.Lottie, &f.TS, &f.Gone); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) Favorite(key string) (FavoriteSticker, error) {
	var f FavoriteSticker
	err := s.db.QueryRow(`SELECT key, direct_path, media_key, file_enc_sha, file_sha, mimetype,
		width, height, file_length, lottie, ts FROM favorite_stickers WHERE key = ?`, key).
		Scan(&f.Key, &f.DirectPath, &f.MediaKey, &f.FileEncSHA, &f.FileSHA, &f.Mimetype,
			&f.Width, &f.Height, &f.FileLength, &f.Lottie, &f.TS)
	return f, err
}

// SetReaction grava (ou, com emoji vazio, remove) a reação de sender.
func (s *Store) SetReaction(chat, msgID, sender, emoji string, ts int64) error {
	if emoji == "" {
		_, err := s.db.Exec(`DELETE FROM reactions WHERE chat = ? AND msg_id = ? AND sender = ?`, chat, msgID, sender)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO reactions (chat, msg_id, sender, emoji, ts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (chat, msg_id, sender) DO UPDATE SET emoji = excluded.emoji, ts = excluded.ts
		WHERE excluded.ts >= reactions.ts`, chat, msgID, sender, emoji, ts)
	return err
}

// Reactions agrupa as reações das mensagens pedidas, por emoji, na ordem
// em que o emoji apareceu primeiro. me marca as suas.
func (s *Store) Reactions(chat string, ids []string, me string) (map[string][]ipc.Reaction, error) {
	out := map[string][]ipc.Reaction{}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{me, chat}
	marks := make([]string, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.Query(`SELECT msg_id, emoji, COUNT(*), MAX(sender = ?), MIN(ts) FROM reactions
		WHERE chat = ? AND msg_id IN (`+strings.Join(marks, ",")+`)
		GROUP BY msg_id, emoji ORDER BY msg_id, MIN(ts)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var r ipc.Reaction
		var first int64
		if err := rows.Scan(&id, &r.Emoji, &r.Count, &r.Mine, &first); err != nil {
			return nil, err
		}
		out[id] = append(out[id], r)
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
