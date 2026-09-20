package main

import (
	"fmt"
	"net"

	"github.com/cajax/mylittleproxy/tunnel"
)

// sameListenAddress reports whether two listen addresses would put the control
// protocol back on the public listener. An empty host and 0.0.0.0 both mean
// every interface, so they clash with any host on the same port.
func sameListenAddress(listen, control string) bool {
	listenHost, listenPort, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}

	controlHost, controlPort, err := net.SplitHostPort(control)
	if err != nil {
		return false
	}

	if listenPort != controlPort {
		return false
	}

	return listenHost == controlHost || isAllInterfaces(listenHost) || isAllInterfaces(controlHost)
}

func isAllInterfaces(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::"
}

// checkListenAddresses rejects a configuration that asks for a separate control
// listener and then names the public one.
func checkListenAddresses(listen, control string) error {
	if control == "" {
		return nil
	}

	if sameListenAddress(listen, control) {
		return fmt.Errorf("listenControl %q is the same address as listen %q: the control protocol would stay on the public listener", control, listen)
	}

	return nil
}

// serve runs the public and control listeners. With no control address both are
// served by one listener, as before.
func serve(listen, control string, server *tunnel.Server) error {
	if control == "" {
		return newHTTPServer(listen, server).ListenAndServe()
	}

	errs := make(chan error, 2)

	go func() {
		errs <- newHTTPServer(control, server.ControlHandler()).ListenAndServe()
	}()
	go func() {
		errs <- newHTTPServer(listen, server.PublicHandler()).ListenAndServe()
	}()

	// Either listener going down takes the server with it.
	return <-errs
}
