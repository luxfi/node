package engine

import (
    "sync/atomic"
    "time"

    "github.com/luxfi/node/quasar/quasar"
)

// Engine interface for Quasar integration
type Engine interface {
    MinRoundInterval() time.Duration
    OnProposal(func([32]byte) []byte)
    OnShare(func([32]byte, quasar.Share))
    InjectPQCert(quasar.Cert, [32]byte)
    SetValidator(func(*Header) bool)
    Height() uint64
}

// Header represents a block header
type Header struct {
    BlockID   [32]byte
    QuasarSig []byte
}

type quasarHook struct {
    pool   *quasar.Pool
    agg    *quasar.Aggregator
    pk     quasar.PublicKey
    quanta uint64 // atomic
}

// AttachQuasar attaches the Quasar quantum-safety overlay to a consensus engine
func AttachQuasar(e Engine, sk quasar.SecretKey, pk quasar.PublicKey, quorum int) {
    h := &quasarHook{
        pool: quasar.NewPool(sk, 64),
        agg:  quasar.NewAggregator(quorum, e.MinRoundInterval()),
        pk:   pk,
    }
    // 1️⃣ sign as soon as we see a proposal
    e.OnProposal(func(bID [32]byte) []byte {
        share := h.pool.Get()
        sig, _ := quasar.QuickSign(share, bID)
        return sig
    })
    // 2️⃣ feed incoming shares
    e.OnShare(func(bID [32]byte, share quasar.Share) {
        // Convert quasar.Share to rt.Share
        h.agg.Add(bID, quasar.ConvertToRTShare(share))
    })
    // 3️⃣ insert cert once aggregated
    go func() {
        for cert := range h.agg.Certs() {
            e.InjectPQCert(cert, quasar.Hash(cert))
            atomic.StoreUint64(&h.quanta, uint64(e.Height()))
        }
    }()
    // 4️⃣ header validity rule
    e.SetValidator(func(hdr *Header) bool {
        if !quasar.QuickVerify(h.pk, hdr.BlockID, hdr.QuasarSig) {
            return false
        }
        return true
    })
}