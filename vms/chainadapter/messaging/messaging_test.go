// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/ids"
)

// mockEncryptor implements the Encryptor interface for testing
type mockEncryptor struct{}

func (m *mockEncryptor) Encrypt(ctx context.Context, data []byte, recipients [][32]byte) ([]byte, error) {
	// Just return the data as-is for testing
	return data, nil
}

func (m *mockEncryptor) Decrypt(ctx context.Context, ciphertext []byte, privateKey []byte) ([]byte, error) {
	return ciphertext, nil
}

func (m *mockEncryptor) DeriveConversationKey(conversationID ids.ID, members [][32]byte) ([]byte, error) {
	return make([]byte, 32), nil
}

func setupTestStore(t *testing.T) *MessageStore {
	return NewMessageStore(&mockEncryptor{})
}

func TestMessageStoreCreateConversation(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, member [32]byte
	copy(creator[:], "creator")
	copy(member[:], "member")

	members := [][32]byte{creator, member}

	conv, err := store.CreateConversation(ctx, ConversationDirect, creator, members)
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	if conv.ID == ids.Empty {
		t.Errorf("conversation ID not generated")
	}

	if conv.Type != ConversationDirect {
		t.Errorf("expected ConversationDirect type, got %d", conv.Type)
	}

	// Verify members
	convMembers := conv.MembersCRDT.GetMembers()
	if len(convMembers) != 2 {
		t.Errorf("expected 2 members, got %d", len(convMembers))
	}
}

func TestMessageStoreGetConversation(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator [32]byte
	copy(creator[:], "creator")
	members := [][32]byte{creator}

	conv, _ := store.CreateConversation(ctx, ConversationDirect, creator, members)

	// Retrieve conversation
	retrieved, err := store.GetConversation(conv.ID)
	if err != nil {
		t.Fatalf("failed to get conversation: %v", err)
	}

	if retrieved.ID != conv.ID {
		t.Errorf("wrong conversation ID")
	}
}

func TestMessageStoreConversationNotFound(t *testing.T) {
	store := setupTestStore(t)

	_, err := store.GetConversation(ids.Empty)
	if err != ErrConversationNotFound {
		t.Errorf("expected ErrConversationNotFound, got %v", err)
	}
}

func TestMessageStoreAddMember(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, newMember [32]byte
	copy(creator[:], "creator")
	copy(newMember[:], "newmember")

	members := [][32]byte{creator}
	conv, _ := store.CreateConversation(ctx, ConversationGroup, creator, members)

	// Add new member
	if err := store.AddMember(ctx, conv.ID, newMember, "member", creator); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	// Verify member added
	retrieved, _ := store.GetConversation(conv.ID)
	convMembers := retrieved.MembersCRDT.GetMembers()
	if len(convMembers) != 2 {
		t.Errorf("expected 2 members, got %d", len(convMembers))
	}

	// Check if new member is in the list
	found := false
	for _, m := range convMembers {
		if m.AccountID == newMember {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("new member not found in conversation")
	}
}

func TestMessageStoreRemoveMember(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, member [32]byte
	copy(creator[:], "creator")
	copy(member[:], "member")

	members := [][32]byte{creator, member}
	conv, _ := store.CreateConversation(ctx, ConversationGroup, creator, members)

	// Remove member
	if err := store.RemoveMember(ctx, conv.ID, member, creator); err != nil {
		t.Fatalf("failed to remove member: %v", err)
	}

	// Verify member is no longer active
	retrieved, _ := store.GetConversation(conv.ID)
	if retrieved.MembersCRDT.IsMember(member) {
		t.Errorf("member should have been removed")
	}
}

func TestMessageStoreUpdateReadMarker(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator [32]byte
	copy(creator[:], "creator")
	members := [][32]byte{creator}

	conv, _ := store.CreateConversation(ctx, ConversationDirect, creator, members)

	// Update read marker
	msgID := ids.GenerateTestID()
	if err := store.UpdateReadMarker(ctx, conv.ID, creator, msgID); err != nil {
		t.Fatalf("failed to update read marker: %v", err)
	}

	// Verify read marker
	retrieved, _ := store.GetConversation(conv.ID)
	marker := retrieved.ReadMarkersCRDT.Get(creator)
	if marker == nil {
		t.Errorf("read marker not found")
	}
	if marker != nil && marker.LastReadID != msgID {
		t.Errorf("wrong message ID in read marker")
	}
}

func TestMessageStoreRateLimit(t *testing.T) {
	store := setupTestStore(t)

	var sender [32]byte
	copy(sender[:], "sender")

	// Check should pass initially
	if err := store.CheckRateLimit(sender); err != nil {
		t.Errorf("initial rate check should pass: %v", err)
	}

	// Increment usage
	for i := 0; i < 60; i++ {
		store.IncrementRateLimit(sender)
	}

	// Check should fail after exceeding limit
	if err := store.CheckRateLimit(sender); err == nil {
		t.Errorf("rate check should fail after exceeding limit")
	}
}

func TestMembershipCRDT(t *testing.T) {
	crdt := NewMembershipCRDT()

	var member1, member2 [32]byte
	copy(member1[:], "member1")
	copy(member2[:], "member2")

	// Add members
	crdt.Add(member1, "admin", member1)
	crdt.Add(member2, "member", member1)

	members := crdt.GetMembers()
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Check membership
	if !crdt.IsMember(member1) {
		t.Errorf("member1 should be a member")
	}

	// Get member entry
	entry, _, err := crdt.GetMember(member1)
	if err != nil {
		t.Errorf("failed to get member: %v", err)
	}
	if entry.Role != "admin" {
		t.Errorf("expected admin role, got %s", entry.Role)
	}
}

func TestMembershipCRDTRemove(t *testing.T) {
	crdt := NewMembershipCRDT()

	var member1 [32]byte
	copy(member1[:], "member1")

	// Add and get tag
	tag := crdt.Add(member1, "member", member1)

	// Remove using tag
	crdt.Remove(tag)

	if crdt.IsMember(member1) {
		t.Errorf("member should have been removed")
	}
}

func TestMembershipCRDTMerge(t *testing.T) {
	crdt1 := NewMembershipCRDT()
	crdt2 := NewMembershipCRDT()

	var member1, member2, member3 [32]byte
	copy(member1[:], "member1")
	copy(member2[:], "member2")
	copy(member3[:], "member3")

	// Add different members to each
	crdt1.Add(member1, "admin", member1)
	crdt1.Add(member2, "member", member1)

	crdt2.Add(member2, "member", member1)
	crdt2.Add(member3, "member", member1)

	// Merge
	crdt1.Merge(crdt2)

	members := crdt1.GetMembers()
	if len(members) < 3 {
		t.Errorf("expected at least 3 members after merge, got %d", len(members))
	}
}

func TestReadMarkerCRDT(t *testing.T) {
	crdt := NewReadMarkerCRDT()

	var account [32]byte
	copy(account[:], "account")
	msgID := ids.GenerateTestID()
	timestamp := time.Now()

	// Update marker
	crdt.Update(account, msgID, timestamp)

	// Get marker
	marker := crdt.Get(account)
	if marker == nil {
		t.Errorf("marker should exist")
	}

	if marker != nil && marker.LastReadID != msgID {
		t.Errorf("wrong message ID")
	}
}

func TestReadMarkerCRDTLWW(t *testing.T) {
	crdt := NewReadMarkerCRDT()

	var account [32]byte
	copy(account[:], "account")

	oldMsgID := ids.GenerateTestID()
	newMsgID := ids.GenerateTestID()

	// Update with first message
	crdt.Update(account, oldMsgID, time.Now())

	// Small delay to ensure different UpdatedAt
	time.Sleep(time.Millisecond)

	// Update with second message
	crdt.Update(account, newMsgID, time.Now())

	// Should have newer marker (LWW based on UpdatedAt, not LastReadTime)
	marker := crdt.Get(account)
	if marker == nil {
		t.Errorf("marker should exist")
	}
	if marker != nil && marker.LastReadID != newMsgID {
		t.Errorf("expected newer message ID")
	}
}

func TestReadMarkerCRDTMerge(t *testing.T) {
	crdt1 := NewReadMarkerCRDT()
	crdt2 := NewReadMarkerCRDT()

	var account1, account2 [32]byte
	copy(account1[:], "account1")
	copy(account2[:], "account2")

	msg1 := ids.GenerateTestID()
	msg2 := ids.GenerateTestID()

	crdt1.Update(account1, msg1, time.Now())
	crdt2.Update(account2, msg2, time.Now())

	// Merge
	crdt1.Merge(crdt2)

	// Should have both markers
	marker1 := crdt1.Get(account1)
	marker2 := crdt1.Get(account2)

	if marker1 == nil || marker2 == nil {
		t.Errorf("merge should include markers from both CRDTs")
	}
}

func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(5, 100, 1000) // 5 per minute

	var sender [32]byte
	copy(sender[:], "sender")

	// Should allow up to limit
	for i := 0; i < 5; i++ {
		if err := limiter.Check(sender); err != nil {
			t.Errorf("should allow message %d: %v", i, err)
		}
		limiter.Increment(sender)
	}

	// Should deny over limit
	if err := limiter.Check(sender); err == nil {
		t.Errorf("should deny message over limit")
	}
}

func TestConversationTypes(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator [32]byte
	copy(creator[:], "creator")
	members := [][32]byte{creator}

	// Test ConversationDirect
	dm, _ := store.CreateConversation(ctx, ConversationDirect, creator, members)
	if dm.Type != ConversationDirect {
		t.Errorf("expected ConversationDirect type")
	}

	// Test ConversationGroup
	gc, _ := store.CreateConversation(ctx, ConversationGroup, creator, members)
	if gc.Type != ConversationGroup {
		t.Errorf("expected ConversationGroup type")
	}

	// Test ConversationBroadcast
	bc, _ := store.CreateConversation(ctx, ConversationBroadcast, creator, members)
	if bc.Type != ConversationBroadcast {
		t.Errorf("expected ConversationBroadcast type")
	}
}

func TestConversationHash(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator [32]byte
	copy(creator[:], "creator")
	members := [][32]byte{creator}

	conv, _ := store.CreateConversation(ctx, ConversationDirect, creator, members)

	hash := conv.Hash()
	if hash == [32]byte{} {
		t.Errorf("expected non-zero hash")
	}

	// Hash should be deterministic
	hash2 := conv.Hash()
	if hash != hash2 {
		t.Errorf("hash not deterministic")
	}
}

func TestSerializeDeserializeConversation(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator [32]byte
	copy(creator[:], "creator")
	members := [][32]byte{creator}

	conv, _ := store.CreateConversation(ctx, ConversationDirect, creator, members)

	// Serialize
	data, err := SerializeConversation(conv)
	if err != nil {
		t.Fatalf("failed to serialize: %v", err)
	}

	// Deserialize
	restored, err := DeserializeConversation(data)
	if err != nil {
		t.Fatalf("failed to deserialize: %v", err)
	}

	if restored.ID != conv.ID {
		t.Errorf("conversation ID mismatch")
	}

	if restored.Type != conv.Type {
		t.Errorf("conversation type mismatch")
	}
}

func TestMergeConversations(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, member1, member2 [32]byte
	copy(creator[:], "creator")
	copy(member1[:], "member1")
	copy(member2[:], "member2")

	members := [][32]byte{creator}

	// Create local conversation
	local, _ := store.CreateConversation(ctx, ConversationGroup, creator, members)
	local.MembersCRDT.Add(member1, "member", creator)

	// Create "remote" conversation with different members
	remote := &Conversation{
		ID:              local.ID,
		Type:            local.Type,
		CreatedAt:       local.CreatedAt,
		UpdatedAt:       time.Now().Add(time.Second),
		MembersCRDT:     NewMembershipCRDT(),
		ReadMarkersCRDT: NewReadMarkerCRDT(),
	}
	remote.MembersCRDT.Add(creator, "admin", creator)
	remote.MembersCRDT.Add(member2, "member", creator)

	// Merge
	merged := MergeConversations(local, remote)

	// Should have members from both
	mergedMembers := merged.MembersCRDT.GetMembers()
	if len(mergedMembers) < 2 {
		t.Errorf("expected at least 2 members after merge, got %d", len(mergedMembers))
	}
}

func TestUpdateReadMarkerNonMember(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, nonMember [32]byte
	copy(creator[:], "creator")
	copy(nonMember[:], "nonmember")

	members := [][32]byte{creator}
	conv, _ := store.CreateConversation(ctx, ConversationDirect, creator, members)

	// Try to update read marker as non-member
	msgID := ids.GenerateTestID()
	err := store.UpdateReadMarker(ctx, conv.ID, nonMember, msgID)
	if err != ErrNotMember {
		t.Errorf("expected ErrNotMember, got %v", err)
	}
}

func TestAddMemberNonAdmin(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, member, newMember [32]byte
	copy(creator[:], "creator")
	copy(member[:], "member")
	copy(newMember[:], "newmember")

	members := [][32]byte{creator, member}
	conv, _ := store.CreateConversation(ctx, ConversationGroup, creator, members)

	// Try to add member as non-admin
	err := store.AddMember(ctx, conv.ID, newMember, "member", member)
	if err != ErrNotMember {
		t.Errorf("expected ErrNotMember for non-admin adding member, got %v", err)
	}
}

func TestRemoveMemberSelfRemoval(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	var creator, member [32]byte
	copy(creator[:], "creator")
	copy(member[:], "member")

	members := [][32]byte{creator, member}
	conv, _ := store.CreateConversation(ctx, ConversationGroup, creator, members)

	// Member should be able to remove themselves
	err := store.RemoveMember(ctx, conv.ID, member, member)
	if err != nil {
		t.Fatalf("member should be able to remove themselves: %v", err)
	}

	// Verify removal
	retrieved, _ := store.GetConversation(conv.ID)
	if retrieved.MembersCRDT.IsMember(member) {
		t.Errorf("member should have been removed")
	}
}
