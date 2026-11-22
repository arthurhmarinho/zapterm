// Package store guarda chats e mensagens do zapterm num SQLite próprio,
// separado da sessão do whatsmeow. O whatsmeow só entrega histórico no
// pareamento e eventos ao vivo; sem este banco a lista nasceria vazia a cada
// abertura.
package store

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
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
	// 6: conversa trancada no celular (escondida aqui) e mensagens
	// temporárias (prazo da conversa e vencimento de cada mensagem).
	`ALTER TABLE chats ADD COLUMN locked INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE chats ADD COLUMN ephemeral INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE messages ADD COLUMN expire_ts INTEGER NOT NULL DEFAULT 0;`,
	// 7: marcas de tarefas que rodam uma vez só.
	`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');`,
	// 8: votos de enquete (o último de cada pessoa vale; options são os
	// SHA-256 das opções escolhidas, em hex, separados por vírgula).
	`CREATE TABLE poll_votes (
		chat TEXT NOT NULL, poll_id TEXT NOT NULL, voter TEXT NOT NULL,
		options TEXT NOT NULL DEFAULT '', ts INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (chat, poll_id, voter));`,
	// 9: mensagem favorita (estrela), sincronizada com o celular.
	`ALTER TABLE messages ADD COLUMN starred INTEGER NOT NULL DEFAULT 0;
	 CREATE INDEX messages_starred ON messages (starred) WHERE starred = 1;`,
	// 10: mensagens fixadas no topo da conversa (expires 0 = sem prazo).
	`CREATE TABLE pins (
		chat TEXT NOT NULL, msg_id TEXT NOT NULL, ts INTEGER NOT NULL DEFAULT 0,
		expires INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (chat, msg_id));`,
	// 11: recibo de cada pessoa nas suas mensagens (quando recebeu, leu,
	// ouviu; unix ms, 0 = ainda não).
	`CREATE TABLE receipts (
		chat TEXT NOT NULL, msg_id TEXT NOT NULL, user TEXT NOT NULL,
		delivered INTEGER NOT NULL DEFAULT 0, read INTEGER NOT NULL DEFAULT 0, played INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (chat, msg_id, user));`,
	// 12: mensagem temporária salva na conversa (não vence enquanto salva).
	`ALTER TABLE messages ADD COLUMN kept INTEGER NOT NULL DEFAULT 0;`,
	// 13: ID da mensagem no servidor (canais: reagir e marcar visto usam).
	`ALTER TABLE messages ADD COLUMN server_id INTEGER NOT NULL DEFAULT 0;`,
	// 14: conversa restrita (o dono limitou aparelhos conectados).
	`ALTER TABLE chats ADD COLUMN restricted INTEGER NOT NULL DEFAULT 0;`,
	// 15: central de mídia (galeria de todas as conversas) ordena por data
	// dentro de cada tipo.
	`CREATE INDEX messages_kind_ts ON messages (kind, ts);`,
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
		(chat, id, sender, sender_name, from_me, ts, kind, text, quote_id, quote_sender, quote_text, raw, expire_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Chat, m.ID, m.Sender, m.SenderName, m.FromMe, m.TS, m.Kind, m.Text, q.ID, q.Sender, q.Text, raw, m.Expires)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Já existia. Se veio sem o protobuf (histórico do pareamento) e
		// agora chegou com ele, guarda: a mídia passa a ter miniatura e
		// download. Nada mais muda.
		if len(raw) > 0 {
			if _, err := tx.Exec(`UPDATE messages SET raw = ? WHERE chat = ? AND id = ? AND raw IS NULL`,
				raw, m.Chat, m.ID); err != nil {
				return false, err
			}
		}
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
		Scan(&c.JID, &c.Name, &c.IsGroup, &c.LastTS, &c.LastText, &c.Unread, &c.Ephemeral, &c.Restricted, &c.LastStatus)
	return c, err
}

// Chats lista as conversas com atividade. O history sync também manda
// centenas de conversas vazias (contatos com quem você nunca falou ou que
// foram apagadas), sem timestamp; essas ficam de fora.
func (s *Store) Chats() ([]ipc.Chat, error) {
	// Trancadas no celular ficam escondidas aqui também.
	rows, err := s.db.Query(`SELECT ` + chatCols + ` FROM chats WHERE last_ts > 0 AND locked = 0 ORDER BY last_ts DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Chat{}
	for rows.Next() {
		var c ipc.Chat
		if err := rows.Scan(&c.JID, &c.Name, &c.IsGroup, &c.LastTS, &c.LastText, &c.Unread, &c.Ephemeral, &c.Restricted, &c.LastStatus); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// galleryWhere: filtro SQL de cada aba da galeria.
var galleryWhere = map[string]string{
	"fotos":  `kind = 'image'`,
	"videos": `kind = 'video'`,
	"docs":   `kind = 'document'`,
	"links":  `kind = 'text' AND (text LIKE '%http://%' OR text LIKE '%https://%' OR text LIKE '%www.%')`,
	"audios": `kind = 'audio'`,
}

// SetPollVote grava o voto de voter (vale o mais recente). hashes vazio =
// tirou o voto.
func (s *Store) SetPollVote(chat, pollID, voter string, hashes []string, ts int64) (bool, error) {
	res, err := s.db.Exec(`INSERT INTO poll_votes (chat, poll_id, voter, options, ts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (chat, poll_id, voter) DO UPDATE SET options = excluded.options, ts = excluded.ts
		WHERE excluded.ts >= poll_votes.ts`, chat, pollID, voter, strings.Join(hashes, ","), ts)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PollVotes devolve os votos de uma enquete: votante → hashes escolhidos.
func (s *Store) PollVotes(chat, pollID string) (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT voter, options FROM poll_votes WHERE chat = ? AND poll_id = ?`, chat, pollID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var voter, opts string
		if err := rows.Scan(&voter, &opts); err != nil {
			return nil, err
		}
		if opts != "" {
			out[voter] = strings.Split(opts, ",")
		}
	}
	return out, rows.Err()
}

// Receipt é o recibo de uma pessoa numa mensagem sua.
type Receipt struct {
	User                    string
	Delivered, Read, Played int64
}

// AddReceipt anota quando user recebeu (2), leu (3) ou ouviu (4) a
// mensagem; o primeiro horário de cada etapa vale.
func (s *Store) AddReceipt(chat, msgID, user string, status int, ts int64) error {
	col := map[int]string{2: "delivered", 3: "read", 4: "played"}[status]
	if col == "" || ts <= 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO receipts (chat, msg_id, user, `+col+`) VALUES (?, ?, ?, ?)
		ON CONFLICT (chat, msg_id, user) DO UPDATE SET `+col+` = excluded.`+col+`
		WHERE receipts.`+col+` = 0`, chat, msgID, user, ts)
	return err
}

func (s *Store) Receipts(chat, msgID string) ([]Receipt, error) {
	rows, err := s.db.Query(`SELECT user, delivered, read, played FROM receipts WHERE chat = ? AND msg_id = ?`, chat, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.User, &r.Delivered, &r.Read, &r.Played); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReactionsBy lista quem reagiu com o quê (sender → emoji).
func (s *Store) ReactionsBy(chat, msgID string) ([][2]string, error) {
	rows, err := s.db.Query(`SELECT sender, emoji FROM reactions WHERE chat = ? AND msg_id = ? ORDER BY ts`, chat, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var r [2]string
		if err := rows.Scan(&r[0], &r[1]); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetServerID guarda o ID de servidor de uma mensagem de canal.
func (s *Store) SetServerID(chat, id string, serverID int) error {
	_, err := s.db.Exec(`UPDATE messages SET server_id = ? WHERE chat = ? AND id = ? AND server_id = 0`, serverID, chat, id)
	return err
}

// ServerIDs devolve o ID de servidor das mensagens (as sem ID ficam de fora).
func (s *Store) ServerIDs(chat string, ids []string) (map[string]int, error) {
	out := map[string]int{}
	for _, id := range ids {
		var sid int
		if err := s.db.QueryRow(`SELECT server_id FROM messages WHERE chat = ? AND id = ?`, chat, id).Scan(&sid); err == nil && sid > 0 {
			out[id] = sid
		}
	}
	return out, nil
}

// SetRestricted marca a conversa como restrita (ou não).
func (s *Store) SetRestricted(jid string, on bool) (bool, error) {
	res, err := s.db.Exec(`UPDATE chats SET restricted = ? WHERE jid = ? AND restricted != ?`, on, jid, on)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetKept salva (ou deixa de salvar) a temporária na conversa.
func (s *Store) SetKept(chat, id string, kept bool) (bool, error) {
	res, err := s.db.Exec(`UPDATE messages SET kept = ? WHERE chat = ? AND id = ? AND kept != ?`, kept, chat, id, kept)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetStarred marca ou desmarca a estrela; true se mudou.
func (s *Store) SetStarred(chat, id string, starred bool) (bool, error) {
	res, err := s.db.Exec(`UPDATE messages SET starred = ? WHERE chat = ? AND id = ? AND starred != ?`,
		starred, chat, id, starred)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// StarredMessages devolve as favoritas de todas as conversas, das mais
// novas para as mais antigas.
func (s *Store) StarredMessages(limit int) ([]ipc.Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE starred = 1
		AND chat NOT IN (SELECT jid FROM chats WHERE locked = 1) ORDER BY ts DESC LIMIT ?`, limit)
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

// SetPin fixa (ou, com pinned false, desafixa) uma mensagem. Vale o aviso
// mais recente: um desafixar antigo não derruba um fixar novo.
func (s *Store) SetPin(chat, msgID string, pinned bool, ts, expires int64) error {
	if !pinned {
		_, err := s.db.Exec(`DELETE FROM pins WHERE chat = ? AND msg_id = ? AND ts <= ?`, chat, msgID, ts)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO pins (chat, msg_id, ts, expires) VALUES (?, ?, ?, ?)
		ON CONFLICT (chat, msg_id) DO UPDATE SET ts = excluded.ts, expires = excluded.expires
		WHERE excluded.ts >= pins.ts`, chat, msgID, ts, expires)
	return err
}

// Pins devolve as mensagens fixadas e ainda valendo, a fixada por último
// primeiro.
func (s *Store) Pins(chat string) ([]ipc.Message, error) {
	rows, err := s.db.Query(`SELECT p.msg_id FROM pins p WHERE p.chat = ?
		AND (p.expires = 0 OR p.expires > CAST(strftime('%s','now') AS INTEGER) * 1000) ORDER BY p.ts DESC`, chat)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []ipc.Message{}
	for _, id := range ids {
		if m, _, err := s.Message(chat, id); err == nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// DeleteChat apaga a conversa e tudo dela.
func (s *Store) DeleteChat(chat string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM reactions WHERE chat = ?`, `DELETE FROM poll_votes WHERE chat = ?`,
		`DELETE FROM receipts WHERE chat = ?`, `DELETE FROM pins WHERE chat = ?`,
		`DELETE FROM messages WHERE chat = ?`, `DELETE FROM chats WHERE jid = ?`,
	} {
		if _, err := tx.Exec(q, chat); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearChat apaga as mensagens da conversa até upTo (unix ms; 0 = todas),
// menos as favoritas se keepStarred, com o que depende delas. A conversa
// fica na lista, na mesma posição, com a prévia da mensagem que sobrou (ou
// vazia). Devolve quantas mensagens saíram.
func (s *Store) ClearChat(chat string, upTo int64, keepStarred bool) (int, error) {
	if upTo <= 0 {
		upTo = math.MaxInt64
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	gone := `SELECT id FROM messages WHERE chat = ? AND ts <= ?`
	if keepStarred {
		gone += ` AND starred = 0`
	}
	for _, q := range []string{
		`DELETE FROM reactions WHERE chat = ? AND msg_id IN (` + gone + `)`,
		`DELETE FROM poll_votes WHERE chat = ? AND poll_id IN (` + gone + `)`,
		`DELETE FROM receipts WHERE chat = ? AND msg_id IN (` + gone + `)`,
		`DELETE FROM pins WHERE chat = ? AND msg_id IN (` + gone + `)`,
	} {
		if _, err := tx.Exec(q, chat, chat, upTo); err != nil {
			return 0, err
		}
	}
	del := `DELETE FROM messages WHERE chat = ? AND ts <= ?`
	if keepStarred {
		del += ` AND starred = 0`
	}
	res, err := tx.Exec(del, chat, upTo)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	// Prévia: a mensagem mais nova que ficou, ou nada.
	var m ipc.Message
	var isGroup bool
	err = tx.QueryRow(`SELECT m.sender_name, m.from_me, m.ts, m.kind, m.text, c.is_group FROM messages m
		JOIN chats c ON c.jid = m.chat WHERE m.chat = ? ORDER BY m.ts DESC LIMIT 1`, chat).
		Scan(&m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &isGroup)
	switch {
	case err == sql.ErrNoRows:
		_, err = tx.Exec(`UPDATE chats SET last_text = '', unread = 0 WHERE jid = ?`, chat)
	case err == nil:
		m.Chat = chat
		_, err = tx.Exec(`UPDATE chats SET last_text = ?, unread = 0 WHERE jid = ?`, preview(m, isGroup), chat)
	}
	if err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

// DeleteMessage tira a mensagem do banco (apagar para mim).
func (s *Store) DeleteMessage(chat, id string) error {
	if _, err := s.db.Exec(`DELETE FROM reactions WHERE chat = ? AND msg_id = ?`, chat, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM messages WHERE chat = ? AND id = ?`, chat, id)
	return err
}

// NewerThan devolve a primeira mensagem do chat depois de ts (a âncora de
// um pedido de histórico ao celular, que manda as anteriores a ela).
func (s *Store) NewerThan(chat string, ts int64) (ipc.Message, error) {
	return scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE chat = ? AND ts > ?
		ORDER BY ts ASC LIMIT 1`, chat, ts))
}

// galleryChat filtra a galeria pelo chat; chat "" é a central de mídia,
// de todas as conversas menos as trancadas.
func galleryChat(chat string) (string, []any) {
	if chat == "" {
		return `chat NOT IN (SELECT jid FROM chats WHERE locked = 1)`, nil
	}
	return `chat = ?`, []any{chat}
}

// Gallery devolve uma página de uma aba da galeria do chat (chat "" = de
// todos), do mais novo ao mais antigo, a partir de before (0 = do começo).
// more diz se há mais.
func (s *Store) Gallery(chat, tab string, before int64, limit int) (items []ipc.Message, more bool, err error) {
	where, ok := galleryWhere[tab]
	if !ok {
		return nil, false, fmt.Errorf("aba desconhecida: %q", tab)
	}
	if before <= 0 {
		before = 1 << 62
	}
	cw, args := galleryChat(chat)
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE `+cw+` AND ts < ? AND `+where+`
		AND `+notExpired+` ORDER BY ts DESC LIMIT ?`, append(args, before, limit+1)...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items = []ipc.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, m)
	}
	if len(items) > limit {
		items, more = items[:limit], true
	}
	return items, more, rows.Err()
}

// GalleryCounts conta os itens de cada aba da galeria do chat (chat "" =
// de todos).
func (s *Store) GalleryCounts(chat string) (map[string]int, error) {
	out := map[string]int{}
	cw, args := galleryChat(chat)
	for tab, where := range galleryWhere {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM messages WHERE `+cw+` AND `+where+` AND `+notExpired, args...).Scan(&n); err != nil {
			return nil, err
		}
		out[tab] = n
	}
	return out, nil
}

// MediaMessages devolve as fotos e vídeos mais recentes do chat (para o
// perfil).
func (s *Store) MediaMessages(chat string, limit int) ([]ipc.Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages
		WHERE chat = ? AND kind IN ('image', 'video') AND `+notExpired+` ORDER BY ts DESC LIMIT ?`, chat, limit)
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
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE chat = ? AND ts > ? AND `+notExpired+`
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

// SearchFilter restringe a busca: conversa, tipo, extensão de documento e
// intervalo de datas. Vazio = só texto, em todas as conversas.
type SearchFilter struct {
	Chat     string // só nesta conversa
	Type     string // chave de searchTypes ("fotos", "pdf" não: use Ext)
	Ext      string // documento com esta extensão ("pdf")
	From, To int64  // unix ms: ts >= From e ts < To (0 = sem limite)
}

// Active diz se o filtro restringe algo além da conversa.
func (f SearchFilter) Active() bool { return f.Type != "" || f.Ext != "" || f.From > 0 || f.To > 0 }

// searchTypes: filtro SQL de cada tipo:… da busca.
var searchTypes = map[string]string{
	"fotos":       `kind = 'image'`,
	"videos":      `kind = 'video'`,
	"gifs":        `kind = 'video' AND text LIKE '[GIF%'`,
	"audios":      `kind = 'audio'`,
	"docs":        `kind = 'document'`,
	"links":       galleryWhere["links"],
	"figurinhas":  `kind = 'sticker'`,
	"enquetes":    `kind = 'other' AND text LIKE '[enquete:%'`,
	"eventos":     `kind = 'other' AND text LIKE '[evento%'`,
	"contatos":    `kind = 'other' AND (text LIKE '[contato:%' OR text LIKE '[% contatos]')`,
	"localizacao": `kind = 'other' AND text LIKE '[localização%'`,
}

// SearchTypes devolve os tipos aceitos pelo filtro.
func SearchTypes() []string {
	out := make([]string, 0, len(searchTypes))
	for k := range searchTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SearchMessages percorre as mensagens da mais nova para a mais antiga e
// devolve as primeiras limit que passam no filtro e em que match(texto) é
// verdadeiro. A comparação do texto fica em Go para ignorar acento e
// maiúscula, o que o LIKE do SQLite não faz fora do ASCII.
func (s *Store) SearchMessages(f SearchFilter, match func(string) bool, limit int) ([]ipc.Message, error) {
	// Varre só chat, id e texto; a mensagem completa é lida só nos achados.
	where, args := []string{notExpired}, []any{}
	switch {
	case f.Ext != "":
		where, args = append(where, `kind = 'document' AND text LIKE ?`), append(args, "[documento: %."+f.Ext+"]%")
	case f.Type != "":
		w, ok := searchTypes[f.Type]
		if !ok {
			return nil, fmt.Errorf("tipo desconhecido: %s", f.Type)
		}
		where = append(where, w)
	default:
		where = append(where, `kind NOT IN ('revoked', 'sticker', 'audio') AND text != ''`)
	}
	if f.Chat != "" {
		where, args = append(where, "chat = ?"), append(args, f.Chat)
	}
	if f.From > 0 {
		where, args = append(where, "ts >= ?"), append(args, f.From)
	}
	if f.To > 0 {
		where, args = append(where, "ts < ?"), append(args, f.To)
	}
	rows, err := s.db.Query(`SELECT chat, id, text FROM messages WHERE (`+strings.Join(where, ") AND (")+`)
		AND chat NOT IN (SELECT jid FROM chats WHERE locked = 1) ORDER BY ts DESC`, args...)
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
			FROM messages WHERE chat = ? AND ts < ? AND `+notExpired+`
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

const msgCols = `id, chat, sender, sender_name, from_me, ts, kind, text, quote_id, quote_sender, quote_text, raw IS NOT NULL, edited, status, starred, expire_ts, kept`

func scanMessage(r interface{ Scan(...any) error }) (ipc.Message, error) {
	var m ipc.Message
	var q ipc.Quote
	err := r.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &q.ID, &q.Sender, &q.Text, &m.HasRaw, &m.Edited, &m.Status, &m.Starred, &m.Expires, &m.Kept)
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
	err := row.Scan(&m.ID, &m.Chat, &m.Sender, &m.SenderName, &m.FromMe, &m.TS, &m.Kind, &m.Text, &q.ID, &q.Sender, &q.Text, &m.HasRaw, &m.Edited, &m.Status, &m.Starred, &m.Expires, &m.Kept, &raw)
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
const chatCols = `jid, name, is_group, last_ts, last_text, unread, ephemeral, restricted,
	COALESCE((SELECT m.status FROM messages m WHERE m.chat = chats.jid AND m.ts = chats.last_ts
		AND m.from_me = 1 LIMIT 1), -1)`

// notExpired filtra mensagens temporárias já vencidas.
const notExpired = `(expire_ts = 0 OR kept = 1 OR expire_ts > CAST(strftime('%s','now') AS INTEGER) * 1000)`

// PurgeExpired apaga as mensagens temporárias vencidas (o celular e os
// outros aparelhos apagam as deles também). Devolve quantas.
func (s *Store) PurgeExpired() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages WHERE expire_ts > 0 AND kept = 0
		AND expire_ts <= CAST(strftime('%s','now') AS INTEGER) * 1000`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Done diz se a tarefa única key já rodou; MarkDone registra que rodou.
func (s *Store) Done(key string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM meta WHERE key = ?`, key).Scan(&n)
	return n > 0
}

func (s *Store) MarkDone(key string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES (?, '1')`, key)
	return err
}

// RawMessages percorre (em ordem de data) as mensagens com o protobuf
// original guardado.
func (s *Store) RawMessages(fn func(chat, id string, fromMe bool, ts int64, raw []byte)) error {
	rows, err := s.db.Query(`SELECT chat, id, from_me, ts, raw FROM messages WHERE raw IS NOT NULL ORDER BY ts`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var chat, id string
		var fromMe bool
		var ts int64
		var raw []byte
		if err := rows.Scan(&chat, &id, &fromMe, &ts, &raw); err != nil {
			return err
		}
		fn(chat, id, fromMe, ts, raw)
	}
	return rows.Err()
}

// SetExpiries grava o vencimento de várias mensagens de uma vez
// (chat → id → unix ms).
func (s *Store) SetExpiries(exp map[[2]string]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.Prepare(`UPDATE messages SET expire_ts = ? WHERE chat = ? AND id = ?`)
	if err != nil {
		return err
	}
	for k, ts := range exp {
		if _, err := st.Exec(ts, k[0], k[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetLocked marca a conversa como trancada (escondida) ou não.
func (s *Store) SetLocked(jid string, locked bool) error {
	// A trava pode chegar antes do histórico criar a conversa.
	_, err := s.db.Exec(`INSERT INTO chats (jid, locked) VALUES (?, ?)
		ON CONFLICT (jid) DO UPDATE SET locked = excluded.locked`, jid, locked)
	return err
}

func (s *Store) IsLocked(jid string) bool {
	var l bool
	_ = s.db.QueryRow(`SELECT locked FROM chats WHERE jid = ?`, jid).Scan(&l)
	return l
}

// SetEphemeral grava o prazo das mensagens temporárias da conversa
// (segundos; 0 desliga). Devolve true se mudou.
func (s *Store) SetEphemeral(jid string, secs int) (bool, error) {
	res, err := s.db.Exec(`INSERT INTO chats (jid, ephemeral) VALUES (?, ?)
		ON CONFLICT (jid) DO UPDATE SET ephemeral = excluded.ephemeral
		WHERE chats.ephemeral != excluded.ephemeral`, jid, secs)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) Ephemeral(jid string) int {
	var secs int
	_ = s.db.QueryRow(`SELECT ephemeral FROM chats WHERE jid = ?`, jid).Scan(&secs)
	return secs
}

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
