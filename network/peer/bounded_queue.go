// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/luxfi/node/message"
	"github.com/luxfi/log"
)

var (
	ErrQueueFull     = errors.New("message queue is full")
	ErrQueueClosed   = errors.New("message queue is closed")
	ErrMessageTooBig = errors.New("message exceeds maximum size")
)

const (
	// DefaultMaxQueueSize is the default maximum queue size in bytes (10MB)
	DefaultMaxQueueSize = 10 * 1024 * 1024

	// DefaultMaxMessages is the default maximum number of messages
	DefaultMaxMessages = 1000

	// MaxMessageSize is the maximum size of a single message (2MB)
	MaxMessageSize = 2 * 1024 * 1024
)

// BoundedMessageQueue implements a thread-safe bounded message queue with backpressure
type BoundedMessageQueue struct {
	mu sync.Mutex

	// Queue state
	messages []message.OutboundMessage
	head     int
	tail     int
	size     int

	// Bounds
	maxSize     int64
	maxMessages int
	currentSize atomic.Int64

	// Signaling
	notEmpty *sync.Cond
	notFull  *sync.Cond

	// Metrics
	dropped    atomic.Uint64
	enqueued   atomic.Uint64
	dequeued   atomic.Uint64
	highWater  atomic.Int64

	// Control
	closed atomic.Bool
	log    log.Logger
}

// NewBoundedMessageQueue creates a new bounded message queue
func NewBoundedMessageQueue(maxSize int64, maxMessages int, log log.Logger) *BoundedMessageQueue {
	if maxSize <= 0 {
		maxSize = DefaultMaxQueueSize
	}
	if maxMessages <= 0 {
		maxMessages = DefaultMaxMessages
	}

	q := &BoundedMessageQueue{
		messages:    make([]message.OutboundMessage, maxMessages+1),
		maxSize:     maxSize,
		maxMessages: maxMessages,
		log:         log,
	}

	q.notEmpty = sync.NewCond(&q.mu)
	q.notFull = sync.NewCond(&q.mu)

	return q
}

// Enqueue adds a message to the queue with backpressure
func (q *BoundedMessageQueue) Enqueue(msg message.OutboundMessage) error {
	if q.closed.Load() {
		return ErrQueueClosed
	}

	msgSize := q.estimateMessageSize(msg)
	if msgSize > MaxMessageSize {
		q.dropped.Add(1)
		return ErrMessageTooBig
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if adding this message would exceed limits
	for q.size >= q.maxMessages || q.currentSize.Load()+msgSize > q.maxSize {
		if q.closed.Load() {
			return ErrQueueClosed
		}

		// Apply backpressure - wait for space
		q.notFull.Wait()
	}

	// Add message to queue
	q.messages[q.tail] = msg
	q.tail = (q.tail + 1) % len(q.messages)
	q.size++

	newSize := q.currentSize.Add(msgSize)
	q.enqueued.Add(1)

	// Update high water mark
	for {
		highWater := q.highWater.Load()
		if newSize <= highWater || q.highWater.CompareAndSwap(highWater, newSize) {
			break
		}
	}

	// Signal that queue is not empty
	q.notEmpty.Signal()

	return nil
}

// TryEnqueue attempts to enqueue without blocking
func (q *BoundedMessageQueue) TryEnqueue(msg message.OutboundMessage) bool {
	if q.closed.Load() {
		return false
	}

	msgSize := q.estimateMessageSize(msg)
	if msgSize > MaxMessageSize {
		q.dropped.Add(1)
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if we have space
	if q.size >= q.maxMessages || q.currentSize.Load()+msgSize > q.maxSize {
		q.dropped.Add(1)
		return false
	}

	// Add message to queue
	q.messages[q.tail] = msg
	q.tail = (q.tail + 1) % len(q.messages)
	q.size++

	q.currentSize.Add(msgSize)
	q.enqueued.Add(1)

	// Signal that queue is not empty
	q.notEmpty.Signal()

	return true
}

// Dequeue removes and returns a message from the queue
func (q *BoundedMessageQueue) Dequeue() (message.OutboundMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Wait for message or closure
	for q.size == 0 && !q.closed.Load() {
		q.notEmpty.Wait()
	}

	if q.size == 0 && q.closed.Load() {
		return nil, ErrQueueClosed
	}

	// Remove message from queue
	msg := q.messages[q.head]
	q.messages[q.head] = nil // Help GC
	q.head = (q.head + 1) % len(q.messages)
	q.size--

	msgSize := q.estimateMessageSize(msg)
	q.currentSize.Add(-msgSize)
	q.dequeued.Add(1)

	// Signal that queue is not full
	q.notFull.Signal()

	return msg, nil
}

// TryDequeue attempts to dequeue without blocking
func (q *BoundedMessageQueue) TryDequeue() (message.OutboundMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.size == 0 {
		return nil, false
	}

	// Remove message from queue
	msg := q.messages[q.head]
	q.messages[q.head] = nil // Help GC
	q.head = (q.head + 1) % len(q.messages)
	q.size--

	msgSize := q.estimateMessageSize(msg)
	q.currentSize.Add(-msgSize)
	q.dequeued.Add(1)

	// Signal that queue is not full
	q.notFull.Signal()

	return msg, true
}

// DequeueBatch removes up to n messages from the queue
func (q *BoundedMessageQueue) DequeueBatch(n int) []message.OutboundMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.size == 0 {
		return nil
	}

	// Determine batch size
	batchSize := n
	if batchSize > q.size {
		batchSize = q.size
	}

	batch := make([]message.OutboundMessage, batchSize)
	totalSize := int64(0)

	for i := 0; i < batchSize; i++ {
		msg := q.messages[q.head]
		batch[i] = msg
		q.messages[q.head] = nil // Help GC
		q.head = (q.head + 1) % len(q.messages)
		totalSize += q.estimateMessageSize(msg)
	}

	q.size -= batchSize
	q.currentSize.Add(-totalSize)
	q.dequeued.Add(uint64(batchSize))

	// Signal that queue has space
	q.notFull.Broadcast()

	return batch
}

// Size returns the current number of messages in the queue
func (q *BoundedMessageQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.size
}

// ByteSize returns the current size in bytes
func (q *BoundedMessageQueue) ByteSize() int64 {
	return q.currentSize.Load()
}

// Close closes the queue and releases waiting goroutines
func (q *BoundedMessageQueue) Close() {
	if q.closed.CompareAndSwap(false, true) {
		q.mu.Lock()
		defer q.mu.Unlock()

		// Clear queue to help GC
		for i := range q.messages {
			q.messages[i] = nil
		}
		q.size = 0
		q.currentSize.Store(0)

		// Wake all waiters
		q.notEmpty.Broadcast()
		q.notFull.Broadcast()
	}
}

// Metrics returns queue statistics
func (q *BoundedMessageQueue) Metrics() QueueMetrics {
	return QueueMetrics{
		Size:      q.size,
		ByteSize:  q.currentSize.Load(),
		Enqueued:  q.enqueued.Load(),
		Dequeued:  q.dequeued.Load(),
		Dropped:   q.dropped.Load(),
		HighWater: q.highWater.Load(),
	}
}

// QueueMetrics contains queue statistics
type QueueMetrics struct {
	Size      int
	ByteSize  int64
	Enqueued  uint64
	Dequeued  uint64
	Dropped   uint64
	HighWater int64
}

// estimateMessageSize estimates the size of a message in bytes
func (q *BoundedMessageQueue) estimateMessageSize(msg message.OutboundMessage) int64 {
	if msg == nil {
		return 0
	}

	// Get actual message bytes
	msgBytes := msg.Bytes()
	return int64(len(msgBytes))
}

// Reset clears the queue but keeps it open
func (q *BoundedMessageQueue) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Clear all messages
	for i := range q.messages {
		q.messages[i] = nil
	}

	q.head = 0
	q.tail = 0
	q.size = 0
	q.currentSize.Store(0)

	// Reset metrics
	q.dropped.Store(0)
	q.enqueued.Store(0)
	q.dequeued.Store(0)
	q.highWater.Store(0)

	// Wake any waiters
	q.notFull.Broadcast()
}