package network

import (
	"net"
	"regexp"
	"time"

	"github.com/infinitete/neo-go-inf/pkg/io"
	log "github.com/sirupsen/logrus"
)

// TCPTransport allows network communication over TCP.
type TCPTransport struct {
	server   *Server
	listener net.Listener
	bindAddr string
}

var reClosedNetwork = regexp.MustCompile(".* use of closed network connection")

// NewTCPTransport returns a new TCPTransport that will listen for
// new incoming peer connections.
func NewTCPTransport(s *Server, bindAddr string) *TCPTransport {
	return &TCPTransport{
		server:   s,
		bindAddr: bindAddr,
	}
}

// Dial implements the Transporter interface.
func (t *TCPTransport) Dial(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	go t.handleConn(conn)
	return nil
}

// Accept implements the Transporter interface.
func (t *TCPTransport) Accept() {
	l, err := net.Listen("tcp", t.bindAddr)
	if err != nil {
		log.Fatalf("TCP listen error %s", err)
		return
	}

	t.listener = l

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Warnf("TCP accept error: %s", err)
			if t.isCloseError(err) {
				break
			}
			continue
		}
		go t.handleConn(conn)
	}
}

func (t *TCPTransport) isCloseError(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		if reClosedNetwork.Match([]byte(opErr.Error())) {
			return true
		}
	}

	return false
}

func (t *TCPTransport) handleConn(conn net.Conn) {
	var (
		p   = NewTCPPeer(conn)
		err error
	)

	t.server.register <- p

	r := io.NewBinReaderFromIO(p.conn)
	for {
		msg := &Message{}
		if err = msg.Decode(r); err != nil {
			break
		}
		if err = t.server.handleMessage(p, msg); err != nil {
			break
		}
	}
	t.server.unregister <- peerDrop{p, err}
	p.Disconnect(err)
}

// Close implements the Transporter interface.
func (t *TCPTransport) Close() {
	t.listener.Close()
}

// Proto implements the Transporter interface.
func (t *TCPTransport) Proto() string {
	return "tcp"
}
