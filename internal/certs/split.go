package certs

import (
	"bufio"
	"net"
	"sync"
	"time"
)

// Splitter lets the panel and automatic HTTPS share one port (for example
// when the panel itself listens on 443): it looks at the first byte of every
// connection and hands TLS handshakes to the HTTPS server and everything else
// to the panel's plain HTTP server.
type Splitter struct {
	ln    net.Listener
	plain *vlistener

	mu  sync.Mutex
	tls *vlistener // nil while HTTPS is off: TLS connections are closed
}

// NewSplitter starts splitting ln.
func NewSplitter(ln net.Listener) *Splitter {
	s := &Splitter{ln: ln, plain: newVListener(ln.Addr())}
	go s.loop()
	return s
}

// Plain is the listener the panel's HTTP server serves.
func (s *Splitter) Plain() net.Listener { return s.plain }

// TLS returns a fresh listener that receives the TLS connections until it is
// closed. A server that stops and starts again simply asks for a new one.
func (s *Splitter) TLS() net.Listener {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tls != nil {
		s.tls.Close()
	}
	s.tls = newVListener(s.ln.Addr())
	return s.tls
}

// Port is the shared listener's port.
func (s *Splitter) Port() string {
	_, p, _ := net.SplitHostPort(s.ln.Addr().String())
	return p
}

func (s *Splitter) loop() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			s.plain.Close()
			s.mu.Lock()
			if s.tls != nil {
				s.tls.Close()
			}
			s.mu.Unlock()
			return
		}
		go s.route(c)
	}
}

func (s *Splitter) route(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		c.Close()
		return
	}
	pc := &peekedConn{Conn: c, r: br}
	if first[0] != 0x16 { // not a TLS handshake record
		s.plain.push(pc)
		return
	}
	s.mu.Lock()
	t := s.tls
	s.mu.Unlock()
	if t == nil || !t.push(pc) {
		c.Close()
	}
}

type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (p *peekedConn) Read(b []byte) (int, error) { return p.r.Read(b) }

// vlistener is a net.Listener fed by the Splitter.
type vlistener struct {
	addr net.Addr
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newVListener(addr net.Addr) *vlistener {
	return &vlistener{addr: addr, ch: make(chan net.Conn), done: make(chan struct{})}
}

func (l *vlistener) push(c net.Conn) bool {
	select {
	case l.ch <- c:
		return true
	case <-l.done:
		c.Close()
		return false
	}
}

func (l *vlistener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *vlistener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *vlistener) Addr() net.Addr { return l.addr }
