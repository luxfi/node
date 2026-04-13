// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package daemon implements the store-and-forward inbox for service nodes.
// It handles message storage, delivery, deduplication, and quota management.
package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/servicenodevm"
)

var (
	messagePrefix     = []byte("msg:")
	inboxPrefix       = []byte("inbox:")
	quotaPrefix       = []byte("quota:")
	dedupePrefix      = []byte("dedupe:")
	statsPrefix       = []byte("stats:")
)

// Inbox manages message storage and retrieval for a service node
type Inbox struct {
	db     database.Database
	config *servicenodevm.Config
	nodeID ids.NodeID

	// Message storage
	messages map[ids.ID]*servicenodevm.StoredMessage
	// Inbox index: accountID -> messageIDs (sorted by time)
	inboxes  map[[32]byte][]ids.ID
	// Deduplication: messageHash -> messageID
	dedupe   map[[32]byte]ids.ID
	// Quota tracking: accountID -> bytes used
	quotas   map[[32]byte]uint64

	// Stats
	totalMessages   uint64
	totalSize       uint64
	deliveredCount  uint64

	mu sync.RWMutex
}

// NewInbox creates a new message inbox
func NewInbox(db database.Database, config *servicenodevm.Config, nodeID ids.NodeID) *Inbox {
	return &Inbox{
		db:       db,
		config:   config,
		nodeID:   nodeID,
		messages: make(map[ids.ID]*servicenodevm.StoredMessage),
		inboxes:  make(map[[32]byte][]ids.ID),
		dedupe:   make(map[[32]byte]ids.ID),
		quotas:   make(map[[32]byte]uint64),
	}
}

// Load loads inbox state from database
func (i *Inbox) Load(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Load stats
	statsData, err := i.db.Get(statsPrefix)
	if err == nil && len(statsData) > 0 {
		var stats InboxStats
		if err := json.Unmarshal(statsData, &stats); err == nil {
			i.totalMessages = stats.TotalMessages
			i.totalSize = stats.TotalSize
			i.deliveredCount = stats.DeliveredCount
		}
	}

	// In a full implementation, we'd load messages from database
	// For efficiency, we only load indexes and fetch messages on demand

	return nil
}

// Store stores a message in the inbox
func (i *Inbox) Store(ctx context.Context, msg *servicenodevm.StoredMessage) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Check for duplicate
	msgHash := msg.Hash()
	if existingID, exists := i.dedupe[msgHash]; exists {
		// Update existing message if newer
		if existing, ok := i.messages[existingID]; ok {
			if msg.CreatedAt.After(existing.CreatedAt) {
				existing.ExpiresAt = msg.ExpiresAt
				return i.persistMessage(existing)
			}
		}
		return nil // Already stored
	}

	// Check quota
	currentQuota := i.quotas[msg.RecipientID]
	if currentQuota+msg.Size > i.config.StorageQuota {
		return servicenodevm.ErrQuotaExceeded
	}

	// Check message size
	if msg.Size > i.config.MaxMessageSize {
		return servicenodevm.ErrQuotaExceeded
	}

	// Generate message ID if not set
	if msg.ID == ids.Empty {
		msg.ID = i.generateMessageID(msg)
	}

	// Store message
	i.messages[msg.ID] = msg

	// Update inbox index
	i.inboxes[msg.RecipientID] = append(i.inboxes[msg.RecipientID], msg.ID)

	// Update deduplication map
	i.dedupe[msgHash] = msg.ID

	// Update quota
	i.quotas[msg.RecipientID] = currentQuota + msg.Size

	// Update stats
	i.totalMessages++
	i.totalSize += msg.Size

	// Persist
	if err := i.persistMessage(msg); err != nil {
		return err
	}

	return i.persistStats()
}

// generateMessageID generates a unique message ID
func (i *Inbox) generateMessageID(msg *servicenodevm.StoredMessage) ids.ID {
	h := sha256.New()
	h.Write(msg.RecipientID[:])
	h.Write(msg.SenderID[:])
	h.Write(msg.Payload)
	binary.Write(h, binary.BigEndian, msg.CreatedAt.UnixNano())
	h.Write(i.nodeID[:])
	return ids.ID(h.Sum(nil))
}

// Fetch retrieves messages for an account
func (i *Inbox) Fetch(ctx context.Context, accountID [32]byte, afterTimestamp time.Time, limit int) ([]*servicenodevm.StoredMessage, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	msgIDs, exists := i.inboxes[accountID]
	if !exists {
		return nil, nil
	}

	if limit <= 0 || limit > i.config.MaxMessagesPerFetch {
		limit = i.config.MaxMessagesPerFetch
	}

	var messages []*servicenodevm.StoredMessage
	for _, msgID := range msgIDs {
		msg, ok := i.messages[msgID]
		if !ok {
			// Try to load from database
			loadedMsg, err := i.loadMessage(msgID)
			if err != nil {
				continue
			}
			msg = loadedMsg
		}

		// Skip expired messages
		if msg.IsExpired() {
			continue
		}

		// Skip messages before timestamp
		if !afterTimestamp.IsZero() && !msg.CreatedAt.After(afterTimestamp) {
			continue
		}

		messages = append(messages, msg)
		if len(messages) >= limit {
			break
		}
	}

	// Sort by creation time
	sort.Slice(messages, func(a, b int) bool {
		return messages[a].CreatedAt.Before(messages[b].CreatedAt)
	})

	return messages, nil
}

// FetchByID retrieves a specific message by ID
func (i *Inbox) FetchByID(ctx context.Context, msgID ids.ID) (*servicenodevm.StoredMessage, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	msg, ok := i.messages[msgID]
	if !ok {
		loadedMsg, err := i.loadMessage(msgID)
		if err != nil {
			return nil, servicenodevm.ErrMessageNotFound
		}
		msg = loadedMsg
	}

	if msg.IsExpired() {
		return nil, servicenodevm.ErrMessageExpired
	}

	return msg, nil
}

// MarkDelivered marks a message as delivered
func (i *Inbox) MarkDelivered(ctx context.Context, msgID ids.ID) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	msg, ok := i.messages[msgID]
	if !ok {
		loadedMsg, err := i.loadMessage(msgID)
		if err != nil {
			return servicenodevm.ErrMessageNotFound
		}
		msg = loadedMsg
		i.messages[msgID] = msg
	}

	if msg.Delivered {
		return nil // Already delivered
	}

	msg.Delivered = true
	msg.DeliveredAt = time.Now()
	i.deliveredCount++

	if err := i.persistMessage(msg); err != nil {
		return err
	}

	return i.persistStats()
}

// Delete deletes a message
func (i *Inbox) Delete(ctx context.Context, msgID ids.ID, accountID [32]byte) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	msg, ok := i.messages[msgID]
	if !ok {
		return servicenodevm.ErrMessageNotFound
	}

	// Verify ownership
	if msg.RecipientID != accountID {
		return servicenodevm.ErrMessageNotFound
	}

	// Update quota
	if i.quotas[accountID] >= msg.Size {
		i.quotas[accountID] -= msg.Size
	}

	// Remove from inbox index
	i.removeFromInbox(accountID, msgID)

	// Remove from deduplication
	msgHash := msg.Hash()
	delete(i.dedupe, msgHash)

	// Remove from messages
	delete(i.messages, msgID)

	// Update stats
	if i.totalMessages > 0 {
		i.totalMessages--
	}
	if i.totalSize >= msg.Size {
		i.totalSize -= msg.Size
	}

	// Delete from database
	key := append(messagePrefix, msgID[:]...)
	if err := i.db.Delete(key); err != nil {
		return err
	}

	return i.persistStats()
}

// PruneExpired removes expired messages
func (i *Inbox) PruneExpired(ctx context.Context) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	var toDelete []ids.ID
	for msgID, msg := range i.messages {
		if msg.IsExpired() {
			toDelete = append(toDelete, msgID)
		}
	}

	for _, msgID := range toDelete {
		msg := i.messages[msgID]

		// Update quota
		if i.quotas[msg.RecipientID] >= msg.Size {
			i.quotas[msg.RecipientID] -= msg.Size
		}

		// Remove from inbox index
		i.removeFromInbox(msg.RecipientID, msgID)

		// Remove from deduplication
		msgHash := msg.Hash()
		delete(i.dedupe, msgHash)

		// Remove from messages
		delete(i.messages, msgID)

		// Update stats
		if i.totalMessages > 0 {
			i.totalMessages--
		}
		if i.totalSize >= msg.Size {
			i.totalSize -= msg.Size
		}

		// Delete from database
		key := append(messagePrefix, msgID[:]...)
		i.db.Delete(key)
	}

	if len(toDelete) > 0 {
		i.persistStats()
	}

	return len(toDelete), nil
}

// removeFromInbox removes a message ID from an account's inbox index
func (i *Inbox) removeFromInbox(accountID [32]byte, msgID ids.ID) {
	msgIDs := i.inboxes[accountID]
	for idx, id := range msgIDs {
		if id == msgID {
			i.inboxes[accountID] = append(msgIDs[:idx], msgIDs[idx+1:]...)
			return
		}
	}
}

// GetQuota returns the current quota usage for an account
func (i *Inbox) GetQuota(accountID [32]byte) (used, total uint64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.quotas[accountID], i.config.StorageQuota
}

// GetMessageCount returns the number of messages for an account
func (i *Inbox) GetMessageCount(accountID [32]byte) int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.inboxes[accountID])
}

// GetStats returns inbox statistics
func (i *Inbox) GetStats() *InboxStats {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return &InboxStats{
		TotalMessages:  i.totalMessages,
		TotalSize:      i.totalSize,
		DeliveredCount: i.deliveredCount,
		AccountCount:   uint64(len(i.inboxes)),
	}
}

// ComputeStoreRoot computes the Merkle root of stored messages
func (i *Inbox) ComputeStoreRoot() [32]byte {
	i.mu.RLock()
	defer i.mu.RUnlock()

	// Sort message IDs for deterministic ordering
	msgIDs := make([]ids.ID, 0, len(i.messages))
	for id := range i.messages {
		msgIDs = append(msgIDs, id)
	}

	sort.Slice(msgIDs, func(a, b int) bool {
		return bytes.Compare(msgIDs[a][:], msgIDs[b][:]) < 0
	})

	// Compute leaf hashes
	leaves := make([][]byte, len(msgIDs))
	for idx, msgID := range msgIDs {
		msg := i.messages[msgID]
		msgHash := msg.Hash()
		leaves[idx] = msgHash[:]
	}

	return computeMerkleRoot(leaves)
}

// CreateStorageCommitment creates a storage commitment for an epoch
func (i *Inbox) CreateStorageCommitment(epochID uint64) *servicenodevm.StorageCommitment {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return &servicenodevm.StorageCommitment{
		NodeID:       i.nodeID,
		EpochID:      epochID,
		StoreRoot:    i.ComputeStoreRoot(),
		MessageCount: i.totalMessages,
		TotalSize:    i.totalSize,
		Timestamp:    time.Now(),
	}
}

// loadMessage loads a message from database
func (i *Inbox) loadMessage(msgID ids.ID) (*servicenodevm.StoredMessage, error) {
	key := append(messagePrefix, msgID[:]...)
	data, err := i.db.Get(key)
	if err != nil {
		return nil, err
	}

	var msg servicenodevm.StoredMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// persistMessage persists a message to database
func (i *Inbox) persistMessage(msg *servicenodevm.StoredMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	key := append(messagePrefix, msg.ID[:]...)
	return i.db.Put(key, data)
}

// persistStats persists inbox statistics
func (i *Inbox) persistStats() error {
	stats := &InboxStats{
		TotalMessages:  i.totalMessages,
		TotalSize:      i.totalSize,
		DeliveredCount: i.deliveredCount,
		AccountCount:   uint64(len(i.inboxes)),
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return err
	}

	return i.db.Put(statsPrefix, data)
}

// computeMerkleRoot computes a Merkle root from leaf hashes
func computeMerkleRoot(leaves [][]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}

	if len(leaves) == 1 {
		var root [32]byte
		copy(root[:], leaves[0])
		return root
	}

	// Pad to power of 2
	for len(leaves)&(len(leaves)-1) != 0 {
		leaves = append(leaves, leaves[len(leaves)-1])
	}

	// Build tree
	for len(leaves) > 1 {
		newLevel := make([][]byte, 0, len(leaves)/2)
		for idx := 0; idx < len(leaves); idx += 2 {
			h := sha256.New()
			h.Write(leaves[idx])
			h.Write(leaves[idx+1])
			newLevel = append(newLevel, h.Sum(nil))
		}
		leaves = newLevel
	}

	var root [32]byte
	copy(root[:], leaves[0])
	return root
}

// InboxStats holds inbox statistics
type InboxStats struct {
	TotalMessages  uint64 `json:"totalMessages"`
	TotalSize      uint64 `json:"totalSize"`
	DeliveredCount uint64 `json:"deliveredCount"`
	AccountCount   uint64 `json:"accountCount"`
}
