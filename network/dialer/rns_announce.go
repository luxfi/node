// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/net/ips"
)

// Wire format constants for RNS announcements.
// Format: [16 dest][32 ed25519][32 x25519][2 applen][appdata][64 sig][1 hops][8 timestamp]
const (
	AnnounceDestLen      = ips.RNSDestinationLen // 16 bytes
	AnnounceEd25519Len   = 32
	AnnounceX25519Len    = 32
	AnnounceAppDataLenSz = 2
	AnnounceSigLen       = 64
	AnnounceHopsLen      = 1
	AnnounceTimestampLen = 8

	// AnnounceMinSize is minimum wire size without app data.
	AnnounceMinSize = AnnounceDestLen + AnnounceEd25519Len + AnnounceX25519Len +
		AnnounceAppDataLenSz + AnnounceSigLen + AnnounceHopsLen + AnnounceTimestampLen

	// AnnounceMaxAppData limits application data size.
	AnnounceMaxAppData = 1024

	// DefaultAnnounceInterval between periodic announcements.
	DefaultAnnounceInterval = 5 * time.Minute
	// DefaultAnnounceExpiry is how long announcements remain valid.
	DefaultAnnounceExpiry = 30 * time.Minute
	// DefaultMaxHops limits propagation depth.
	DefaultMaxHops = 16
	// DefaultDestTableSize is LRU cache capacity.
	DefaultDestTableSize = 10000
)

// Announce errors.
var (
	ErrAnnounceInvalidSize      = errors.New("announce: invalid wire size")
	ErrAnnounceInvalidSignature = errors.New("announce: invalid signature")
	ErrAnnounceExpired          = errors.New("announce: expired")
	ErrAnnounceMaxHops          = errors.New("announce: max hops exceeded")
	ErrAnnounceDestMismatch     = errors.New("announce: destination mismatch")
	ErrAnnounceAppDataTooLarge  = errors.New("announce: app data too large")
	ErrAnnounceFutureTimestamp  = errors.New("announce: timestamp in future")
	ErrAnnounceNoIdentity       = errors.New("announce: identity not set")
	ErrDestinationUnknown       = errors.New("destination unknown")
)

// Announce represents a Reticulum destination announcement.
type Announce struct {
	Destination   [AnnounceDestLen]byte
	Ed25519PubKey [AnnounceEd25519Len]byte
	X25519PubKey  [AnnounceX25519Len]byte
	AppData       []byte
	Signature     [AnnounceSigLen]byte
	Hops          uint8
	Timestamp     int64 // Unix milliseconds
}

// SignableBytes returns bytes covered by the signature.
func (a *Announce) SignableBytes() []byte {
	size := AnnounceDestLen + AnnounceEd25519Len + AnnounceX25519Len +
		AnnounceAppDataLenSz + len(a.AppData) + AnnounceHopsLen + AnnounceTimestampLen
	buf := make([]byte, size)
	off := 0

	copy(buf[off:], a.Destination[:])
	off += AnnounceDestLen

	copy(buf[off:], a.Ed25519PubKey[:])
	off += AnnounceEd25519Len

	copy(buf[off:], a.X25519PubKey[:])
	off += AnnounceX25519Len

	binary.BigEndian.PutUint16(buf[off:], uint16(len(a.AppData)))
	off += AnnounceAppDataLenSz

	copy(buf[off:], a.AppData)
	off += len(a.AppData)

	buf[off] = a.Hops
	off++

	binary.BigEndian.PutUint64(buf[off:], uint64(a.Timestamp))
	return buf
}

// Sign signs the announcement with an Ed25519 private key.
func (a *Announce) Sign(priv ed25519.PrivateKey) {
	sig := ed25519.Sign(priv, a.SignableBytes())
	copy(a.Signature[:], sig)
}

// Verify checks the signature against the embedded public key.
func (a *Announce) Verify() bool {
	return ed25519.Verify(a.Ed25519PubKey[:], a.SignableBytes(), a.Signature[:])
}

// VerifyDestination checks destination matches the public keys.
func (a *Announce) VerifyDestination() bool {
	computed, err := DestinationFromPublicKeys(a.Ed25519PubKey[:], a.X25519PubKey[:])
	if err != nil {
		return false
	}
	return a.Destination == computed
}

// Marshal serializes to wire format.
func (a *Announce) Marshal() []byte {
	size := AnnounceMinSize + len(a.AppData)
	buf := make([]byte, size)
	off := 0

	copy(buf[off:], a.Destination[:])
	off += AnnounceDestLen

	copy(buf[off:], a.Ed25519PubKey[:])
	off += AnnounceEd25519Len

	copy(buf[off:], a.X25519PubKey[:])
	off += AnnounceX25519Len

	binary.BigEndian.PutUint16(buf[off:], uint16(len(a.AppData)))
	off += AnnounceAppDataLenSz

	copy(buf[off:], a.AppData)
	off += len(a.AppData)

	copy(buf[off:], a.Signature[:])
	off += AnnounceSigLen

	buf[off] = a.Hops
	off++

	binary.BigEndian.PutUint64(buf[off:], uint64(a.Timestamp))
	return buf
}

// UnmarshalAnnounce deserializes from wire format.
func UnmarshalAnnounce(data []byte) (*Announce, error) {
	if len(data) < AnnounceMinSize {
		return nil, ErrAnnounceInvalidSize
	}

	a := &Announce{}
	off := 0

	copy(a.Destination[:], data[off:off+AnnounceDestLen])
	off += AnnounceDestLen

	copy(a.Ed25519PubKey[:], data[off:off+AnnounceEd25519Len])
	off += AnnounceEd25519Len

	copy(a.X25519PubKey[:], data[off:off+AnnounceX25519Len])
	off += AnnounceX25519Len

	appDataLen := int(binary.BigEndian.Uint16(data[off:]))
	off += AnnounceAppDataLenSz

	if appDataLen > AnnounceMaxAppData {
		return nil, ErrAnnounceAppDataTooLarge
	}

	if len(data) != AnnounceMinSize+appDataLen {
		return nil, ErrAnnounceInvalidSize
	}

	if appDataLen > 0 {
		a.AppData = make([]byte, appDataLen)
		copy(a.AppData, data[off:off+appDataLen])
		off += appDataLen
	}

	copy(a.Signature[:], data[off:off+AnnounceSigLen])
	off += AnnounceSigLen

	a.Hops = data[off]
	off++

	a.Timestamp = int64(binary.BigEndian.Uint64(data[off:]))
	return a, nil
}

// AnnounceHandler receives validated announcements.
type AnnounceHandler interface {
	OnAnnounce(announce *Announce) error
}

// AnnounceHandlerFunc adapts a function to AnnounceHandler.
type AnnounceHandlerFunc func(*Announce) error

func (f AnnounceHandlerFunc) OnAnnounce(a *Announce) error {
	return f(a)
}

// destEntry is an LRU destination table entry.
type destEntry struct {
	announce   *Announce
	receivedAt time.Time
	prev, next *destEntry
	key        [AnnounceDestLen]byte
}

// DestTable tracks destinations with LRU eviction and expiry.
type DestTable struct {
	mu      sync.RWMutex
	entries map[[AnnounceDestLen]byte]*destEntry
	head    *destEntry // Most recently used
	tail    *destEntry // Least recently used
	maxSize int
	expiry  time.Duration
}

// NewDestTable creates a destination table.
func NewDestTable(maxSize int, expiry time.Duration) *DestTable {
	return &DestTable{
		entries: make(map[[AnnounceDestLen]byte]*destEntry),
		maxSize: maxSize,
		expiry:  expiry,
	}
}

// Get retrieves an announcement by destination.
func (t *DestTable) Get(dest [AnnounceDestLen]byte) *Announce {
	t.mu.RLock()
	entry, ok := t.entries[dest]
	t.mu.RUnlock()

	if !ok {
		return nil
	}

	if time.Since(entry.receivedAt) > t.expiry {
		t.mu.Lock()
		t.removeEntry(entry)
		t.mu.Unlock()
		return nil
	}

	t.mu.Lock()
	t.moveToHead(entry)
	t.mu.Unlock()

	return entry.announce
}

// Put stores an announcement. Returns true if newer than existing.
func (t *DestTable) Put(a *Announce) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if existing, ok := t.entries[a.Destination]; ok {
		if a.Timestamp <= existing.announce.Timestamp {
			return false
		}
		existing.announce = a
		existing.receivedAt = time.Now()
		t.moveToHead(existing)
		return true
	}

	entry := &destEntry{
		announce:   a,
		receivedAt: time.Now(),
		key:        a.Destination,
	}

	if len(t.entries) >= t.maxSize {
		t.evictLRU()
	}

	t.entries[a.Destination] = entry
	t.addToHead(entry)
	return true
}

// Remove deletes a destination.
func (t *DestTable) Remove(dest [AnnounceDestLen]byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if entry, ok := t.entries[dest]; ok {
		t.removeEntry(entry)
	}
}

// Len returns entry count.
func (t *DestTable) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.entries)
}

// All returns all non-expired announcements.
func (t *DestTable) All() []*Announce {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	result := make([]*Announce, 0, len(t.entries))
	for _, entry := range t.entries {
		if now.Sub(entry.receivedAt) <= t.expiry {
			result = append(result, entry.announce)
		}
	}
	return result
}

// Prune removes expired entries. Returns count pruned.
func (t *DestTable) Prune() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	pruned := 0
	for _, entry := range t.entries {
		if now.Sub(entry.receivedAt) > t.expiry {
			t.removeEntry(entry)
			pruned++
		}
	}
	return pruned
}

func (t *DestTable) moveToHead(entry *destEntry) {
	if entry == t.head {
		return
	}
	t.removeFromList(entry)
	t.addToHead(entry)
}

func (t *DestTable) addToHead(entry *destEntry) {
	entry.prev = nil
	entry.next = t.head
	if t.head != nil {
		t.head.prev = entry
	}
	t.head = entry
	if t.tail == nil {
		t.tail = entry
	}
}

func (t *DestTable) removeFromList(entry *destEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		t.head = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		t.tail = entry.prev
	}
}

func (t *DestTable) removeEntry(entry *destEntry) {
	delete(t.entries, entry.key)
	t.removeFromList(entry)
}

func (t *DestTable) evictLRU() {
	if t.tail != nil {
		t.removeEntry(t.tail)
	}
}

// AnnouncerConfig configures the Announcer.
type AnnouncerConfig struct {
	AnnounceInterval time.Duration
	AnnounceExpiry   time.Duration
	MaxHops          uint8
	DestTableSize    int
	ClockSkew        time.Duration
}

// DefaultAnnouncerConfig returns defaults.
func DefaultAnnouncerConfig() AnnouncerConfig {
	return AnnouncerConfig{
		AnnounceInterval: DefaultAnnounceInterval,
		AnnounceExpiry:   DefaultAnnounceExpiry,
		MaxHops:          DefaultMaxHops,
		DestTableSize:    DefaultDestTableSize,
		ClockSkew:        time.Minute,
	}
}

// Announcer manages announcement creation, validation, and propagation.
type Announcer struct {
	config    AnnouncerConfig
	log       log.Logger
	destTable *DestTable
	handlers  []AnnounceHandler

	mu          sync.RWMutex
	identity    *RNSIdentity
	appData     []byte
	broadcastFn func(*Announce) error

	stopCh    chan struct{}
	stoppedCh chan struct{}
}

// NewAnnouncer creates an Announcer.
func NewAnnouncer(config AnnouncerConfig, logger log.Logger) *Announcer {
	if config.DestTableSize <= 0 {
		config.DestTableSize = DefaultDestTableSize
	}
	if config.AnnounceExpiry <= 0 {
		config.AnnounceExpiry = DefaultAnnounceExpiry
	}

	return &Announcer{
		config:    config,
		log:       logger,
		destTable: NewDestTable(config.DestTableSize, config.AnnounceExpiry),
		handlers:  make([]AnnounceHandler, 0),
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// SetIdentity sets the identity for signing announcements.
func (a *Announcer) SetIdentity(identity *RNSIdentity, appData []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.identity = identity
	a.appData = appData

	a.log.Info("identity set for announcements",
		log.String("destination", destinationHex(identity.Destination())),
	)
}

// SetBroadcastFunc sets the function to broadcast announcements.
func (a *Announcer) SetBroadcastFunc(fn func(*Announce) error) {
	a.mu.Lock()
	a.broadcastFn = fn
	a.mu.Unlock()
}

// AddHandler registers a handler for received announcements.
func (a *Announcer) AddHandler(h AnnounceHandler) {
	a.mu.Lock()
	a.handlers = append(a.handlers, h)
	a.mu.Unlock()
}

// CreateAnnounce creates a signed announcement for our identity.
func (a *Announcer) CreateAnnounce() (*Announce, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.identity == nil {
		return nil, ErrAnnounceNoIdentity
	}

	dest := a.identity.Destination()
	xPub := a.identity.X25519PublicKey()

	ann := &Announce{
		Destination: dest,
		AppData:     a.appData,
		Hops:        0,
		Timestamp:   time.Now().UnixMilli(),
	}
	copy(ann.Ed25519PubKey[:], a.identity.SigningPublicKey())
	copy(ann.X25519PubKey[:], xPub[:])

	sig := a.identity.Sign(ann.SignableBytes())
	copy(ann.Signature[:], sig)

	return ann, nil
}

// Validate checks an announcement for validity.
func (a *Announcer) Validate(ann *Announce) error {
	if !ann.VerifyDestination() {
		return ErrAnnounceDestMismatch
	}

	if !ann.Verify() {
		return ErrAnnounceInvalidSignature
	}

	if ann.Hops > a.config.MaxHops {
		return ErrAnnounceMaxHops
	}

	now := time.Now()
	annTime := time.UnixMilli(ann.Timestamp)

	if annTime.After(now.Add(a.config.ClockSkew)) {
		return ErrAnnounceFutureTimestamp
	}

	if now.Sub(annTime) > a.config.AnnounceExpiry {
		return ErrAnnounceExpired
	}

	return nil
}

// HandleReceived processes a received announcement.
// Returns the announcement for forwarding (with hops incremented) or nil.
func (a *Announcer) HandleReceived(data []byte) (*Announce, error) {
	ann, err := UnmarshalAnnounce(data)
	if err != nil {
		return nil, err
	}

	if err := a.Validate(ann); err != nil {
		return nil, err
	}

	if !a.destTable.Put(ann) {
		return nil, nil // Not newer, don't forward
	}

	a.log.Debug("received valid announcement",
		log.String("destination", destinationHex(ann.Destination)),
		log.Uint8("hops", ann.Hops),
	)

	a.mu.RLock()
	handlers := a.handlers
	a.mu.RUnlock()

	for _, h := range handlers {
		if err := h.OnAnnounce(ann); err != nil {
			a.log.Warn("announce handler error", log.Err(err))
		}
	}

	if ann.Hops >= a.config.MaxHops {
		return nil, nil
	}

	forward := *ann
	forward.Hops++
	return &forward, nil
}

// Lookup returns the announcement for a destination.
func (a *Announcer) Lookup(dest [AnnounceDestLen]byte) *Announce {
	return a.destTable.Get(dest)
}

// DestTable returns the underlying destination table.
func (a *Announcer) DestTable() *DestTable {
	return a.destTable
}

// Start begins periodic announcement broadcasting.
func (a *Announcer) Start() {
	if a.config.AnnounceInterval <= 0 {
		return
	}
	go a.announceLoop()
}

func (a *Announcer) announceLoop() {
	defer close(a.stoppedCh)

	ticker := time.NewTicker(a.config.AnnounceInterval)
	defer ticker.Stop()

	a.broadcastOurAnnounce()

	for {
		select {
		case <-ticker.C:
			a.broadcastOurAnnounce()
		case <-a.stopCh:
			return
		}
	}
}

func (a *Announcer) broadcastOurAnnounce() {
	a.mu.RLock()
	if a.identity == nil || a.broadcastFn == nil {
		a.mu.RUnlock()
		return
	}
	a.mu.RUnlock()

	ann, err := a.CreateAnnounce()
	if err != nil {
		a.log.Error("failed to create announcement", log.Err(err))
		return
	}

	a.mu.RLock()
	broadcastFn := a.broadcastFn
	a.mu.RUnlock()

	if err := broadcastFn(ann); err != nil {
		a.log.Warn("failed to broadcast announcement", log.Err(err))
		return
	}

	a.log.Debug("broadcast announcement",
		log.String("destination", destinationHex(ann.Destination)),
	)
}

// Stop halts periodic announcements.
func (a *Announcer) Stop() {
	close(a.stopCh)
	<-a.stoppedCh
}

// RNSAnnouncer wraps Announcer with the interface expected by rns_transport.go.
type RNSAnnouncer struct {
	*Announcer
	mu          sync.RWMutex
	table       map[[ips.RNSDestinationLen]byte]*AnnounceEntry
	handlers    []func(dest [ips.RNSDestinationLen]byte, entry *AnnounceEntry)
	gatewayAddr string
	listener    net.Listener
}

// RNSAnnouncerConfig configures the RNS announcer.
type RNSAnnouncerConfig struct {
	AnnounceInterval time.Duration
	GatewayAddr      string
	ListenAddr       string
}

// DefaultRNSAnnouncerConfig returns defaults.
func DefaultRNSAnnouncerConfig() RNSAnnouncerConfig {
	return RNSAnnouncerConfig{
		AnnounceInterval: DefaultAnnounceInterval,
	}
}

// NewRNSAnnouncer creates an RNS announcer wrapping an identity.
// The logger parameter is optional for backwards compatibility.
func NewRNSAnnouncer(identity *RNSIdentity, config RNSAnnouncerConfig, loggers ...log.Logger) *RNSAnnouncer {
	var logger log.Logger
	if len(loggers) > 0 {
		logger = loggers[0]
	} else {
		logger = log.NewNoOpLogger()
	}

	annConfig := DefaultAnnouncerConfig()
	annConfig.AnnounceInterval = config.AnnounceInterval

	a := NewAnnouncer(annConfig, logger)
	a.SetIdentity(identity, nil)

	return &RNSAnnouncer{
		Announcer:   a,
		table:       make(map[[ips.RNSDestinationLen]byte]*AnnounceEntry),
		gatewayAddr: config.GatewayAddr,
	}
}

// Start begins announcing and listening for announcements.
func (a *RNSAnnouncer) Start() error {
	a.Announcer.Start()
	return nil
}

// Announce broadcasts our destination to the network.
func (a *RNSAnnouncer) Announce() error {
	ann, err := a.CreateAnnounce()
	if err != nil {
		return err
	}

	// If we have a gateway, send to it
	if a.gatewayAddr != "" {
		conn, err := net.DialTimeout("tcp", a.gatewayAddr, 5*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err = conn.Write(ann.Marshal())
		return err
	}
	return nil
}

// ProcessAnnouncement processes a received announcement packet.
func (a *RNSAnnouncer) ProcessAnnouncement(packet []byte, transportAddr netip.AddrPort) error {
	ann, err := UnmarshalAnnounce(packet)
	if err != nil {
		return err
	}

	if err := a.Validate(ann); err != nil {
		return err
	}

	// Store in underlying destination table
	a.destTable.Put(ann)

	// Create entry for the legacy interface
	entry := &AnnounceEntry{
		Destination:   ann.Destination,
		SigningKey:    ann.Ed25519PubKey[:],
		ExchangeKey:   ann.X25519PubKey,
		TransportAddr: transportAddr,
		LastSeen:      time.Now(),
		ExpiresAt:     time.Now().Add(DefaultAnnounceExpiry),
		Hops:          ann.Hops,
	}

	a.mu.Lock()
	a.table[ann.Destination] = entry
	handlers := make([]func(dest [ips.RNSDestinationLen]byte, entry *AnnounceEntry), len(a.handlers))
	copy(handlers, a.handlers)
	a.mu.Unlock()

	// Notify handlers
	for _, h := range handlers {
		h(ann.Destination, entry)
	}

	return nil
}

// RegisterHandler adds a handler (legacy interface for rns_transport.go).
func (a *RNSAnnouncer) RegisterHandler(handler interface{}) {
	switch h := handler.(type) {
	case func(dest [ips.RNSDestinationLen]byte, entry *AnnounceEntry):
		a.mu.Lock()
		a.handlers = append(a.handlers, h)
		a.mu.Unlock()
	case AnnounceHandler:
		a.AddHandler(h)
	}
}

// Lookup returns the underlying Announce for a destination (for rns_transport.go).
// Returns nil if destination is unknown.
func (a *RNSAnnouncer) Lookup(dest [ips.RNSDestinationLen]byte) *Announce {
	return a.destTable.Get(dest)
}

// LookupEntry returns the entry for a destination with error handling.
func (a *RNSAnnouncer) LookupEntry(dest [ips.RNSDestinationLen]byte) (*AnnounceEntry, error) {
	a.mu.RLock()
	entry, ok := a.table[dest]
	a.mu.RUnlock()

	if ok && time.Now().Before(entry.ExpiresAt) {
		return entry, nil
	}

	// Fall back to announce table
	ann := a.destTable.Get(dest)
	if ann == nil {
		return nil, ErrDestinationUnknown
	}

	return &AnnounceEntry{
		Destination: ann.Destination,
		SigningKey:  ann.Ed25519PubKey[:],
		ExchangeKey: ann.X25519PubKey,
		LastSeen:    time.Now(),
		ExpiresAt:   time.Now().Add(DefaultAnnounceExpiry),
		Hops:        ann.Hops,
	}, nil
}

// AddEntry manually adds an entry to the table.
func (a *RNSAnnouncer) AddEntry(entry *AnnounceEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.table[entry.Destination] = entry
}

// GetTable returns a copy of the destination table (for rns_transport.go).
func (a *RNSAnnouncer) GetTable() map[[ips.RNSDestinationLen]byte]*AnnounceEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[[ips.RNSDestinationLen]byte]*AnnounceEntry, len(a.table))
	for k, v := range a.table {
		entryCopy := *v
		result[k] = &entryCopy
	}
	return result
}

// Size returns the number of known destinations.
func (a *RNSAnnouncer) Size() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.table)
}

// AnnounceEntry contains information about a known destination.
type AnnounceEntry struct {
	Destination   [ips.RNSDestinationLen]byte
	SigningKey    ed25519.PublicKey
	ExchangeKey   [32]byte
	TransportAddr netip.AddrPort
	LastSeen      time.Time
	ExpiresAt     time.Time
	Hops          uint8
}

var _ AnnounceHandler = (AnnounceHandlerFunc)(nil)
