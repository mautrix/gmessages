package libgm

import (
	"sync"
	"sync/atomic"
)

// eventQueue dispatches events off the long polling read loop. That loop is the only
// goroutine that delivers responses to in-flight requests to the phone, so an event
// handler running on it that makes a request deadlocks waiting for a response it would
// have to deliver itself. Disconnect closes the queue and discards anything undelivered,
// so a dead session's events can't be replayed onto whatever replaced it; the next
// connect reopens it.
type eventQueue struct {
	client *Client

	lock    sync.Mutex
	drained *sync.Cond
	queue   []any
	// dispatching is only cleared by the drainer, under the lock, with the queue empty,
	// which is what guarantees a single drainer and FIFO dispatch order.
	dispatching bool
	// closed is mutated under the lock so push can't append concurrently with a close
	// discarding the queue.
	closed atomic.Bool
}

// The queue has no cap: blocking would reintroduce the read loop deadlock and dropping
// would lose messages, so a slow handler just makes it grow and complain.
const eventQueueWarnInterval = 1024

func (eq *eventQueue) push(evt any) {
	eq.lock.Lock()
	defer eq.lock.Unlock()
	if eq.closed.Load() {
		return
	}
	eq.queue = append(eq.queue, evt)
	if len(eq.queue)%eventQueueWarnInterval == 0 {
		eq.client.Logger.Warn().
			Int("queue_length", len(eq.queue)).
			Msg("Event queue is backing up, events are being handled too slowly")
	}
	if !eq.dispatching {
		eq.dispatching = true
		go eq.drain()
	}
}

func (eq *eventQueue) drain() {
	for {
		eq.lock.Lock()
		batch := eq.queue
		eq.queue = nil
		if len(batch) == 0 || eq.closed.Load() {
			eq.dispatching = false
			eq.drained.Broadcast()
			eq.lock.Unlock()
			return
		}
		eq.lock.Unlock()
		for _, evt := range batch {
			if eq.closed.Load() {
				break
			}
			eq.client.dispatchEvent(evt)
		}
	}
}

func (eq *eventQueue) wait() {
	eq.lock.Lock()
	for eq.dispatching || len(eq.queue) > 0 {
		eq.drained.Wait()
	}
	eq.lock.Unlock()
}

func (eq *eventQueue) open() {
	eq.closed.Store(false)
}

func (eq *eventQueue) close() {
	eq.lock.Lock()
	defer eq.lock.Unlock()
	eq.closed.Store(true)
	if len(eq.queue) > 0 {
		// Dropped data events are recovered by the conversation sync on the next connect.
		eq.client.Logger.Debug().
			Int("dropped_events", len(eq.queue)).
			Msg("Dropping queued events on disconnect")
		eq.queue = nil
	}
}
