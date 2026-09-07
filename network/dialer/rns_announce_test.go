// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/netip"
	"testing"
	"time"

	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
)

func createTestAnnounce(t *testing.T, appData []byte) (*Announce, *RNSIdentity) {
	t.Helper()

	identity, err := NewRNSIdentity()
	require.NoError(t, err)

	dest := identity.Destination()
	xPub := identity.X25519PublicKey()

	ann := &Announce{
		Destination: dest,
		AppData:     appData,
		Hops:        0,
		Timestamp:   time.Now().UnixMilli(),
	}
	copy(ann.Ed25519PubKey[:], identity.SigningPublicKey())
	copy(ann.X25519PubKey[:], xPub[:])

	sig := identity.Sign(ann.SignableBytes())
	copy(ann.Signature[:], sig)

	return ann, identity
}

func TestAnnounceSignVerify(t *testing.T) {
	require := require.New(t)

	ann, _ := createTestAnnounce(t, []byte("validator:node123"))

	// Verify destination derivation
	require.True(ann.VerifyDestination())

	// Verify signature
	require.True(ann.Verify())

	// Tamper with data and verify fails
	ann.Hops = 5
	require.False(ann.Verify())
}

func TestAnnounceMarshalUnmarshal(t *testing.T) {
	require := require.New(t)

	appData := []byte("test-validator-info")
	ann, _ := createTestAnnounce(t, appData)

	// Marshal
	data := ann.Marshal()
	expectedSize := AnnounceMinSize + len(appData)
	require.Len(data, expectedSize)

	// Unmarshal
	ann2, err := UnmarshalAnnounce(data)
	require.NoError(err)

	// Compare fields
	require.Equal(ann.Destination, ann2.Destination)
	require.Equal(ann.Ed25519PubKey, ann2.Ed25519PubKey)
	require.Equal(ann.X25519PubKey, ann2.X25519PubKey)
	require.Equal(ann.AppData, ann2.AppData)
	require.Equal(ann.Signature, ann2.Signature)
	require.Equal(ann.Hops, ann2.Hops)
	require.Equal(ann.Timestamp, ann2.Timestamp)

	// Verify unmarshaled
	require.True(ann2.Verify())
	require.True(ann2.VerifyDestination())
}

func TestAnnounceMarshalEmptyAppData(t *testing.T) {
	require := require.New(t)

	ann, _ := createTestAnnounce(t, nil)

	data := ann.Marshal()
	require.Len(data, AnnounceMinSize)

	ann2, err := UnmarshalAnnounce(data)
	require.NoError(err)
	require.Len(ann2.AppData, 0)
	require.True(ann2.Verify())
}

func TestUnmarshalErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{
			name:    "too short",
			data:    make([]byte, AnnounceMinSize-1),
			wantErr: ErrAnnounceInvalidSize,
		},
		{
			name: "app data too large",
			data: func() []byte {
				// Create data with appDataLen > AnnounceMaxAppData
				data := make([]byte, AnnounceMinSize)
				// Set appDataLen at offset 80 (16+32+32)
				data[80] = 0xFF // High byte
				data[81] = 0xFF // Low byte = 65535
				return data
			}(),
			wantErr: ErrAnnounceAppDataTooLarge,
		},
		{
			name: "size mismatch with appdata len",
			data: func() []byte {
				data := make([]byte, AnnounceMinSize)
				// Set appDataLen to 10 but don't include the data
				data[80] = 0x00
				data[81] = 0x0A // 10 bytes expected
				return data
			}(),
			wantErr: ErrAnnounceInvalidSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalAnnounce(tt.data)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestDestTableBasic(t *testing.T) {
	require := require.New(t)

	table := NewDestTable(100, time.Hour)
	require.Equal(0, table.Len())

	ann, _ := createTestAnnounce(t, []byte("test"))

	// Put
	require.True(table.Put(ann))
	require.Equal(1, table.Len())

	// Get
	got := table.Get(ann.Destination)
	require.NotNil(got)
	require.Equal(ann.Timestamp, got.Timestamp)

	// Put older - should return false
	older, _ := createTestAnnounce(t, []byte("older"))
	older.Destination = ann.Destination
	older.Ed25519PubKey = ann.Ed25519PubKey
	older.X25519PubKey = ann.X25519PubKey
	older.Timestamp = ann.Timestamp - 1000
	require.False(table.Put(older))

	// Put newer - should return true
	newer, _ := createTestAnnounce(t, []byte("newer"))
	newer.Destination = ann.Destination
	newer.Ed25519PubKey = ann.Ed25519PubKey
	newer.X25519PubKey = ann.X25519PubKey
	newer.Timestamp = ann.Timestamp + 1000
	require.True(table.Put(newer))

	got = table.Get(ann.Destination)
	require.Equal(newer.Timestamp, got.Timestamp)

	// Remove
	table.Remove(ann.Destination)
	require.Equal(0, table.Len())
	require.Nil(table.Get(ann.Destination))
}

func TestDestTableLRUEviction(t *testing.T) {
	require := require.New(t)

	table := NewDestTable(3, time.Hour)

	// Create 4 entries
	entries := make([]*Announce, 4)
	for i := 0; i < 4; i++ {
		ann, _ := createTestAnnounce(t, nil)
		entries[i] = ann
	}

	// Add first 3
	for i := 0; i < 3; i++ {
		require.True(table.Put(entries[i]))
	}
	require.Equal(3, table.Len())

	// Access entry 0 to make it recently used
	table.Get(entries[0].Destination)

	// Add 4th - should evict entry 1 (LRU)
	require.True(table.Put(entries[3]))
	require.Equal(3, table.Len())

	// Entry 1 should be evicted
	require.Nil(table.Get(entries[1].Destination))

	// Entry 0, 2, 3 should exist
	require.NotNil(table.Get(entries[0].Destination))
	require.NotNil(table.Get(entries[2].Destination))
	require.NotNil(table.Get(entries[3].Destination))
}

func TestDestTableExpiry(t *testing.T) {
	require := require.New(t)

	// Very short expiry
	table := NewDestTable(100, 50*time.Millisecond)

	ann, _ := createTestAnnounce(t, nil)

	require.True(table.Put(ann))
	require.NotNil(table.Get(ann.Destination))

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	require.Nil(table.Get(ann.Destination))
	require.Equal(0, table.Len())
}

func TestDestTablePrune(t *testing.T) {
	require := require.New(t)

	table := NewDestTable(100, 50*time.Millisecond)

	// Add several entries
	for i := 0; i < 5; i++ {
		ann, _ := createTestAnnounce(t, nil)
		table.Put(ann)
	}
	require.Equal(5, table.Len())

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Prune
	pruned := table.Prune()
	require.Equal(5, pruned)
	require.Equal(0, table.Len())
}

func TestDestTableAll(t *testing.T) {
	require := require.New(t)

	table := NewDestTable(100, time.Hour)

	for i := 0; i < 3; i++ {
		ann, _ := createTestAnnounce(t, nil)
		table.Put(ann)
	}

	all := table.All()
	require.Len(all, 3)
}

func TestAnnouncerValidate(t *testing.T) {
	require := require.New(t)

	config := DefaultAnnouncerConfig()
	announcer := NewAnnouncer(config, log.NewNoOpLogger())

	// Valid announcement
	ann, identity := createTestAnnounce(t, []byte("test"))
	require.NoError(announcer.Validate(ann))

	// Invalid signature - create unsigned announcement
	badSig := &Announce{
		Destination: identity.Destination(),
		AppData:     []byte("test"),
		Hops:        0,
		Timestamp:   time.Now().UnixMilli(),
	}
	copy(badSig.Ed25519PubKey[:], identity.SigningPublicKey())
	xPub := identity.X25519PublicKey()
	copy(badSig.X25519PubKey[:], xPub[:])
	// Signature is all zeros - invalid
	require.ErrorIs(announcer.Validate(badSig), ErrAnnounceInvalidSignature)

	// Expired
	expired, identity2 := createTestAnnounce(t, []byte("test"))
	expired.Timestamp = time.Now().Add(-time.Hour).UnixMilli()
	sig := identity2.Sign(expired.SignableBytes())
	copy(expired.Signature[:], sig)
	require.ErrorIs(announcer.Validate(expired), ErrAnnounceExpired)

	// Future timestamp
	future, identity3 := createTestAnnounce(t, []byte("test"))
	future.Timestamp = time.Now().Add(10 * time.Minute).UnixMilli()
	sig = identity3.Sign(future.SignableBytes())
	copy(future.Signature[:], sig)
	require.ErrorIs(announcer.Validate(future), ErrAnnounceFutureTimestamp)

	// Max hops exceeded
	maxHops, identity4 := createTestAnnounce(t, []byte("test"))
	maxHops.Hops = DefaultMaxHops + 1
	sig = identity4.Sign(maxHops.SignableBytes())
	copy(maxHops.Signature[:], sig)
	require.ErrorIs(announcer.Validate(maxHops), ErrAnnounceMaxHops)

	// Destination mismatch
	mismatch, identity5 := createTestAnnounce(t, []byte("test"))
	mismatch.Destination[0] ^= 0xFF // Corrupt destination
	sig = identity5.Sign(mismatch.SignableBytes())
	copy(mismatch.Signature[:], sig)
	require.ErrorIs(announcer.Validate(mismatch), ErrAnnounceDestMismatch)
}

func TestAnnouncerHandleReceived(t *testing.T) {
	require := require.New(t)

	config := DefaultAnnouncerConfig()
	announcer := NewAnnouncer(config, log.NewNoOpLogger())

	// Track received announcements
	received := make([]*Announce, 0)
	announcer.AddHandler(AnnounceHandlerFunc(func(a *Announce) error {
		received = append(received, a)
		return nil
	}))

	ann, identity := createTestAnnounce(t, []byte("validator-info"))
	data := ann.Marshal()

	// Handle first time
	forward, err := announcer.HandleReceived(data)
	require.NoError(err)
	require.NotNil(forward)
	require.Equal(uint8(1), forward.Hops) // Incremented for forwarding
	require.Len(received, 1)

	// Handle same again - should not forward (not newer)
	forward2, err := announcer.HandleReceived(data)
	require.NoError(err)
	require.Nil(forward2)
	require.Len(received, 1) // Handler not called again

	// Handle newer (ensure timestamp is definitely later)
	newer := &Announce{
		Destination: ann.Destination,
		AppData:     []byte("updated-info"),
		Hops:        0,
		Timestamp:   ann.Timestamp + 1000, // 1 second later
	}
	copy(newer.Ed25519PubKey[:], ann.Ed25519PubKey[:])
	copy(newer.X25519PubKey[:], ann.X25519PubKey[:])
	sig := identity.Sign(newer.SignableBytes())
	copy(newer.Signature[:], sig)

	forward3, err := announcer.HandleReceived(newer.Marshal())
	require.NoError(err)
	require.NotNil(forward3)
	require.Len(received, 2)

	// Verify stored
	stored := announcer.Lookup(ann.Destination)
	require.NotNil(stored)
	require.Equal(newer.Timestamp, stored.Timestamp)
}

func TestAnnouncerMaxHopsNoForward(t *testing.T) {
	require := require.New(t)

	config := DefaultAnnouncerConfig()
	config.MaxHops = 5
	announcer := NewAnnouncer(config, log.NewNoOpLogger())

	// Create announcement at max hops
	ann, identity := createTestAnnounce(t, nil)
	ann.Hops = 5 // At max
	sig := identity.Sign(ann.SignableBytes())
	copy(ann.Signature[:], sig)

	forward, err := announcer.HandleReceived(ann.Marshal())
	require.NoError(err)
	require.Nil(forward) // Should not forward at max hops
}

func TestAnnouncerCreateAnnounce(t *testing.T) {
	require := require.New(t)

	config := DefaultAnnouncerConfig()
	announcer := NewAnnouncer(config, log.NewNoOpLogger())

	// Without identity - should fail
	_, err := announcer.CreateAnnounce()
	require.ErrorIs(err, ErrAnnounceNoIdentity)

	// Set identity
	identity, err := NewRNSIdentity()
	require.NoError(err)
	announcer.SetIdentity(identity, []byte("my-validator"))

	// Now should succeed
	ann, err := announcer.CreateAnnounce()
	require.NoError(err)
	require.NotNil(ann)
	require.True(ann.Verify())
	require.True(ann.VerifyDestination())
	require.Equal([]byte("my-validator"), ann.AppData)
	require.Equal(uint8(0), ann.Hops)
}

func TestAnnouncerBroadcast(t *testing.T) {
	require := require.New(t)

	config := DefaultAnnouncerConfig()
	config.AnnounceInterval = 50 * time.Millisecond
	announcer := NewAnnouncer(config, log.NewNoOpLogger())

	// Track broadcasts
	broadcasts := make(chan *Announce, 10)
	announcer.SetBroadcastFunc(func(a *Announce) error {
		broadcasts <- a
		return nil
	})

	identity, err := NewRNSIdentity()
	require.NoError(err)
	announcer.SetIdentity(identity, []byte("test"))

	// Start periodic announcements
	announcer.Start()

	// Should broadcast immediately and then periodically
	select {
	case ann := <-broadcasts:
		require.NotNil(ann)
		require.True(ann.Verify())
	case <-time.After(time.Second):
		t.Fatal("expected broadcast")
	}

	// Wait for second broadcast
	select {
	case ann := <-broadcasts:
		require.NotNil(ann)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected second broadcast")
	}

	announcer.Stop()
}

func TestWireFormat(t *testing.T) {
	require := require.New(t)

	appData := []byte("test-app-data")
	ann, _ := createTestAnnounce(t, appData)

	data := ann.Marshal()

	// Verify wire format layout
	// [16 dest][32 ed25519][32 x25519][2 applen][appdata][64 sig][1 hops][8 timestamp]
	off := 0

	// Destination (16 bytes)
	require.Equal(ann.Destination[:], data[off:off+16])
	off += 16

	// Ed25519 pubkey (32 bytes)
	require.Equal(ann.Ed25519PubKey[:], data[off:off+32])
	off += 32

	// X25519 pubkey (32 bytes)
	require.Equal(ann.X25519PubKey[:], data[off:off+32])
	off += 32

	// App data length (2 bytes, big endian)
	require.Equal(uint8(0), data[off])
	require.Equal(uint8(len(appData)), data[off+1])
	off += 2

	// App data
	require.Equal(appData, data[off:off+len(appData)])
	off += len(appData)

	// Signature (64 bytes)
	require.Equal(ann.Signature[:], data[off:off+64])
	off += 64

	// Hops (1 byte)
	require.Equal(ann.Hops, data[off])
	off++

	// Timestamp (8 bytes)
	require.Equal(AnnounceMinSize+len(appData), off+8)
}

func TestAnnounceHandlerFunc(t *testing.T) {
	require := require.New(t)

	called := false
	handler := AnnounceHandlerFunc(func(a *Announce) error {
		called = true
		return nil
	})

	ann, _ := createTestAnnounce(t, nil)

	err := handler.OnAnnounce(ann)
	require.NoError(err)
	require.True(called)
}

func TestRNSAnnouncerCompat(t *testing.T) {
	require := require.New(t)

	identity, err := NewRNSIdentity()
	require.NoError(err)

	config := DefaultRNSAnnouncerConfig()
	announcer := NewRNSAnnouncer(identity, config, log.NewNoOpLogger())

	require.Equal(0, announcer.Size())

	// Create and process an announcement
	ann, err := announcer.CreateAnnounce()
	require.NoError(err)

	err = announcer.ProcessAnnouncement(ann.Marshal(), netip.AddrPort{})
	require.NoError(err)
	require.Equal(1, announcer.Size())
}

func TestSignableBytesDeterministic(t *testing.T) {
	require := require.New(t)

	ann, _ := createTestAnnounce(t, []byte("test-data"))

	// SignableBytes should be deterministic
	bytes1 := ann.SignableBytes()
	bytes2 := ann.SignableBytes()
	require.Equal(bytes1, bytes2)
}

func TestAnnounceWithRawKeys(t *testing.T) {
	require := require.New(t)

	// Generate raw Ed25519 keys
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(err)

	// Generate X25519 key (random for this test)
	var x25519Pub [AnnounceX25519Len]byte
	_, err = rand.Read(x25519Pub[:])
	require.NoError(err)

	// Compute destination
	dest, err := DestinationFromPublicKeys(pub, x25519Pub[:])
	require.NoError(err)

	// Create announcement
	ann := &Announce{
		Destination: dest,
		AppData:     []byte("raw-key-test"),
		Hops:        0,
		Timestamp:   time.Now().UnixMilli(),
	}
	copy(ann.Ed25519PubKey[:], pub)
	copy(ann.X25519PubKey[:], x25519Pub[:])

	// Sign with raw private key
	ann.Sign(priv)

	// Verify
	require.True(ann.Verify())
	require.True(ann.VerifyDestination())

	// Marshal/unmarshal roundtrip
	data := ann.Marshal()
	ann2, err := UnmarshalAnnounce(data)
	require.NoError(err)
	require.True(ann2.Verify())
}
