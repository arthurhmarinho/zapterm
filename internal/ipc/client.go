package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Client é o lado da TUI. Call é síncrono; eventos chegam em Events().
type Client struct {
	c       net.Conn
	enc     *json.Encoder
	encMu   sync.Mutex
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan Frame
	events  chan Frame
	closed  chan struct{}
}

var ErrClosed = errors.New("conexão com o daemon fechada")

func Dial(path string) (*Client, error) {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return nil, err
	}
	cl := &Client{
		c:       c,
		enc:     json.NewEncoder(c),
		pending: map[int64]chan Frame{},
		events:  make(chan Frame, 256),
		closed:  make(chan struct{}),
	}
	go cl.loop()
	return cl, nil
}

func (cl *Client) Events() <-chan Frame { return cl.events }
func (cl *Client) Close() error         { return cl.c.Close() }

// Call manda o pedido e decodifica a resposta em out (pode ser nil).
func (cl *Client) Call(req Request, out any) error {
	req.ID = cl.nextID.Add(1)
	ch := make(chan Frame, 1)
	cl.mu.Lock()
	cl.pending[req.ID] = ch
	cl.mu.Unlock()
	defer func() {
		cl.mu.Lock()
		delete(cl.pending, req.ID)
		cl.mu.Unlock()
	}()

	cl.encMu.Lock()
	err := cl.enc.Encode(req)
	cl.encMu.Unlock()
	if err != nil {
		return err
	}

	select {
	case f := <-ch:
		if f.Error != "" {
			return errors.New(f.Error)
		}
		if out != nil && len(f.Data) > 0 {
			return json.Unmarshal(f.Data, out)
		}
		return nil
	case <-cl.closed:
		return ErrClosed
	case <-time.After(callTimeout(req.Op)):
		return errors.New("daemon não respondeu a tempo")
	}
}

func callTimeout(op string) time.Duration {
	switch op {
	case OpMedia, OpSendImage:
		return 4 * time.Minute
	case OpAvatar:
		return 3 * time.Minute
	}
	return 30 * time.Second
}

func (cl *Client) loop() {
	defer close(cl.closed)
	defer close(cl.events)
	sc := bufio.NewScanner(cl.c)
	sc.Buffer(make([]byte, 64*1024), 16<<20) // lista de chats pode ser grande
	for sc.Scan() {
		var f Frame
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			continue
		}
		if f.Event != "" {
			select {
			case cl.events <- f:
			default: // TUI atrasada perde evento; ela recarrega no próximo foco
			}
			continue
		}
		cl.mu.Lock()
		ch := cl.pending[f.ID]
		cl.mu.Unlock()
		if ch != nil {
			ch <- f
		}
	}
}
