package tunnel

import "testing"

func TestCheckIdentifierSignature(t *testing.T) {
	const (
		id  = "1234"
		key = "secretkey"
	)

	tests := []struct {
		name      string
		signature string
		want      bool
	}{
		{"valid", signIdentifier(id, key), true},
		{"empty", "", false},
		{"signed with another key", signIdentifier(id, "wrongkey"), false},
		{"signature of another identifier", signIdentifier("5678", key), false},
		{"truncated", signIdentifier(id, key)[:10], false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkIdentifierSignature(id, key, tt.signature); got != tt.want {
				t.Errorf("checkIdentifierSignature(%q) = %v, want %v", tt.signature, got, tt.want)
			}
		})
	}
}
