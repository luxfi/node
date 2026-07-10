// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/p2p/gossip"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

var (
	_ gossip.Gossipable = (*Tx)(nil)

	ErrNilSignedTx = errors.New("nil signed tx is not valid")

	errSignedTxNotInitialized = errors.New("signed tx was never initialized and is not valid")
)

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	Unsigned UnsignedTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`

	TxID  ids.ID `json:"id"`
	bytes []byte
}

func NewSigned(
	unsigned UnsignedTx,
	c pcodecs.Manager,
	signers [][]*secp256k1.PrivateKey,
) (*Tx, error) {
	res := &Tx{Unsigned: unsigned}
	return res, res.Sign(c, signers)
}

// Initialize marshals tx with the canonical ZAP codec and derives
// TxID = hash(signedBytes). This is appropriate ONLY for txs built
// fresh in this process (e.g. by the wallet or the block builder). For
// txs loaded from disk or received from the wire, use
// InitializeFromBytes, which binds the exact received bytes rather than
// re-encoding.
func (tx *Tx) Initialize(c pcodecs.Manager) error {
	signedBytes, err := c.Marshal(CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}

	unsignedBytesLen, err := c.Size(CodecVersion, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}

	unsignedBytes := signedBytes[:unsignedBytesLen]
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}

// InitializeFromBytes binds the tx to the EXACT signedBytes it was
// decoded from, deriving TxID = hash(signedBytes). It never re-marshals,
// so the TxID is exactly the hash of the bytes the chain committed.
//
// version is the codec version the bytes were decoded under (returned
// by codec.Manager.Unmarshal — always CodecVersion). It selects the
// c.Size(...) call used to split signedBytes into unsignedBytes (the
// prefix the signature covers) and the credentials suffix.
func (tx *Tx) InitializeFromBytes(c pcodecs.Manager, version uint16, signedBytes []byte) error {
	unsignedBytesLen, err := c.Size(version, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't calculate UnsignedTx marshal length: %w", err)
	}
	if unsignedBytesLen > len(signedBytes) {
		return fmt.Errorf("unsigned length %d exceeds signed length %d", unsignedBytesLen, len(signedBytes))
	}
	unsignedBytes := signedBytes[:unsignedBytesLen]
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}

// InitializeFromBytesAtVersion is the byte-preserving init used after a
// struct-level Unmarshal where the outer container (a genesis blob, a
// stored block) decoded the tx field directly — in that case we only
// have the struct value and the codec version it was decoded under, not
// the raw signed bytes (which were never sliced out of the outer stream
// by the codec).
//
// We re-derive signedBytes by Marshal'ing the just-decoded tx at the
// same version, then bind via SetBytes. This is byte-exact because:
//   1. ZAP marshal is a pure function of the struct fields under a fixed
//      slot map, so the output is byte-equal to the producer's output.
//   2. The struct fields were just populated by c.Unmarshal at the same
//      version, so no information has been lost or rotated.
//   3. The c.Size(...) inverse used by InitializeFromBytes walks the
//      same layout in the opposite direction.
//
// Therefore TxID = hash(re-marshaled bytes) = TxID the chain committed.
func (tx *Tx) InitializeFromBytesAtVersion(c pcodecs.Manager, version uint16) error {
	signedBytes, err := c.Marshal(version, tx)
	if err != nil {
		return fmt.Errorf("couldn't re-marshal tx at v%d: %w", version, err)
	}
	return tx.InitializeFromBytes(c, version, signedBytes)
}

func (tx *Tx) SetBytes(unsignedBytes, signedBytes []byte) {
	tx.Unsigned.SetBytes(unsignedBytes)
	tx.bytes = signedBytes
	tx.TxID = hash.ComputeHash256Array(signedBytes)
}

// Parse signed tx starting from its byte representation. The wire
// version is taken from the 2-byte prefix that c.Unmarshal reads (it
// must equal CodecVersion, else Unmarshal rejects it); c.Size(...) is
// then called under that version so the split point between
// unsignedBytes and the credentials suffix is correct.
//
// Parse never re-marshals: TxID = hash(signedBytes) verbatim. This is
// the byte-preserving path that all from-disk and from-wire reads go
// through to keep TxIDs equal to the committed bytes' hash.
//
// We explicitly pass the codec in Parse since some call sites (genesis,
// state fallback) must use GenesisCodec to admit txs larger than the
// max length of Codec.
func Parse(c pcodecs.Manager, signedBytes []byte) (*Tx, error) {
	tx := &Tx{}
	version, err := c.Unmarshal(signedBytes, tx)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse tx: %w", err)
	}
	if err := tx.InitializeFromBytes(c, version, signedBytes); err != nil {
		return nil, fmt.Errorf("couldn't initialize tx from bytes: %w", err)
	}
	return tx, nil
}

func (tx *Tx) Bytes() []byte {
	return tx.bytes
}

func (tx *Tx) Size() int {
	return len(tx.bytes)
}

func (tx *Tx) ID() ids.ID {
	return tx.TxID
}

func (tx *Tx) GossipID() ids.ID {
	return tx.TxID
}

// UTXOs returns the UTXOs transaction is producing.
func (tx *Tx) UTXOs() []*lux.UTXO {
	outs := tx.Unsigned.Outputs()
	utxos := make([]*lux.UTXO, len(outs))
	for i, out := range outs {
		utxos[i] = &lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        tx.TxID,
				OutputIndex: uint32(i),
			},
			Asset: lux.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
	}
	return utxos
}

// InputIDs returns the set of inputs this transaction consumes
func (tx *Tx) InputIDs() set.Set[ids.ID] {
	return tx.Unsigned.InputIDs()
}

func (tx *Tx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilSignedTx
	case tx.TxID == ids.Empty:
		return errSignedTxNotInitialized
	default:
		return tx.Unsigned.SyntacticVerify(rt)
	}
}

// Sign this transaction with the provided signers
// Note: We explicitly pass the codec in Sign since we may need to sign P-Chain
// genesis txs whose length exceed the max length of txs.Codec.
func (tx *Tx) Sign(c pcodecs.Manager, signers [][]*secp256k1.PrivateKey) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}

	// Attach credentials
	hash := hash.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.Creds = append(tx.Creds, cred) // Attach credential
	}

	signedBytes, err := c.Marshal(CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}
