package tunnel

import (
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
)

// applyCustomHeaders sets the client's configured headers on a request on its
// way to the local server, replacing whatever the caller sent under the same
// name. An empty value sets an empty header rather than removing one.
func applyCustomHeaders(req *http.Request, headers map[string]string) {
	for name, value := range headers {
		// Host is carried on the request rather than in the header map, and
		// setting it there would silently do nothing.
		if strings.EqualFold(name, "Host") {
			req.Host = value
			continue
		}

		req.Header.Set(name, value)
	}
}

// validateCustomHeaders rejects names and values that cannot be sent, so a
// client fails at startup rather than producing broken requests. A newline in a
// value would let one header smuggle another.
func validateCustomHeaders(headers map[string]string) error {
	for name, value := range headers {
		if name == "" {
			return fmt.Errorf("custom header name must not be empty")
		}
		if textproto.CanonicalMIMEHeaderKey(name) == "" || !validHeaderName(name) {
			return fmt.Errorf("invalid custom header name %q", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("custom header %q has a value containing a line break", name)
		}
	}

	return nil
}

// validHeaderName reports whether name is a token as HTTP defines it.
func validHeaderName(name string) bool {
	for _, r := range name {
		if !isTokenRune(r) {
			return false
		}
	}

	return true
}

func isTokenRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}

	return strings.ContainsRune("!#$%&'*+-.^_`|~", r)
}
