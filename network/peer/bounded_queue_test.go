// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
)

// sizedMsg is an OutboundMessage of a chosen byte width. The bounded queue
// budgets in bytes, so the byte width is the only thing about a message it
// actually reasons about.
type sizedMsg struct {
	op    message.Op
	bytes []byte
}

func newSizedMsg(n int) *sizedMsg {
	return &sizedMsg{op: message.PingOp, bytes: make([]byte, n)}
}

func (*sizedMsg) BypassThrottling() bool     { return false }
func (m *sizedMsg) Op() message.Op           { return m.op }
func (m *sizedMsg) Bytes() []byte            { return m.bytes }
func (*sizedMsg) BytesSavedCompression() int { return 0 }

func newTestBoundedQueue(maxBytes int64, maxMessages int) *BoundedMessageQueue {
	return NewBoundedMessageQueue(maxBytes, maxMessages, log.NewNoOpLogger())
}

// within runs f and fails the test if it has not returned by d. Used wherever
// a defect would show up as a goroutine that never comes back rather than as
// a wrong answer.
func within(t *testing.T, d time.Duration, what string, f func()) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		t.Errorf("%s did not return within %s", what, d)
		return false
	}
}

// TestBoundedQueue_FIFOAndAccounting pins the two things the queue exists to
// do: hand messages back in the order they arrived, and keep the byte total
// equal to the bytes actually held. An accounting drift is worse than a wrong
// order — it silently shrinks or removes the bound over the life of a peer.
func TestBoundedQueue_FIFOAndAccounting(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 16)

	sent := make([]*sizedMsg, 8)
	total := int64(0)
	for i := range sent {
		sent[i] = newSizedMsg(100 + i)
		total += int64(100 + i)
		require.True(q.TryEnqueue(sent[i]))
	}
	require.Equal(8, q.Size())
	require.Equal(total, q.ByteSize())

	for i := range sent {
		got, ok := q.TryDequeue()
		require.True(ok)
		require.Same(sent[i], got, "messages must come back in the order they went in")
		total -= int64(len(sent[i].bytes))
		require.Equal(total, q.ByteSize(), "the byte total must track exactly what is held")
	}
	require.Zero(q.Size())
	require.Zero(q.ByteSize(), "an empty queue holds zero bytes")

	_, ok := q.TryDequeue()
	require.False(ok, "an empty queue has nothing to hand back")
}

// TestBoundedQueue_RefusesPastEitherBound is the backpressure property, from
// both directions. The message count and the byte budget are independent
// limits and either one alone must stop an enqueue — a queue bounded only by
// count admits 1000 huge messages, and one bounded only by bytes admits
// unbounded tiny ones.
func TestBoundedQueue_RefusesPastEitherBound(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		require := require.New(t)
		q := newTestBoundedQueue(1<<20, 3)
		for i := 0; i < 3; i++ {
			require.True(q.TryEnqueue(newSizedMsg(1)))
		}
		require.False(q.TryEnqueue(newSizedMsg(1)), "the message count is a hard bound")
		require.Equal(3, q.Size())
		require.EqualValues(1, q.Metrics().Dropped)

		// Space frees up and the next message goes in — the bound is a bound,
		// not a permanent poisoning.
		_, ok := q.TryDequeue()
		require.True(ok)
		require.True(q.TryEnqueue(newSizedMsg(1)))
	})

	t.Run("bytes", func(t *testing.T) {
		require := require.New(t)
		q := newTestBoundedQueue(1000, 1000)
		require.True(q.TryEnqueue(newSizedMsg(600)))
		require.True(q.TryEnqueue(newSizedMsg(400)), "exactly at the budget is admissible")
		require.EqualValues(1000, q.ByteSize())
		require.False(q.TryEnqueue(newSizedMsg(1)), "one byte past the budget is not")
		require.Equal(2, q.Size())
	})
}

// TestBoundedQueue_RefusesAMessageBiggerThanTheCap keeps a single peer from
// spending the whole budget: a message over MaxMessageSize is dropped before
// the lock is even taken, so it never displaces queued traffic.
func TestBoundedQueue_RefusesAMessageBiggerThanTheCap(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(DefaultMaxQueueSize, DefaultMaxMessages)

	require.False(q.TryEnqueue(newSizedMsg(MaxMessageSize + 1)))
	require.Zero(q.Size())
	require.ErrorIs(q.Enqueue(newSizedMsg(MaxMessageSize+1)), ErrMessageTooBig)
	require.Zero(q.Size())
	require.EqualValues(2, q.Metrics().Dropped)

	require.True(q.TryEnqueue(newSizedMsg(MaxMessageSize)), "exactly at the cap is admissible")
}

// TestBoundedQueue_EnqueueBlocksUntilThereIsRoom is the point of having a
// blocking Enqueue at all: a full queue must make the producer wait, not drop,
// and the wait must end the moment a consumer takes something.
func TestBoundedQueue_EnqueueBlocksUntilThereIsRoom(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 1)
	require.NoError(q.Enqueue(newSizedMsg(10)))

	returned := make(chan error, 1)
	go func() { returned <- q.Enqueue(newSizedMsg(10)) }()

	select {
	case err := <-returned:
		t.Fatalf("Enqueue on a full queue returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	_, ok := q.TryDequeue()
	require.True(ok)

	select {
	case err := <-returned:
		require.NoError(err)
	case <-time.After(5 * time.Second):
		t.Fatal("Enqueue never woke after space was freed")
	}
	require.Equal(1, q.Size())
}

// TestBoundedQueue_CloseWakesEveryWaiter is the shutdown property. Close must
// release a blocked producer AND a blocked consumer, or a peer teardown leaks
// the goroutines that were mid-send when the connection dropped.
func TestBoundedQueue_CloseWakesEveryWaiter(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 1)
	require.NoError(q.Enqueue(newSizedMsg(10)))

	producer := make(chan error, 1)
	go func() { producer <- q.Enqueue(newSizedMsg(10)) }() // blocks: queue is full

	consumer := make(chan error, 1)
	empty := newTestBoundedQueue(1<<20, 4)
	go func() {
		_, err := empty.Dequeue()
		consumer <- err
	}() // blocks: queue is empty

	time.Sleep(50 * time.Millisecond) // let both reach their wait
	q.Close()
	empty.Close()

	select {
	case <-producer:
	case <-time.After(5 * time.Second):
		t.Fatal("Close left a producer blocked")
	}
	select {
	case err := <-consumer:
		require.ErrorIs(err, ErrQueueClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("Close left a consumer blocked")
	}

	require.ErrorIs(q.Enqueue(newSizedMsg(1)), ErrQueueClosed)
	require.False(q.TryEnqueue(newSizedMsg(1)))

	q.Close() // idempotent: a second teardown must not panic or re-broadcast
}

// TestBoundedQueue_CloseMustNotAcceptTheMessageItWokeUp is the shutdown
// property from the producer's side, and it is where the blocking Enqueue
// gets it wrong.
//
// A producer parked on a full queue is woken by Close. Close also empties the
// queue — which makes the wait condition false — so the producer falls
// straight out of the loop WITHOUT re-testing whether the queue is still open,
// stores its message in a queue no consumer will ever read, and returns nil.
// The caller is told the message was accepted. Nobody ever sends it, and
// nothing reports it failed.
//
// Every other entry point agrees the message must be refused: TryEnqueue
// returns false, and a fresh Enqueue after Close returns ErrQueueClosed. Only
// the producer that was already waiting is told yes.
func TestBoundedQueue_CloseMustNotAcceptTheMessageItWokeUp(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 1)
	require.NoError(q.Enqueue(newSizedMsg(10)))

	late := newSizedMsg(10)
	returned := make(chan error, 1)
	go func() { returned <- q.Enqueue(late) }() // parks: the queue is full

	time.Sleep(50 * time.Millisecond)
	q.Close()

	var err error
	select {
	case err = <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Close left a producer blocked")
	}

	require.ErrorIs(err, ErrQueueClosed,
		"a message that arrives at a closing queue must be refused, not silently swallowed")
	require.Zero(q.Size(), "a closed queue must hold nothing")
	require.Zero(q.ByteSize())
}

// TestBoundedQueue_DequeueBatchTakesAPrefix pins the batch path against the
// same accounting rule as the single-message path, including the two edges
// that get written wrong: asking for more than is held, and asking on empty.
func TestBoundedQueue_DequeueBatchTakesAPrefix(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 32)

	require.Nil(q.DequeueBatch(4), "an empty queue yields no batch")

	sent := make([]*sizedMsg, 10)
	for i := range sent {
		sent[i] = newSizedMsg(50)
		require.True(q.TryEnqueue(sent[i]))
	}

	batch := q.DequeueBatch(4)
	require.Len(batch, 4)
	for i, msg := range batch {
		require.Same(sent[i], msg, "a batch is the front of the queue, in order")
	}
	require.Equal(6, q.Size())
	require.EqualValues(300, q.ByteSize(), "the batch must return exactly its own bytes to the budget")

	// Asking for more than is held yields what is held, not a padded slice.
	rest := q.DequeueBatch(100)
	require.Len(rest, 6)
	for i, msg := range rest {
		require.Same(sent[i+4], msg)
	}
	require.Zero(q.Size())
	require.Zero(q.ByteSize())
	require.EqualValues(10, q.Metrics().Dequeued)
}

// TestBoundedQueue_WrapsWithoutLosingMessages walks the ring buffer past its
// own length many times over. An off-by-one in the head/tail arithmetic shows
// up here as a message that vanishes or one that is handed back twice, and
// nowhere else.
func TestBoundedQueue_WrapsWithoutLosingMessages(t *testing.T) {
	require := require.New(t)
	const capacity = 4
	q := newTestBoundedQueue(1<<20, capacity)

	for round := 0; round < 50; round++ {
		in := make([]*sizedMsg, capacity)
		for i := range in {
			in[i] = newSizedMsg(8)
			require.True(q.TryEnqueue(in[i]), "round=%d i=%d", round, i)
		}
		require.False(q.TryEnqueue(newSizedMsg(8)), "round=%d: full is full on every lap", round)

		for i := range in {
			got, ok := q.TryDequeue()
			require.True(ok, "round=%d i=%d", round, i)
			require.Same(in[i], got, "round=%d i=%d", round, i)
		}
		require.Zero(q.Size(), "round=%d", round)
		require.Zero(q.ByteSize(), "round=%d", round)
	}
}

// TestBoundedQueue_ResetRestoresAnEmptyOpenQueue distinguishes Reset from
// Close: it drops what is held and zeroes the books, and the queue keeps
// working afterwards.
func TestBoundedQueue_ResetRestoresAnEmptyOpenQueue(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 8)
	for i := 0; i < 5; i++ {
		require.True(q.TryEnqueue(newSizedMsg(64)))
	}
	require.EqualValues(320, q.ByteSize())

	q.Reset()
	require.Zero(q.Size())
	require.Zero(q.ByteSize())
	require.Equal(QueueMetrics{}, q.Metrics(), "Reset zeroes the books as well as the contents")

	require.True(q.TryEnqueue(newSizedMsg(1)), "Reset does not close the queue")
	require.Equal(1, q.Size())
}

// TestBoundedQueue_HighWaterRecordsThePeak keeps the pressure signal honest:
// the high-water mark must remember the worst moment, not the current one,
// which is the only reason to record it separately from ByteSize.
func TestBoundedQueue_HighWaterRecordsThePeak(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 8)

	for i := 0; i < 4; i++ {
		require.NoError(q.Enqueue(newSizedMsg(1000)))
	}
	require.EqualValues(4000, q.Metrics().HighWater)

	for i := 0; i < 4; i++ {
		_, ok := q.TryDequeue()
		require.True(ok)
	}
	require.Zero(q.ByteSize())
	require.EqualValues(4000, q.Metrics().HighWater, "the peak must survive the drain")
	require.EqualValues(4, q.Metrics().Enqueued)
	require.EqualValues(4, q.Metrics().Dequeued)
}

// TestBoundedQueue_ConcurrentProducersAndConsumers is the accounting property
// under contention: every message a producer got a yes for is handed to
// exactly one consumer, and the books balance at the end. Run under -race this
// also asserts that reading the queue's own metrics is not a data race.
func TestBoundedQueue_ConcurrentProducersAndConsumers(t *testing.T) {
	require := require.New(t)
	q := newTestBoundedQueue(1<<20, 64)

	const producers, perProducer = 8, 200
	var wg sync.WaitGroup

	consumed := make(chan message.OutboundMessage, producers*perProducer)
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					for {
						msg, ok := q.TryDequeue()
						if !ok {
							return
						}
						consumed <- msg
					}
				default:
					if msg, ok := q.TryDequeue(); ok {
						consumed <- msg
					}
				}
			}
		}()
	}

	// A metrics reader running alongside the mutators: Metrics() is part of
	// the public surface and is called from monitoring goroutines.
	metricsDone := make(chan struct{})
	go func() {
		defer close(metricsDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = q.Metrics()
				_ = q.ByteSize()
			}
		}
	}()

	accepted := make(chan struct{}, producers*perProducer)
	var prodWG sync.WaitGroup
	for p := 0; p < producers; p++ {
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()
			for i := 0; i < perProducer; i++ {
				if err := q.Enqueue(newSizedMsg(16)); err == nil {
					accepted <- struct{}{}
				}
			}
		}()
	}

	prodWG.Wait()
	close(stop)
	wg.Wait()
	<-metricsDone
	close(accepted)
	close(consumed)

	require.Len(consumed, len(accepted),
		"every accepted message must be delivered exactly once")
	require.Zero(q.Size())
	require.Zero(q.ByteSize(), "the byte budget must return to zero once the queue drains")
}

// TestBoundedQueue_EnqueueOfAnUnservableMessageMustNotBlockForever is a
// liveness property, not a correctness one. A message that fits under
// MaxMessageSize but is larger than THIS queue's whole byte budget can never
// be admitted no matter how much drains — the wait condition is unsatisfiable.
// Enqueue must say so; a sender goroutine parked forever on a peer's send
// queue is a wedged connection nobody can see.
//
// TryEnqueue gets the same message right, which is what makes the blocking
// path's answer a defect rather than a design choice.
func TestBoundedQueue_EnqueueOfAnUnservableMessageMustNotBlockForever(t *testing.T) {
	const budget = 1024
	q := newTestBoundedQueue(budget, DefaultMaxMessages)
	msg := newSizedMsg(budget * 2) // under MaxMessageSize, over this queue's budget

	require.False(t, q.TryEnqueue(msg), "the non-blocking path refuses it outright")
	require.Zero(t, q.Size(), "precondition: nothing is in the way — the queue is empty")

	var err error
	returned := within(t, 2*time.Second, "Enqueue of a message larger than the queue budget", func() {
		err = q.Enqueue(msg)
	})
	if !returned {
		// Release the parked goroutine so the rest of the suite can finish.
		q.Close()
		return
	}
	require.Error(t, err, "a message that can never be admitted must be refused, not queued")
}
