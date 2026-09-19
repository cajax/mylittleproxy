package tunnel

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/cajax/mylittleproxy/proto"

	"go.uber.org/zap"
)

// WSProxy forwards a websocket connection to the local server.
//
// A websocket is a request/response exchange only until the upgrade is
// accepted; after that it is a bidirectional stream of frames with no end in
// sight. It cannot be replayed with an http.Client the way HTTPProxy replays an
// ordinary request: the connection is handed over instead. The upgrade request
// is written to the local server and the two connections are then joined, so
// the upgrade response and every frame after it flow back through the tunnel on
// their own.
type WSProxy struct {
	// TargetHost defines the address of the local server.
	TargetHost string

	// Log is a custom logger that can be used for the proxy. If not set,
	// nothing is logged.
	Log *zap.Logger
}

// Proxy is a ProxyFunc.
func (p *WSProxy) Proxy(remote net.Conn, msg *proto.ControlMessage) {
	if msg.Protocol != proto.WS {
		p.log().Error("Proxy mismatch", zap.Any("message", msg))
		remote.Close()
		return
	}

	// The reader may buffer bytes past the request headers, so everything that
	// goes to the local server has to come through it.
	br := bufio.NewReader(remote)

	req, err := http.ReadRequest(br)
	if err != nil {
		p.log().Warn("Failed to parse upgrade request", zap.Error(err))
		sendErrorResponse(remote, noLocalServer(), p.log())
		return
	}

	local, err := dialTarget(p.TargetHost)
	if err != nil {
		p.log().Warn("Failed to reach local server",
			zap.String("target", p.TargetHost), zap.String("URL", req.URL.String()), zap.Error(err))
		sendErrorResponse(remote, noLocalServer(), p.log())
		return
	}

	// The tunnel server clears Host when it rewrites the request, but the local
	// server needs one to accept the upgrade.
	req.Host = hostHeaderFor(p.TargetHost, req.Host)

	if err := req.Write(local); err != nil {
		p.log().Warn("Failed to write upgrade request to local server",
			zap.String("URL", req.URL.String()), zap.Error(err))
		local.Close()
		sendErrorResponse(remote, noLocalServer(), p.log())
		return
	}

	// Whatever the reader buffered past the headers is already part of the
	// conversation and belongs to the local server.
	if n := br.Buffered(); n > 0 {
		if _, err := io.CopyN(local, br, int64(n)); err != nil {
			p.log().Warn("Failed to forward buffered request data", zap.Error(err))
			local.Close()
			return
		}
	}

	p.log().Info("Handling websocket", zap.String("URL", req.URL.String()), zap.String("target", p.TargetHost))

	joinStreams(remote, local, p.log())
}

func (p *WSProxy) log() *zap.Logger {
	if p.Log == nil {
		return zap.NewNop()
	}

	return p.Log
}

// dialTarget opens a connection to the local server, with TLS when the target
// asks for it.
func dialTarget(target string) (net.Conn, error) {
	addr, useTLS, err := targetAddress(target)
	if err != nil {
		return nil, err
	}

	if useTLS {
		return tls.Dial("tcp", addr, nil)
	}

	return net.Dial("tcp", addr)
}

// targetAddress turns a target URL into the address to dial, filling in the
// port its scheme implies, and says whether the connection has to be TLS.
func targetAddress(target string) (addr string, useTLS bool, err error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", false, fmt.Errorf("invalid target %q: %s", target, err)
	}

	switch u.Scheme {
	case "https", "wss":
		useTLS = true
	case "http", "ws", "":
	default:
		return "", false, fmt.Errorf("unsupported target scheme %q", u.Scheme)
	}

	if u.Host == "" {
		return "", false, fmt.Errorf("target %q names no host", target)
	}

	addr = u.Host
	if u.Port() == "" {
		port := "80"
		if useTLS {
			port = "443"
		}
		addr = net.JoinHostPort(u.Hostname(), port)
	}

	return addr, useTLS, nil
}

// hostHeaderFor returns the Host header to send to the local server, keeping
// the original when the target does not name a host.
func hostHeaderFor(target, original string) string {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return original
	}

	return u.Host
}

// joinStreams copies between two connections until either side is done, then
// closes both so the other direction cannot be left hanging.
func joinStreams(a, b net.Conn, log *zap.Logger) {
	var wg sync.WaitGroup
	wg.Add(2)

	transfer := func(dst, src net.Conn) {
		defer wg.Done()

		n, err := io.Copy(dst, src)
		log.Debug("Stream finished", zap.Int64("bytes", n), zap.Error(err))

		// Unblock the other direction.
		dst.Close()
		src.Close()
	}

	go transfer(a, b)
	go transfer(b, a)

	wg.Wait()
}
