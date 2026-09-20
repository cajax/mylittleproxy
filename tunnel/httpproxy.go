package tunnel

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/cajax/mylittleproxy/proto"
	"go.uber.org/zap"
	"io"
	"net"
	"net/http"
	"net/url"
)

// HTTPProxy forwards HTTP traffic.
//
// We take original request, replace host and protocol with target host and execute it. Returned data redirected to the yamux tunnel
// When connection to local server cannot be established proxy responds with http error message.
type HTTPProxy struct {
	// TargetHost defines the TCP address of the local server.
	// This is optional if you want to specify a single TCP address.
	TargetHost string

	// CustomHeaders are set on the request before it is sent to the local
	// server, replacing what the caller sent under the same name.
	CustomHeaders map[string]string

	// ErrorResp is custom response send to tunnel server when client cannot
	// establish connection to local server. If not set a default "no local server"
	// response is sent.
	ErrorResp *http.Response
	// Log is a custom logger that can be used for the proxy.
	// If not set a "http" logger is used.
	Log *zap.Logger
}

// Proxy is a ProxyFunc.
func (p *HTTPProxy) Proxy(remote net.Conn, msg *proto.ControlMessage) {
	if msg.Protocol != proto.HTTP && msg.Protocol != proto.WS {
		panic("Proxy mismatch")
	}

	req, err := http.ReadRequest(bufio.NewReader(remote))
	if err != nil {
		p.log().Warn("Failed to parse original request", zap.Error(err))
		p.sendError(remote)
		return
	}

	if err := p.patchRequest(req); err != nil {
		p.log().Warn("Failed to build request to local server",
			zap.String("target", p.TargetHost), zap.Error(err))
		p.sendError(remote)
		return
	}

	res, err := http.DefaultClient.Do(req)
	status := ""
	if res != nil {
		status = res.Status
	}
	p.log().Info("Handled HTTP", zap.String("URL", req.URL.String()), zap.Error(err), zap.String("status", status))
	if err != nil {
		p.log().Warn("Failed remote request", zap.String("URL", req.URL.String()), zap.Error(err))
		p.sendError(remote)
		return
	}
	defer res.Body.Close()

	if err := res.Write(remote); err != nil {
		p.log().Warn("Failed to write response to the tunnel",
			zap.String("URL", req.URL.String()), zap.Error(err))
	}
}

// log returns the configured logger. HTTPProxy is exported and Log is optional,
// so a caller that left it unset gets a no-op logger rather than a panic.
func (p *HTTPProxy) log() *zap.Logger {
	if p.Log == nil {
		return zap.NewNop()
	}

	return p.Log
}

// patchRequest redirects a request that arrived over the tunnel at the local
// server. Only the scheme and host are taken from the configured target: the URL
// that came off the tunnel is kept as it is, so the path the server rewrote, the
// query string and their encoding all survive.
func (p *HTTPProxy) patchRequest(req *http.Request) error {
	targetUrl, err := url.Parse(p.TargetHost)
	if err != nil {
		return fmt.Errorf("invalid target %q: %s", p.TargetHost, err)
	}

	// Set on requests read by a server, rejected on requests sent by a client.
	req.RequestURI = ""
	req.URL.Scheme = targetUrl.Scheme
	req.URL.Host = targetUrl.Host
	// The Host header follows req.URL.Host, the server having cleared req.Host
	// when it rewrote the request.
	req.Host = ""

	applyCustomHeaders(req, p.CustomHeaders)

	return nil
}

func (p *HTTPProxy) sendError(remote net.Conn) {
	var w = noLocalServer()
	if p.ErrorResp != nil {
		w = p.ErrorResp
	}

	sendErrorResponse(remote, w, p.log())
}

// sendErrorResponse writes an in-memory response into the tunnel and closes it.
func sendErrorResponse(remote net.Conn, resp *http.Response, log *zap.Logger) {
	buf := new(bytes.Buffer)
	if err := resp.Write(buf); err != nil {
		log.Debug("Cannot render the error response", zap.Error(err))
		remote.Close()

		return
	}

	if _, err := io.Copy(remote, buf); err != nil {
		log.Debug("Copy in-mem response error", zap.Error(err))
	}

	remote.Close()
}

func noLocalServer() *http.Response {
	body := bytes.NewBufferString("no local server")
	return &http.Response{
		Status:        http.StatusText(http.StatusServiceUnavailable),
		StatusCode:    http.StatusServiceUnavailable,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          io.NopCloser(body),
		ContentLength: int64(body.Len()),
	}
}
