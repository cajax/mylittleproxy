package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"

	"github.com/cajax/mylittleproxy/appConfig"
	"github.com/cajax/mylittleproxy/proto"
)

// clientConfigOptions are the parts of a client config the server cannot know:
// which client it is for, which domain it claims and where it proxies to.
type clientConfigOptions struct {
	identifier string
	domain     string
	target     string

	// address overrides the address clients dial, for when the server listens
	// on every interface.
	address string

	// omitSignatureKey leaves the shared secret out, for operators who pass it
	// through MYLITTLEPROXY_SIGNATURE_KEY instead.
	omitSignatureKey bool

	debug bool
}

const defaultGeneratedTarget = "http://127.0.0.1:8080"

// buildClientConfig derives a client config from the server's own, so the
// settings that have to match on both sides cannot drift. It refuses to produce
// a config this server would then reject.
func buildClientConfig(server appConfig.Server, opts clientConfigOptions) (appConfig.Client, error) {
	var client appConfig.Client

	// Clients dial the control listener, which is a different address when the
	// two are separated.
	listen := server.Listen
	if server.ListenControl != "" {
		listen = server.ListenControl
	}

	address, err := clientFacingAddress(listen, opts.address)
	if err != nil {
		return client, err
	}

	if opts.domain == "" {
		return client, fmt.Errorf("a domain is required: the server cannot derive one from allowedHosts")
	}
	if err := checkAgainstAllowedHosts(server.AllowedHosts, opts.domain); err != nil {
		return client, err
	}
	if err := checkAgainstAllowedClients(server.AllowedClients, opts.identifier); err != nil {
		return client, err
	}

	target := opts.target
	if target == "" {
		target = defaultGeneratedTarget
	}

	controlPath := server.ControlPath
	if controlPath == "" {
		controlPath = proto.DefaultControlPath
	}

	controlMethod := server.ControlMethod
	if controlMethod == "" {
		controlMethod = proto.DefaultControlMethod
	}

	signatureKey := server.SignatureKey
	if opts.omitSignatureKey {
		signatureKey = ""
	}

	return appConfig.Client{
		Debug:         opts.debug,
		Identifier:    opts.identifier,
		ServerAddress: address,
		SignatureKey:  signatureKey,
		ControlPath:   controlPath,
		ControlMethod: controlMethod,
		Proxy: appConfig.Proxy{Http: appConfig.HTTPConfig{
			Domain: opts.domain,
			Target: target,
			// The client refuses to start without a rule, and a rule that
			// matches everything is the least surprising starting point.
			Rewrite: []appConfig.HTTPRewriteRule{{From: "/", To: "/"}},
		}},
	}, nil
}

// clientFacingAddress works out what a client should dial. A listener bound to
// every interface says nothing about how the server is reached, so in that case
// the operator has to supply it.
func clientFacingAddress(listen, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("cannot read an address from listen %q: %s", listen, err)
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		return "", fmt.Errorf("the server listens on %q, which does not say how clients reach it: pass the address they should dial", listen)
	}

	return net.JoinHostPort(host, port), nil
}

func checkAgainstAllowedHosts(allowedHosts []string, domain string) error {
	if len(allowedHosts) == 0 {
		return fmt.Errorf("the server has no allowedHosts, so it would reject %q", domain)
	}

	for _, h := range allowedHosts {
		re, err := regexp.Compile(h)
		if err != nil {
			return fmt.Errorf("invalid allowedHosts pattern %q: %s", h, err)
		}
		if re.MatchString(domain) {
			return nil
		}
	}

	return fmt.Errorf("domain %q matches none of the server's allowedHosts patterns", domain)
}

func checkAgainstAllowedClients(allowedClients []string, identifier string) error {
	if len(allowedClients) == 0 {
		// An empty list admits any client with a valid signature.
		return nil
	}

	for _, c := range allowedClients {
		if c == identifier {
			return nil
		}
	}

	return fmt.Errorf("identifier %q is not in the server's allowedClients", identifier)
}

// generateClientConfig writes a client config for this server to out.
func generateClientConfig(server appConfig.Server, opts clientConfigOptions, out io.Writer) error {
	client, err := buildClientConfig(server, opts)
	if err != nil {
		return err
	}

	encoded, err := marshalClientConfig(client)
	if err != nil {
		return err
	}

	_, err = out.Write(encoded)

	return err
}

func marshalClientConfig(client appConfig.Client) ([]byte, error) {
	out, err := json.MarshalIndent(client, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(out, '\n'), nil
}
