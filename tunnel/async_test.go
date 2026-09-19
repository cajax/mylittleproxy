package tunnel

import (
	"errors"
	"testing"
	"time"
)

// TestAsyncDeliversErrorProducedBeforeTheReceive covers the case the callers
// actually hit: fn fails immediately, so it finishes before the select that
// reads from the channel is reached.
func TestAsyncDeliversErrorProducedBeforeTheReceive(t *testing.T) {
	want := errors.New("accept failed")

	ran := make(chan struct{})
	errCh := async(func() error {
		close(ran)
		return want
	})

	<-ran
	time.Sleep(10 * time.Millisecond) // let the send happen first

	select {
	case got := <-errCh:
		if !errors.Is(got, want) {
			t.Fatalf("got %v, want %v: a failure was reported as success", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no value received")
	}
}

func TestAsyncDeliversNilOnSuccess(t *testing.T) {
	errCh := async(func() error { return nil })

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("got %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no value received")
	}
}

// TestAsyncDoesNotBlockWhenNobodyReceives covers the timeout path: the callers
// abandon the channel, and the goroutine must still finish.
func TestAsyncDoesNotBlockWhenNobodyReceives(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		async(func() error { return errors.New("nobody is listening") })
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("async blocked when its result was abandoned")
	}
}
