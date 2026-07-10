// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
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

// Tx is a signed transaction: an unsigned tx (itself a zap buffer) plus its
// credentials. The signed bytes are unsigned ‖ creds; there is no codec.
type Tx struct {
	// Unsigned is the tx body (a zap-backed UnsignedTx).
	Unsigned UnsignedTx `json:"unsignedTx"`

	// Creds are the credentials authorizing the tx.
	Creds []verify.Verifiable `json:"credentials"`

	TxID  ids.ID `json:"id"`
	bytes []byte
}

// NewSigned builds a signed tx from an unsigned tx (already a zap buffer) and
// signs it.
func NewSigned(unsigned UnsignedTx, signers [][]*secp256k1.PrivateKey) (*Tx, error) {
	tx := &Tx{Unsigned: unsigned}
	return tx, tx.Sign(signers)
}

// Parse wraps signed bytes zero-copy: the leading self-delimiting zap buffer
// is the unsigned body; any remainder is the credential buffer. TxID =
// hash(signedBytes), no re-encoding.
func Parse(signedBytes []byte) (*Tx, error) {
	n, err := zapLen(signedBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse tx: %w", err)
	}
	unsigned, err := parseUnsigned(signedBytes[:n])
	if err != nil {
		return nil, fmt.Errorf("couldn't parse unsigned tx: %w", err)
	}
	tx := &Tx{
		Unsigned: unsigned,
		bytes:    signedBytes,
		TxID:     hash.ComputeHash256Array(signedBytes),
	}
	if len(signedBytes) > n {
		creds, err := parseCredsBuf(signedBytes[n:])
		if err != nil {
			return nil, fmt.Errorf("couldn't parse credentials: %w", err)
		}
		tx.Creds = creds
	}
	return tx, nil
}

// Initialize binds the tx's cached bytes and TxID from its Unsigned buffer and
// Creds. Used for txs built fresh in-process (wallet, block builder) whose
// Unsigned buffer is already set. For txs from disk/wire use Parse.
func (tx *Tx) Initialize() error {
	unsignedBytes := tx.Unsigned.Bytes()
	signedBytes := unsignedBytes
	if len(tx.Creds) > 0 {
		credsBuf, err := writeCredsBuf(tx.Creds)
		if err != nil {
			return fmt.Errorf("couldn't encode credentials: %w", err)
		}
		signedBytes = concat(unsignedBytes, credsBuf)
	}
	tx.SetBytes(signedBytes)
	return nil
}

// SetBytes binds the exact signed bytes and derives TxID = hash(signedBytes).
func (tx *Tx) SetBytes(signedBytes []byte) {
	tx.bytes = signedBytes
	tx.TxID = hash.ComputeHash256Array(signedBytes)
}

// Sign attaches credentials for the provided signer sets over the unsigned
// bytes, then binds signed bytes = unsigned ‖ creds.
func (tx *Tx) Sign(signers [][]*secp256k1.PrivateKey) error {
	unsignedBytes := tx.Unsigned.Bytes()
	h := hash.ComputeHash256(unsignedBytes)

	tx.Creds = nil
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(h)
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.Creds = append(tx.Creds, cred)
	}

	signedBytes := unsignedBytes
	if len(tx.Creds) > 0 {
		credsBuf, err := writeCredsBuf(tx.Creds)
		if err != nil {
			return fmt.Errorf("couldn't encode credentials: %w", err)
		}
		signedBytes = concat(unsignedBytes, credsBuf)
	}
	tx.SetBytes(signedBytes)
	return nil
}

func (tx *Tx) Bytes() []byte    { return tx.bytes }
func (tx *Tx) Size() int        { return len(tx.bytes) }
func (tx *Tx) ID() ids.ID       { return tx.TxID }
func (tx *Tx) GossipID() ids.ID { return tx.TxID }

// UTXOs returns the UTXOs this transaction produces.
func (tx *Tx) UTXOs() []*lux.UTXO {
	outs := tx.Unsigned.Outputs()
	utxos := make([]*lux.UTXO, len(outs))
	for i, out := range outs {
		utxos[i] = &lux.UTXO{
			UTXOID: lux.UTXOID{TxID: tx.TxID, OutputIndex: uint32(i)},
			Asset:  lux.Asset{ID: out.AssetID()},
			Out:    out.Out,
		}
	}
	return utxos
}

// InputIDs returns the set of inputs this transaction consumes.
func (tx *Tx) InputIDs() set.Set[ids.ID] { return tx.Unsigned.InputIDs() }

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

// concat returns a ‖ b as a fresh slice.
func concat(a, b []byte) []byte {
	out := make([]byte, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
