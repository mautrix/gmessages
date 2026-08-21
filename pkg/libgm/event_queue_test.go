package libgm

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestClient(handler EventHandler) *Client {
	cli := NewClient(NewAuthData(), nil, zerolog.Nop())
	cli.SetEventHandler(handler)
	return cli
}

func TestEventQueue_DispatchesInOrder(t *testing.T) {
	var lock sync.Mutex
	var got []any
	cli := newTestClient(func(evt any) {
		lock.Lock()
		got = append(got, evt)
		lock.Unlock()
	})
	for i := range 100 {
		cli.queueEvent(i)
	}
	cli.eventQueue.wait()
	if len(got) != 100 {
		t.Fatalf("expected 100 events, got %d", len(got))
	}
	for i, evt := range got {
		if evt != i {
			t.Fatalf("event %d out of order: got %v", i, evt)
		}
	}
}

func TestEventQueue_CloseDropsQueuedEvents(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	var lock sync.Mutex
	var got []any
	cli := newTestClient(func(evt any) {
		started <- struct{}{}
		<-release
		lock.Lock()
		got = append(got, evt)
		lock.Unlock()
	})
	cli.queueEvent("in flight")
	// Wait until the drainer is committed to dispatching the first event, so the events
	// queued behind it are deterministically still in the queue when the close hits.
	<-started
	for range 5 {
		cli.queueEvent("queued behind")
	}
	cli.Disconnect()
	close(release)
	cli.eventQueue.wait()
	lock.Lock()
	defer lock.Unlock()
	if len(got) != 1 || got[0] != "in flight" {
		t.Fatalf("expected only the in-flight event to be delivered, got %v", got)
	}
}

func TestEventQueue_ClosedDropsInlineAndQueuedEvents(t *testing.T) {
	var lock sync.Mutex
	var got []any
	cli := newTestClient(func(evt any) {
		lock.Lock()
		got = append(got, evt)
		lock.Unlock()
	})
	cli.Disconnect()
	cli.queueEvent("queued while closed")
	cli.triggerEvent("inline while closed")
	cli.eventQueue.wait()

	cli.eventQueue.open()
	cli.queueEvent("queued after reopen")
	cli.eventQueue.wait()
	cli.triggerEvent("inline after reopen")

	lock.Lock()
	defer lock.Unlock()
	if len(got) != 2 || got[0] != "queued after reopen" || got[1] != "inline after reopen" {
		t.Fatalf("expected only post-reopen events to be delivered, got %v", got)
	}
}

func TestEventQueue_WaitBlocksUntilDrained(t *testing.T) {
	release := make(chan struct{})
	var handled sync.WaitGroup
	handled.Add(3)
	cli := newTestClient(func(evt any) {
		<-release
		handled.Done()
	})
	for range 3 {
		cli.queueEvent("evt")
	}
	waited := make(chan struct{})
	go func() {
		cli.eventQueue.wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("wait returned before events were handled")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	handled.Wait()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("wait didn't return after events were handled")
	}
}

func TestEventQueue_DrainerRespawnsAfterIdle(t *testing.T) {
	var lock sync.Mutex
	var got []any
	cli := newTestClient(func(evt any) {
		lock.Lock()
		got = append(got, evt)
		lock.Unlock()
	})
	for round := range 3 {
		cli.queueEvent(round)
		cli.eventQueue.wait()
	}
	lock.Lock()
	defer lock.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 events across drainer respawns, got %v", got)
	}
}
