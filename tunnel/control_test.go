package tunnel

import (
	"errors"
	"net"
	"testing"
)

func TestControlRefusesToSendOnceClosed(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()

	ct := newControl(a)

	if err := ct.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := ct.send(struct{ A int }{1})
	if !errors.Is(err, errControlClosed) {
		t.Errorf("send after Close = %v, want %v", err, errControlClosed)
	}
}

func TestControlCloseIsSafeWithoutAConnection(t *testing.T) {
	ct := &control{}

	if err := ct.Close(); err != nil {
		t.Errorf("Close on a control with no connection = %v, want nil", err)
	}
}

func TestControlsRegistry(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	cs := newControls()

	if _, ok := cs.getControl("1234"); ok {
		t.Error("an unknown identifier returned a control")
	}

	ct := newControl(a)
	cs.addControl("1234", ct)

	got, ok := cs.getControl("1234")
	if !ok || got != ct {
		t.Fatal("the control that was added did not come back")
	}
	// addControl stamps the identifier on the control, which listenControl
	// relies on when cleaning up.
	if got.identifier != "1234" {
		t.Errorf("identifier = %q, want %q", got.identifier, "1234")
	}

	cs.deleteControl("1234")
	if _, ok := cs.getControl("1234"); ok {
		t.Error("the control is still registered after deleteControl")
	}
}
