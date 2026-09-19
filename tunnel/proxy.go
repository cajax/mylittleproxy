package tunnel

import (
	"net"

	"github.com/cajax/mylittleproxy/proto"

	"go.uber.org/zap"
)

// ProxyFunc is responsible for forwarding a remote connection to local server and writing the response back.
type ProxyFunc func(remote net.Conn, msg *proto.ControlMessage)

var (
	// DefaultProxyFuncs holds global default proxy functions for all transport protocols.
	DefaultProxyFuncs = ProxyFuncs{
		HTTP: new(HTTPProxy).Proxy,
		WS:   new(WSProxy).Proxy,
	}
	// DefaultProxy is a ProxyFunc that uses DefaultProxyFuncs.
	DefaultProxy = Proxy(ProxyFuncs{})
)

// ProxyFuncs is a collection of ProxyFunc.
type ProxyFuncs struct {
	// HTTP is custom implementation of HTTP proxing.
	HTTP ProxyFunc
	// TCP is custom implementation of TCP proxing.
	TCP ProxyFunc
	// WS is custom implementation of web socket proxing.
	WS ProxyFunc

	// Log is used to report a connection that cannot be proxied. If not set,
	// nothing is logged.
	Log *zap.Logger
}

// Proxy returns a ProxyFunc that uses custom function if provided, otherwise falls back to DefaultProxyFuncs.
func Proxy(p ProxyFuncs) ProxyFunc {
	return func(remote net.Conn, msg *proto.ControlMessage) {
		var f ProxyFunc
		switch msg.Protocol {
		case proto.HTTP:
			f = DefaultProxyFuncs.HTTP
			if p.HTTP != nil {
				f = p.HTTP
			}
		case proto.TCP:
			f = DefaultProxyFuncs.TCP
			if p.TCP != nil {
				f = p.TCP
			}
		case proto.WS:
			f = DefaultProxyFuncs.WS
			if p.WS != nil {
				f = p.WS
			}
		}

		if f == nil {
			log := p.Log
			if log == nil {
				log = zap.NewNop()
			}
			log.Error("Could not determine proxy function", zap.Any("message", msg))
			remote.Close()
			return
		}

		f(remote, msg)
	}
}
