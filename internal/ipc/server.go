package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

// Handler responde a um pedido. conn identifica a conexão (uma por TUI
// aberta). O valor devolvido vira Frame.Data.
type Handler func(conn uint64, req Request) (any, error)

// Server aceita várias TUIs ao mesmo tempo e faz broadcast dos eventos.
type Server struct {
	ln      net.Listener
	handle  Handler
	log     *slog.Logger
	mu      sync.Mutex
	clients map[*conn]struct{}
	lastID  uint64

	// OnClose é chamado (em goroutine própria) quando uma TUI desconecta.
	OnClose func(conn uint64)
}

type conn struct {
	id  uint64
	c   net.Conn
	out chan Frame
}

// Listen abre o socket. Se já houver um daemon respondendo, falha; se o
// arquivo sobrou de um daemon morto, remove e segue.
func Listen(path string, h Handler, log *slog.Logger) (*Server, error) {
	if c, err := net.DialTimeout("unix", path, 300*time.Millisecond); err == nil {
		c.Close()
		return nil, fmt.Errorf("já existe um daemon em %s", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return &Server{ln: ln, handle: h, log: log, clients: map[*conn]struct{}{}}, nil
}

func (s *Server) Serve() error {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.mu.Lock()
		s.lastID++
		cc := &conn{id: s.lastID, c: c, out: make(chan Frame, 256)}
		s.clients[cc] = struct{}{}
		s.mu.Unlock()
		go s.writer(cc)
		go s.reader(cc)
	}
}

func (s *Server) Close() error { return s.ln.Close() }

// Clients diz quantas TUIs estão conectadas.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// Broadcast manda um evento para todas as TUIs conectadas. Cliente lento
// demais (fila cheia) é desconectado em vez de travar o daemon.
func (s *Server) Broadcast(event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		s.log.Error("broadcast", "err", err)
		return
	}
	f := Frame{Event: event, Data: raw}
	s.mu.Lock()
	defer s.mu.Unlock()
	for cc := range s.clients {
		select {
		case cc.out <- f:
		default:
			s.log.Warn("cliente lento, desconectando")
			s.drop(cc)
		}
	}
}

// drop exige s.mu travado.
func (s *Server) drop(cc *conn) {
	if _, ok := s.clients[cc]; !ok {
		return
	}
	delete(s.clients, cc)
	close(cc.out)
	cc.c.Close()
	if s.OnClose != nil {
		go s.OnClose(cc.id)
	}
}

func (s *Server) writer(cc *conn) {
	enc := json.NewEncoder(cc.c)
	for f := range cc.out {
		if err := enc.Encode(f); err != nil {
			s.mu.Lock()
			s.drop(cc)
			s.mu.Unlock()
			return
		}
	}
}

func (s *Server) reader(cc *conn) {
	defer func() {
		s.mu.Lock()
		s.drop(cc)
		s.mu.Unlock()
	}()
	sc := bufio.NewScanner(cc.c)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			s.reply(cc, Frame{Error: "json inválido: " + err.Error()})
			continue
		}
		// Cada pedido roda em goroutine própria: um send lento não pode
		// segurar a listagem de chats.
		go func() {
			f := Frame{ID: req.ID}
			data, err := s.handle(cc.id, req)
			if err != nil {
				f.Error = err.Error()
			} else if data != nil {
				f.Data, err = json.Marshal(data)
				if err != nil {
					f.Error = err.Error()
				}
			}
			s.reply(cc, f)
		}()
	}
}

func (s *Server) reply(cc *conn, f Frame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clients[cc]; !ok {
		return
	}
	select {
	case cc.out <- f:
	default:
		s.drop(cc)
	}
}
