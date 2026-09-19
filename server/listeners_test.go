package main

import (
	"strings"
	"testing"
)

// The point of a second listener is that the control protocol is somewhere the
// public cannot reach, so an address that is effectively the same one has to be
// refused rather than quietly collapsing back to a single listener.
func TestSameListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		listen  string
		control string
		want    bool
	}{
		{name: "identical", listen: ":8080", control: ":8080", want: true},
		{name: "all interfaces against explicit", listen: ":8080", control: "0.0.0.0:8080", want: true},
		{name: "explicit against all interfaces", listen: "0.0.0.0:8080", control: ":8080", want: true},
		{name: "same host and port", listen: "127.0.0.1:8080", control: "127.0.0.1:8080", want: true},
		{name: "different ports", listen: ":8080", control: ":9090", want: false},
		// A listener on every interface already occupies that port on loopback,
		// so binding the control listener there would fail.
		{name: "explicit host covered by all interfaces", listen: "0.0.0.0:8080", control: "127.0.0.1:8080", want: true},
		{name: "different hosts, different ports", listen: "0.0.0.0:8080", control: "127.0.0.1:9090", want: false},
		{name: "loopback control, public listener", listen: ":8080", control: "127.0.0.1:9090", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameListenAddress(tt.listen, tt.control); got != tt.want {
				t.Errorf("sameListenAddress(%q, %q) = %v, want %v", tt.listen, tt.control, got, tt.want)
			}
		})
	}
}

func TestSameListenAddressOnUnparsableInput(t *testing.T) {
	// Not an address at all: let the listener report it rather than claiming a
	// clash that may not exist.
	if sameListenAddress("not-an-address", ":8080") {
		t.Error("an unparsable address was reported as clashing")
	}
}

func TestCheckListenAddresses(t *testing.T) {
	if err := checkListenAddresses(":8080", ""); err != nil {
		t.Errorf("a single listener is allowed: %v", err)
	}
	if err := checkListenAddresses(":8080", ":9090"); err != nil {
		t.Errorf("two different addresses are allowed: %v", err)
	}

	err := checkListenAddresses(":8080", ":8080")
	if err == nil {
		t.Fatal("the same address for both listeners was accepted")
	}
	if !strings.Contains(err.Error(), "listenControl") {
		t.Errorf("error = %v, want it to name the option", err)
	}
}
