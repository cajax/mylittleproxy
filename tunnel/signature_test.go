package tunnel

import "testing"

// The signature is the wire format between server and client, so it is pinned
// to a value computed independently of this code. Changing it means every
// client and server has to be upgraded together.
func TestSignIdentifierIsHMACSHA256(t *testing.T) {
	const (
		id   = "1234"
		key  = "secretkey"
		want = "Kz5TW03swyEBROF-SYhMw4wuCIW6rxr83T5X912vM-k="
	)

	if got := signIdentifier(id, key); got != want {
		t.Errorf("signIdentifier(%q, %q) = %q, want %q", id, key, got, want)
	}
}

// The signature this replaces: base64(sha1(identifier + ":" + key)). A server on
// this version must not accept it.
func TestOldSHA1SignatureIsRejected(t *testing.T) {
	const (
		id  = "1234"
		key = "secretkey"
		old = "Cp3DZxD9RWBZD17-jVeaKoZsSG8="
	)

	if checkIdentifierSignature(id, key, old) {
		t.Error("a signature from the old SHA-1 scheme was accepted")
	}
}

func TestSignIdentifierDependsOnBothInputs(t *testing.T) {
	base := signIdentifier("1234", "secretkey")

	if other := signIdentifier("5678", "secretkey"); other == base {
		t.Error("two identifiers produced the same signature")
	}
	if other := signIdentifier("1234", "anotherkey"); other == base {
		t.Error("two keys produced the same signature")
	}
}

// The signature travels in an HTTP header, so it has to survive one.
func TestSignatureIsHeaderSafe(t *testing.T) {
	sig := signIdentifier("1234", "secretkey")

	for _, r := range sig {
		if r < 0x20 || r > 0x7e {
			t.Fatalf("signature %q contains a character that cannot go in a header: %q", sig, r)
		}
	}
}
