// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/servicenodevm"
)

func setupTestInbox(t *testing.T) *Inbox {
	db := memdb.New()
	config := servicenodevm.DefaultConfig()
	nodeID := ids.GenerateTestNodeID()

	inbox := NewInbox(db, config, nodeID)
	ctx := context.Background()
	if err := inbox.Load(ctx); err != nil {
		t.Fatalf("failed to load inbox: %v", err)
	}

	return inbox
}

func createTestMessage(recipientID, senderID [32]byte) *servicenodevm.StoredMessage {
	return &servicenodevm.StoredMessage{
		RecipientID: recipientID,
		SenderID:    senderID,
		SwarmID:     0,
		Payload:     []byte("test message content"),
		TTL:         3600,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
		Size:        uint64(len("test message content")),
	}
}

func TestInboxStore(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	msg := createTestMessage(recipientID, senderID)

	// Store message
	if err := inbox.Store(ctx, msg); err != nil {
		t.Fatalf("failed to store message: %v", err)
	}

	// Verify message was stored
	if msg.ID == ids.Empty {
		t.Errorf("message ID not generated")
	}

	// Verify stats
	stats := inbox.GetStats()
	if stats.TotalMessages != 1 {
		t.Errorf("expected 1 total message, got %d", stats.TotalMessages)
	}

	if stats.TotalSize != msg.Size {
		t.Errorf("expected total size %d, got %d", msg.Size, stats.TotalSize)
	}
}

func TestInboxFetch(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	// Store multiple messages
	for i := 0; i < 5; i++ {
		msg := createTestMessage(recipientID, senderID)
		msg.Payload = []byte("message " + string(rune('0'+i)))
		inbox.Store(ctx, msg)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Fetch all messages
	messages, err := inbox.Fetch(ctx, recipientID, time.Time{}, 10)
	if err != nil {
		t.Fatalf("failed to fetch messages: %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(messages))
	}

	// Verify order (oldest first)
	for i := 1; i < len(messages); i++ {
		if messages[i].CreatedAt.Before(messages[i-1].CreatedAt) {
			t.Errorf("messages not in chronological order")
		}
	}
}

func TestInboxFetchWithTimestamp(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	// Store messages with delays
	var midTime time.Time
	for i := 0; i < 5; i++ {
		msg := createTestMessage(recipientID, senderID)
		inbox.Store(ctx, msg)
		time.Sleep(10 * time.Millisecond)
		if i == 2 {
			midTime = time.Now()
		}
	}

	// Fetch messages after midTime
	messages, err := inbox.Fetch(ctx, recipientID, midTime, 10)
	if err != nil {
		t.Fatalf("failed to fetch messages: %v", err)
	}

	// Should get messages after midTime (should be 2)
	if len(messages) > 3 {
		t.Errorf("expected <= 3 messages after midTime, got %d", len(messages))
	}
}

func TestInboxFetchByID(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	msg := createTestMessage(recipientID, senderID)
	inbox.Store(ctx, msg)

	// Fetch by ID
	retrieved, err := inbox.FetchByID(ctx, msg.ID)
	if err != nil {
		t.Fatalf("failed to fetch by ID: %v", err)
	}

	if retrieved.ID != msg.ID {
		t.Errorf("wrong message retrieved")
	}
}

func TestInboxDeduplication(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	msg := createTestMessage(recipientID, senderID)
	inbox.Store(ctx, msg)

	// Try to store same message again
	msg2 := createTestMessage(recipientID, senderID)
	msg2.CreatedAt = msg.CreatedAt
	msg2.Payload = msg.Payload
	inbox.Store(ctx, msg2)

	// Should still have only 1 message
	stats := inbox.GetStats()
	if stats.TotalMessages != 1 {
		t.Errorf("expected 1 message after dedup, got %d", stats.TotalMessages)
	}
}

func TestInboxQuota(t *testing.T) {
	db := memdb.New()
	config := servicenodevm.DefaultConfig()
	config.StorageQuota = 100 // Very small quota for testing
	nodeID := ids.GenerateTestNodeID()

	inbox := NewInbox(db, config, nodeID)
	ctx := context.Background()
	inbox.Load(ctx)

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	// Store message that exceeds quota
	msg := createTestMessage(recipientID, senderID)
	msg.Payload = make([]byte, 50)
	msg.Size = 50
	inbox.Store(ctx, msg)

	// Second message should exceed quota
	msg2 := createTestMessage(recipientID, senderID)
	msg2.Payload = make([]byte, 60)
	msg2.Size = 60
	msg2.CreatedAt = time.Now().Add(time.Second)

	err := inbox.Store(ctx, msg2)
	if err != servicenodevm.ErrQuotaExceeded {
		t.Errorf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestInboxMarkDelivered(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	msg := createTestMessage(recipientID, senderID)
	inbox.Store(ctx, msg)

	// Mark as delivered
	if err := inbox.MarkDelivered(ctx, msg.ID); err != nil {
		t.Fatalf("failed to mark delivered: %v", err)
	}

	// Verify
	retrieved, _ := inbox.FetchByID(ctx, msg.ID)
	if !retrieved.Delivered {
		t.Errorf("message not marked as delivered")
	}

	if retrieved.DeliveredAt.IsZero() {
		t.Errorf("delivered time not set")
	}

	// Verify stats
	stats := inbox.GetStats()
	if stats.DeliveredCount != 1 {
		t.Errorf("expected 1 delivered, got %d", stats.DeliveredCount)
	}
}

func TestInboxDelete(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	msg := createTestMessage(recipientID, senderID)
	inbox.Store(ctx, msg)

	// Delete message
	if err := inbox.Delete(ctx, msg.ID, recipientID); err != nil {
		t.Fatalf("failed to delete message: %v", err)
	}

	// Verify message is gone
	_, err := inbox.FetchByID(ctx, msg.ID)
	if err != servicenodevm.ErrMessageNotFound {
		t.Errorf("expected ErrMessageNotFound, got %v", err)
	}

	// Verify stats
	stats := inbox.GetStats()
	if stats.TotalMessages != 0 {
		t.Errorf("expected 0 messages after delete, got %d", stats.TotalMessages)
	}
}

func TestInboxPruneExpired(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	// Store an expired message
	msg := createTestMessage(recipientID, senderID)
	msg.ExpiresAt = time.Now().Add(-time.Hour) // Already expired
	inbox.Store(ctx, msg)

	// Store a valid message
	msg2 := createTestMessage(recipientID, senderID)
	msg2.ExpiresAt = time.Now().Add(time.Hour)
	msg2.CreatedAt = time.Now().Add(time.Second)
	inbox.Store(ctx, msg2)

	// Prune expired
	pruned, err := inbox.PruneExpired(ctx)
	if err != nil {
		t.Fatalf("failed to prune: %v", err)
	}

	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// Verify stats
	stats := inbox.GetStats()
	if stats.TotalMessages != 1 {
		t.Errorf("expected 1 message after prune, got %d", stats.TotalMessages)
	}
}

func TestInboxStorageCommitment(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	// Store some messages
	for i := 0; i < 5; i++ {
		msg := createTestMessage(recipientID, senderID)
		msg.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		inbox.Store(ctx, msg)
	}

	// Create commitment
	commit := inbox.CreateStorageCommitment(1)

	if commit.EpochID != 1 {
		t.Errorf("expected epoch ID 1, got %d", commit.EpochID)
	}

	if commit.MessageCount != 5 {
		t.Errorf("expected 5 messages, got %d", commit.MessageCount)
	}

	if commit.StoreRoot == [32]byte{} {
		t.Errorf("expected non-zero store root")
	}
}

func TestInboxStoreRoot(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	// Empty inbox should have zero root
	root := inbox.ComputeStoreRoot()
	if root != [32]byte{} {
		t.Errorf("expected zero root for empty inbox")
	}

	var recipientID, senderID [32]byte
	copy(recipientID[:], "recipient")
	copy(senderID[:], "sender")

	// Store messages
	for i := 0; i < 3; i++ {
		msg := createTestMessage(recipientID, senderID)
		msg.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		inbox.Store(ctx, msg)
	}

	// Root should be non-zero
	root = inbox.ComputeStoreRoot()
	if root == [32]byte{} {
		t.Errorf("expected non-zero root")
	}

	// Root should be deterministic
	root2 := inbox.ComputeStoreRoot()
	if root != root2 {
		t.Errorf("store root not deterministic")
	}
}

func TestInboxMultipleRecipients(t *testing.T) {
	inbox := setupTestInbox(t)
	ctx := context.Background()

	var senderID [32]byte
	copy(senderID[:], "sender")

	// Send to multiple recipients
	for i := 0; i < 3; i++ {
		var recipientID [32]byte
		copy(recipientID[:], []byte{byte(i)})

		for j := 0; j < 5; j++ {
			msg := createTestMessage(recipientID, senderID)
			// Ensure unique payload and timestamp for each message to avoid deduplication
			msg.Payload = []byte(fmt.Sprintf("message-%d-%d", i, j))
			msg.Size = uint64(len(msg.Payload))
			msg.CreatedAt = time.Now().Add(time.Duration(i*5+j) * time.Second)
			inbox.Store(ctx, msg)
		}
	}

	// Each recipient should have 5 messages
	for i := 0; i < 3; i++ {
		var recipientID [32]byte
		copy(recipientID[:], []byte{byte(i)})

		count := inbox.GetMessageCount(recipientID)
		if count != 5 {
			t.Errorf("recipient %d: expected 5 messages, got %d", i, count)
		}
	}

	// Total should be 15
	stats := inbox.GetStats()
	if stats.TotalMessages != 15 {
		t.Errorf("expected 15 total messages, got %d", stats.TotalMessages)
	}
}
