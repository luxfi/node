package quasar

import (
    "crypto/sha256"
    "sync"
    "time"

    rt "github.com/luxfi/node/consensus/quasar/ringtail"
)

// Aggregator collects Ringtail shares → certificate once quorum reached.
type Aggregator struct {
    mu       sync.Mutex
    quorum   int
    shares   map[[32]byte][]rt.Share // blockID ⇒ shares
    outbox   chan Cert            // sealed certs
    timeout  time.Duration
}

func NewAggregator(quorum int, d time.Duration) *Aggregator {
    return &Aggregator{
        quorum:  quorum,
        shares:  make(map[[32]byte][]rt.Share),
        outbox:  make(chan Cert, 32),
        timeout: d,
    }
}

func (a *Aggregator) Add(blockID [32]byte, s rt.Share) {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.shares[blockID] = append(a.shares[blockID], s)
    if len(a.shares[blockID]) >= a.quorum {
        cert, _ := rt.Aggregate(a.shares[blockID])
        a.outbox <- Cert(cert)
        delete(a.shares, blockID) // free mem
    }
}

// Certs returns read-only channel of sealed certificates.
func (a *Aggregator) Certs() <-chan Cert { return a.outbox }

// Hash returns the SHA-256 of the serialized cert (for header field).
func Hash(c Cert) [32]byte { return sha256.Sum256(c) }