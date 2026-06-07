// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package messaging provides FHE-CRDT extensions for private messaging.
// It enables encrypted CRDTs for conversation metadata, group membership,
// read markers, and rate-limit counters integrated with service node storage.
package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// Errors
var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrMemberNotFound       = errors.New("member not found")
	ErrRateLimitExceeded    = errors.New("rate limit exceeded")
	ErrEncryptionFailed     = errors.New("encryption failed")
	ErrDecryptionFailed     = errors.New("decryption failed")
	ErrInvalidSignature     = errors.New("invalid signature")
	ErrNotMember            = errors.New("not a member of this conversation")
)

// ConversationType defines the type of conversation
type ConversationType uint8

const (
	ConversationDirect ConversationType = iota
	ConversationGroup
	ConversationBroadcast
)

// Conversation represents an encrypted conversation with CRDT metadata
type Conversation struct {
	ID        ids.ID           `json:"id"`
	Type      ConversationType `json:"type"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`

	// Encrypted metadata
	EncryptedName        []byte `json:"encryptedName,omitempty"`
	EncryptedDescription []byte `json:"encryptedDescription,omitempty"`
	EncryptedAvatar      []byte `json:"encryptedAvatar,omitempty"`

	// CRDT state
	MembersCRDT   *MembershipCRDT  `json:"membersCrdt"`
	ReadMarkersCRDT *ReadMarkerCRDT `json:"readMarkersCrdt"`

	// Key management
	KeyRotationEpoch uint64   `json:"keyRotationEpoch"`
	EncryptedKeys    [][]byte `json:"encryptedKeys"` // Per-member encrypted session key

	// Settings
	DisappearingMessages bool  `json:"disappearingMessages"`
	MessageTTL           int64 `json:"messageTtl"` // Seconds

	// Version for optimistic concurrency
	Version [32]byte `json:"version"`
}

// Hash returns the hash of the conversation
func (c *Conversation) Hash() [32]byte {
	h := sha256.New()
	h.Write(c.ID[:])
	binary.Write(h, binary.BigEndian, uint8(c.Type))
	h.Write(c.Version[:])
	return sha256.Sum256(h.Sum(nil))
}

// MembershipCRDT tracks group membership using an OR-Set CRDT
type MembershipCRDT struct {
	Added   map[string]*MemberEntry `json:"added"`   // uniqueTag -> MemberEntry
	Removed map[string]time.Time    `json:"removed"` // uniqueTag -> removal time

	mu sync.RWMutex
}

// MemberEntry represents a member in the membership CRDT
type MemberEntry struct {
	AccountID [32]byte  `json:"accountId"`
	Role      string    `json:"role"` // "admin", "member", "viewer"
	AddedAt   time.Time `json:"addedAt"`
	AddedBy   [32]byte  `json:"addedBy"`

	// Encrypted member-specific data
	EncryptedNickname []byte `json:"encryptedNickname,omitempty"`
}

// NewMembershipCRDT creates a new membership CRDT
func NewMembershipCRDT() *MembershipCRDT {
	return &MembershipCRDT{
		Added:   make(map[string]*MemberEntry),
		Removed: make(map[string]time.Time),
	}
}

// Add adds a member to the conversation
func (m *MembershipCRDT) Add(accountID [32]byte, role string, addedBy [32]byte) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate unique tag
	h := sha256.New()
	h.Write(accountID[:])
	binary.Write(h, binary.BigEndian, time.Now().UnixNano())
	tag := string(h.Sum(nil)[:16])

	m.Added[tag] = &MemberEntry{
		AccountID: accountID,
		Role:      role,
		AddedAt:   time.Now(),
		AddedBy:   addedBy,
	}

	return tag
}

// Remove removes a member from the conversation
func (m *MembershipCRDT) Remove(tag string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.Added[tag]; exists {
		m.Removed[tag] = time.Now()
	}
}

// GetMembers returns all active members
func (m *MembershipCRDT) GetMembers() []*MemberEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var members []*MemberEntry
	for tag, entry := range m.Added {
		if _, removed := m.Removed[tag]; !removed {
			members = append(members, entry)
		}
	}

	return members
}

// IsMember checks if an account is a member
func (m *MembershipCRDT) IsMember(accountID [32]byte) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for tag, entry := range m.Added {
		if entry.AccountID == accountID {
			if _, removed := m.Removed[tag]; !removed {
				return true
			}
		}
	}

	return false
}

// GetMember returns a member entry by account ID
func (m *MembershipCRDT) GetMember(accountID [32]byte) (*MemberEntry, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for tag, entry := range m.Added {
		if entry.AccountID == accountID {
			if _, removed := m.Removed[tag]; !removed {
				return entry, tag, nil
			}
		}
	}

	return nil, "", ErrMemberNotFound
}

// Merge merges another membership CRDT into this one
func (m *MembershipCRDT) Merge(other *MembershipCRDT) {
	m.mu.Lock()
	defer m.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	// Merge added entries
	for tag, entry := range other.Added {
		if existing, exists := m.Added[tag]; !exists || entry.AddedAt.After(existing.AddedAt) {
			m.Added[tag] = entry
		}
	}

	// Merge removed entries
	for tag, removedAt := range other.Removed {
		if existing, exists := m.Removed[tag]; !exists || removedAt.After(existing) {
			m.Removed[tag] = removedAt
		}
	}
}

// ReadMarkerCRDT tracks read markers using LWW registers
type ReadMarkerCRDT struct {
	Markers map[string]*ReadMarker `json:"markers"` // accountID hex -> ReadMarker

	mu sync.RWMutex
}

// ReadMarker represents a member's read position
type ReadMarker struct {
	AccountID    [32]byte  `json:"accountId"`
	LastReadID   ids.ID    `json:"lastReadId"`
	LastReadTime time.Time `json:"lastReadTime"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// NewReadMarkerCRDT creates a new read marker CRDT
func NewReadMarkerCRDT() *ReadMarkerCRDT {
	return &ReadMarkerCRDT{
		Markers: make(map[string]*ReadMarker),
	}
}

// Update updates a read marker (LWW semantics)
func (r *ReadMarkerCRDT) Update(accountID [32]byte, lastReadID ids.ID, lastReadTime time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := string(accountID[:])
	now := time.Now()

	if existing, exists := r.Markers[key]; exists {
		if now.After(existing.UpdatedAt) {
			existing.LastReadID = lastReadID
			existing.LastReadTime = lastReadTime
			existing.UpdatedAt = now
		}
	} else {
		r.Markers[key] = &ReadMarker{
			AccountID:    accountID,
			LastReadID:   lastReadID,
			LastReadTime: lastReadTime,
			UpdatedAt:    now,
		}
	}
}

// Get returns a read marker for an account
func (r *ReadMarkerCRDT) Get(accountID [32]byte) *ReadMarker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := string(accountID[:])
	return r.Markers[key]
}

// Merge merges another read marker CRDT into this one
func (r *ReadMarkerCRDT) Merge(other *ReadMarkerCRDT) {
	r.mu.Lock()
	defer r.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for key, marker := range other.Markers {
		if existing, exists := r.Markers[key]; !exists || marker.UpdatedAt.After(existing.UpdatedAt) {
			r.Markers[key] = marker
		}
	}
}

// RateLimiter provides rate limiting for message sending
type RateLimiter struct {
	// Per-account counters
	counters map[[32]byte]*RateCounter

	// Configuration
	maxPerMinute int
	maxPerHour   int
	maxPerDay    int

	mu sync.RWMutex
}

// RateCounter tracks message counts for an account
type RateCounter struct {
	AccountID    [32]byte
	MinuteCount  int
	HourCount    int
	DayCount     int
	MinuteReset  time.Time
	HourReset    time.Time
	DayReset     time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxPerMinute, maxPerHour, maxPerDay int) *RateLimiter {
	return &RateLimiter{
		counters:     make(map[[32]byte]*RateCounter),
		maxPerMinute: maxPerMinute,
		maxPerHour:   maxPerHour,
		maxPerDay:    maxPerDay,
	}
}

// Check checks if an account can send a message
func (r *RateLimiter) Check(accountID [32]byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	counter, exists := r.counters[accountID]
	if !exists {
		counter = &RateCounter{
			AccountID:   accountID,
			MinuteReset: time.Now().Add(time.Minute),
			HourReset:   time.Now().Add(time.Hour),
			DayReset:    time.Now().Add(24 * time.Hour),
		}
		r.counters[accountID] = counter
	}

	now := time.Now()

	// Reset counters if needed
	if now.After(counter.MinuteReset) {
		counter.MinuteCount = 0
		counter.MinuteReset = now.Add(time.Minute)
	}
	if now.After(counter.HourReset) {
		counter.HourCount = 0
		counter.HourReset = now.Add(time.Hour)
	}
	if now.After(counter.DayReset) {
		counter.DayCount = 0
		counter.DayReset = now.Add(24 * time.Hour)
	}

	// Check limits
	if counter.MinuteCount >= r.maxPerMinute {
		return ErrRateLimitExceeded
	}
	if counter.HourCount >= r.maxPerHour {
		return ErrRateLimitExceeded
	}
	if counter.DayCount >= r.maxPerDay {
		return ErrRateLimitExceeded
	}

	return nil
}

// Increment increments the counters for an account
func (r *RateLimiter) Increment(accountID [32]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	counter, exists := r.counters[accountID]
	if !exists {
		return
	}

	counter.MinuteCount++
	counter.HourCount++
	counter.DayCount++
}

// MessageStore manages encrypted messages with CRDT semantics
type MessageStore struct {
	// Conversations
	conversations map[ids.ID]*Conversation

	// Rate limiter
	rateLimiter *RateLimiter

	// Encryption provider
	encryptor Encryptor

	mu sync.RWMutex
}

// Encryptor provides encryption/decryption operations
type Encryptor interface {
	// Encrypt encrypts data for the given recipients
	Encrypt(ctx context.Context, data []byte, recipients [][32]byte) ([]byte, error)

	// Decrypt decrypts data using the given private key
	Decrypt(ctx context.Context, ciphertext []byte, privateKey []byte) ([]byte, error)

	// DeriveConversationKey derives a shared key for a conversation
	DeriveConversationKey(conversationID ids.ID, members [][32]byte) ([]byte, error)
}

// NewMessageStore creates a new message store
func NewMessageStore(encryptor Encryptor) *MessageStore {
	return &MessageStore{
		conversations: make(map[ids.ID]*Conversation),
		rateLimiter:   NewRateLimiter(60, 1000, 10000),
		encryptor:     encryptor,
	}
}

// CreateConversation creates a new conversation
func (s *MessageStore) CreateConversation(ctx context.Context, convType ConversationType, creatorID [32]byte, members [][32]byte) (*Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate conversation ID
	h := sha256.New()
	h.Write(creatorID[:])
	for _, member := range members {
		h.Write(member[:])
	}
	binary.Write(h, binary.BigEndian, time.Now().UnixNano())
	convID := ids.ID(h.Sum(nil))

	// Create membership CRDT
	membership := NewMembershipCRDT()

	// Add creator as admin
	membership.Add(creatorID, "admin", creatorID)

	// Add other members
	for _, member := range members {
		if member != creatorID {
			membership.Add(member, "member", creatorID)
		}
	}

	// Create read markers CRDT
	readMarkers := NewReadMarkerCRDT()

	now := time.Now()
	conv := &Conversation{
		ID:               convID,
		Type:             convType,
		CreatedAt:        now,
		UpdatedAt:        now,
		MembersCRDT:      membership,
		ReadMarkersCRDT:  readMarkers,
		KeyRotationEpoch: 0,
	}

	// Compute initial version
	conv.Version = conv.Hash()

	s.conversations[convID] = conv

	return conv, nil
}

// GetConversation retrieves a conversation by ID
func (s *MessageStore) GetConversation(conversationID ids.ID) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conv, exists := s.conversations[conversationID]
	if !exists {
		return nil, ErrConversationNotFound
	}

	return conv, nil
}

// AddMember adds a member to a conversation
func (s *MessageStore) AddMember(ctx context.Context, conversationID ids.ID, accountID [32]byte, role string, addedBy [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv, exists := s.conversations[conversationID]
	if !exists {
		return ErrConversationNotFound
	}

	// Verify adder is a member with admin role
	adder, _, err := conv.MembersCRDT.GetMember(addedBy)
	if err != nil {
		return ErrNotMember
	}
	if adder.Role != "admin" && conv.Type == ConversationGroup {
		return ErrNotMember
	}

	conv.MembersCRDT.Add(accountID, role, addedBy)
	conv.UpdatedAt = time.Now()
	conv.Version = conv.Hash()

	return nil
}

// RemoveMember removes a member from a conversation
func (s *MessageStore) RemoveMember(ctx context.Context, conversationID ids.ID, accountID [32]byte, removedBy [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv, exists := s.conversations[conversationID]
	if !exists {
		return ErrConversationNotFound
	}

	// Verify remover has permission
	remover, _, err := conv.MembersCRDT.GetMember(removedBy)
	if err != nil {
		return ErrNotMember
	}
	if remover.Role != "admin" && removedBy != accountID {
		return ErrNotMember
	}

	_, tag, err := conv.MembersCRDT.GetMember(accountID)
	if err != nil {
		return err
	}

	conv.MembersCRDT.Remove(tag)
	conv.UpdatedAt = time.Now()
	conv.Version = conv.Hash()

	return nil
}

// UpdateReadMarker updates a read marker for an account
func (s *MessageStore) UpdateReadMarker(ctx context.Context, conversationID ids.ID, accountID [32]byte, lastReadID ids.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv, exists := s.conversations[conversationID]
	if !exists {
		return ErrConversationNotFound
	}

	// Verify account is a member
	if !conv.MembersCRDT.IsMember(accountID) {
		return ErrNotMember
	}

	conv.ReadMarkersCRDT.Update(accountID, lastReadID, time.Now())
	conv.UpdatedAt = time.Now()

	return nil
}

// CheckRateLimit checks if an account can send a message
func (s *MessageStore) CheckRateLimit(accountID [32]byte) error {
	return s.rateLimiter.Check(accountID)
}

// IncrementRateLimit increments the rate limit counters
func (s *MessageStore) IncrementRateLimit(accountID [32]byte) {
	s.rateLimiter.Increment(accountID)
}

// SerializeConversation serializes a conversation to ZAP. Replaces the
// legacy JSON codec, which was UTF-8 unsafe (binary OR-Set tags stuffed
// into Go map keys).
func SerializeConversation(conv *Conversation) ([]byte, error) {
	return encodeConversation(conv), nil
}

// DeserializeConversation deserializes a conversation from ZAP.
func DeserializeConversation(data []byte) (*Conversation, error) {
	return decodeConversation(data)
}

// MergeConversations merges two conversation states
func MergeConversations(local, remote *Conversation) *Conversation {
	// Use the newer version as base
	var result *Conversation
	if remote.UpdatedAt.After(local.UpdatedAt) {
		result = remote
		result.MembersCRDT.Merge(local.MembersCRDT)
		result.ReadMarkersCRDT.Merge(local.ReadMarkersCRDT)
	} else {
		result = local
		result.MembersCRDT.Merge(remote.MembersCRDT)
		result.ReadMarkersCRDT.Merge(remote.ReadMarkersCRDT)
	}

	result.Version = result.Hash()
	return result
}
