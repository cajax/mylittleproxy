package tunnel

import (
	"fmt"
	"regexp"
	"sync"
)

type vhostStorage interface {
	// AddHost adds the given host and identifier to the storage. It fails if the
	// host is already claimed by another identifier.
	AddHost(host, identifier string, rewrites []HTTPRewriteRule) error

	// DeleteHost deletes the given host
	DeleteHost(host string)

	// GetHost returns the host name for the given identifier
	GetHost(identifier string) (string, bool)

	// GetVirtualHost returns entire virtualhost info for the given identifier
	GetVirtualHost(identifier string) (*virtualHost, bool, string)

	// GetIdentifier returns the identifier for the given host
	GetIdentifier(host string) (string, bool)
}

type virtualHost struct {
	identifier string
	Rewrite    []HTTPRewriteRule
}

type HTTPRewriteRule struct {
	re          *regexp.Regexp
	replacement string
}

// virtualHosts is used for mapping host to users example: host
// "fs-1-fatih.kd.io" belongs to user "arslan"
type virtualHosts struct {
	mapping map[string]*virtualHost
	sync.Mutex
}

// newVirtualHosts provides an in memory virtual host storage for mapping
// virtual hosts to identifiers.
func newVirtualHosts() *virtualHosts {
	return &virtualHosts{
		mapping: make(map[string]*virtualHost),
	}
}

// AddHost maps host to identifier. A host may only be claimed by one identifier
// at a time: re-registering a host the identifier already owns is allowed, so a
// reconnecting client can reclaim it, but claiming a host owned by somebody else
// is refused. An identifier owns a single host, so any host it held previously
// is released here.
func (v *virtualHosts) AddHost(host, identifier string, rewrites []HTTPRewriteRule) error {
	v.Lock()
	defer v.Unlock()

	if current, ok := v.mapping[host]; ok && current.identifier != identifier {
		return fmt.Errorf("host %q is already in use by another client", host)
	}

	for hostname, hst := range v.mapping {
		if hst.identifier == identifier && hostname != host {
			delete(v.mapping, hostname)
		}
	}

	v.mapping[host] = &virtualHost{identifier: identifier, Rewrite: rewrites}

	return nil
}

func (v *virtualHosts) DeleteHost(host string) {
	v.Lock()
	delete(v.mapping, host)
	v.Unlock()
}

// GetIdentifier returns the identifier associated with the given host
func (v *virtualHosts) GetIdentifier(host string) (string, bool) {
	v.Lock()
	ht, ok := v.mapping[host]
	v.Unlock()

	if !ok {
		return "", false
	}

	return ht.identifier, true
}

// GetHost returns the host associated with the given identifier
func (v *virtualHosts) GetHost(identifier string) (string, bool) {
	v.Lock()
	defer v.Unlock()

	for hostname, hst := range v.mapping {
		if hst.identifier == identifier {
			return hostname, true
		}
	}

	return "", false
}

func (v *virtualHosts) GetVirtualHost(identifier string) (*virtualHost, bool, string) {
	v.Lock()
	defer v.Unlock()

	for hostId, hst := range v.mapping {
		if hst.identifier == identifier {
			return hst, true, hostId
		}
	}

	return nil, false, ""
}
