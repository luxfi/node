// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"sync"

	qcert "github.com/luxfi/consensus/protocol/quasar"
)

// CertStore resolves the QuasarCert that certifies a finalized block. In
// production it is filled by the cert gossip/ingest path (the producer
// follow-on, producer.go); the verify gate only READS from it. Keyed by the
// finalized position (chainID, height, blockID) so a cert can never be returned
// for the wrong block.
type CertStore interface {
	Lookup(chainID uint32, height uint64, blockID [32]byte) (*qcert.ConsensusCert, bool)
}

type certKey struct {
	chainID uint32
	height  uint64
	blockID [32]byte
}

// MemCertStore is an in-memory CertStore keyed by (chainID, height, blockID). It
// is the ingest sink the cert-gossip handler writes into (Put) and the gate
// reads from (Lookup). Safe for concurrent use.
type MemCertStore struct {
	mu    sync.RWMutex
	certs map[certKey]*qcert.ConsensusCert
}

// NewMemCertStore returns an empty in-memory cert store.
func NewMemCertStore() *MemCertStore {
	return &MemCertStore{certs: make(map[certKey]*qcert.ConsensusCert)}
}

// Put indexes a cert by its own (ChainID, Height, BlockHash). The ingest path
// MUST verify a cert before Put (verify-before-store), exactly as the gossip
// layer verifies before re-gossip; the gate re-verifies at the checkpoint so a
// store poisoned by an unverified Put still cannot finalize an invalid cert.
func (m *MemCertStore) Put(cert *qcert.ConsensusCert) {
	if cert == nil {
		return
	}
	k := certKey{chainID: cert.ChainID, height: cert.Height, blockID: cert.BlockHash}
	m.mu.Lock()
	m.certs[k] = cert
	m.mu.Unlock()
}

// Lookup returns the cert for the finalized position, or (nil, false).
func (m *MemCertStore) Lookup(chainID uint32, height uint64, blockID [32]byte) (*qcert.ConsensusCert, bool) {
	k := certKey{chainID: chainID, height: height, blockID: blockID}
	m.mu.RLock()
	c, ok := m.certs[k]
	m.mu.RUnlock()
	return c, ok
}
