package tunnel

import "testing"

func TestAddHostRejectsTakeoverByAnotherIdentifier(t *testing.T) {
	v := newVirtualHosts()

	if err := v.AddHost("app.example.com", "alice", nil); err != nil {
		t.Fatalf("first AddHost: %v", err)
	}

	if err := v.AddHost("app.example.com", "mallory", nil); err == nil {
		t.Fatal("AddHost allowed a second identifier to claim a host already in use")
	}

	if id, ok := v.GetIdentifier("app.example.com"); !ok || id != "alice" {
		t.Fatalf("host owner = %q (found=%v), want %q", id, ok, "alice")
	}
}

func TestAddHostAllowsSameIdentifierToReclaimItsHost(t *testing.T) {
	v := newVirtualHosts()

	if err := v.AddHost("app.example.com", "alice", nil); err != nil {
		t.Fatalf("first AddHost: %v", err)
	}

	// A reconnecting client re-registers the host it already owns.
	if err := v.AddHost("app.example.com", "alice", nil); err != nil {
		t.Fatalf("reclaim by owner: %v", err)
	}
}

func TestAddHostDropsPreviousHostOfSameIdentifier(t *testing.T) {
	v := newVirtualHosts()

	if err := v.AddHost("old.example.com", "alice", nil); err != nil {
		t.Fatalf("AddHost old: %v", err)
	}
	if err := v.AddHost("new.example.com", "alice", nil); err != nil {
		t.Fatalf("AddHost new: %v", err)
	}

	// Otherwise GetHost/GetVirtualHost pick one of the two at random, since
	// they scan the map.
	if _, ok := v.GetIdentifier("old.example.com"); ok {
		t.Error("old host still mapped after the client moved to a new one")
	}

	host, ok := v.GetHost("alice")
	if !ok || host != "new.example.com" {
		t.Errorf("GetHost = %q (found=%v), want %q", host, ok, "new.example.com")
	}
}

func TestDeleteHostOfDisconnectedClientLeavesOtherClientsAlone(t *testing.T) {
	v := newVirtualHosts()

	if err := v.AddHost("alice.example.com", "alice", nil); err != nil {
		t.Fatalf("AddHost alice: %v", err)
	}
	if err := v.AddHost("bob.example.com", "bob", nil); err != nil {
		t.Fatalf("AddHost bob: %v", err)
	}

	v.DeleteHost("bob.example.com")

	if _, ok := v.GetIdentifier("alice.example.com"); !ok {
		t.Error("alice lost her host when bob disconnected")
	}
}
