// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/p2p/gossip"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/wire"
)

var _ gossip.Gossipable = (*Tx)(nil)

type UnsignedTx interface {
	SetBytes(unsignedBytes []byte)
	Bytes() []byte

	InputIDs() set.Set[ids.ID]

	NumCredentials() int
	InputUTXOs() []*lux.UTXOID

	// Visit calls [visitor] with this transaction's concrete type
	Visit(visitor Visitor) error
}

// Tx is the core operation that can be performed. The tx uses the UTXO model.
// Specifically, a txs inputs will consume previous txs outputs. A tx will be
// valid if the inputs have the authority to consume the outputs they are
// attempting to consume and the inputs consume sufficient state to produce the
// outputs.
//
// A signed tx is a wire.SignedTx envelope: the unsigned tx bytes (which carry
// the xkind discriminator at offset 0) followed by a packed list of fx
// credential envelopes. There is no codec.
type Tx struct {
	Unsigned UnsignedTx          `json:"unsignedTx"`
	Creds    []*fxs.FxCredential `json:"credentials"` // The credentials of this transaction

	TxID  ids.ID `json:"id"`
	bytes []byte
}

// UnsignedBytes returns the canonical native-ZAP wire bytes of an unsigned tx,
// serializing (and caching) them on first use. This is the signing target —
// every fx signature is computed over hash(unsignedBytes).
func UnsignedBytes(u UnsignedTx) ([]byte, error) {
	if b := u.Bytes(); len(b) > 0 {
		return b, nil
	}
	b, err := serializeUnsigned(u)
	if err != nil {
		return nil, err
	}
	u.SetBytes(b)
	return b, nil
}

// serializeUnsigned encodes an unsigned tx to its wire bytes, dispatching on
// the concrete type. This is the write-side inverse of parseUnsigned.
func serializeUnsigned(u UnsignedTx) ([]byte, error) {
	switch t := u.(type) {
	case *BaseTx:
		return t.serialize()
	case *CreateAssetTx:
		return t.serialize()
	case *OperationTx:
		return t.serialize()
	case *ImportTx:
		return t.serialize()
	case *ExportTx:
		return t.serialize()
	default:
		return nil, fmt.Errorf("xvm txs: cannot serialize unknown unsigned tx %T", u)
	}
}

// signedBytesFrom wraps unsigned bytes plus fx credential envelopes into a
// wire.SignedTx buffer.
func signedBytesFrom(unsignedBytes []byte, creds []*fxs.FxCredential) ([]byte, error) {
	credEnvelopes := make([][]byte, len(creds))
	for i, c := range creds {
		b, err := childBytes(c.Credential)
		if err != nil {
			return nil, fmt.Errorf("credential %d: %w", i, err)
		}
		credEnvelopes[i] = b
	}
	return wire.NewSignedTx(wire.SignedTxInput{
		UnsignedBytes: unsignedBytes,
		Credentials:   credEnvelopes,
	}), nil
}

// Initialize binds the tx's cached bytes and TxID from its Unsigned tx and
// Creds. Used for txs built fresh in-process (wallet, block builder).
func (t *Tx) Initialize() error {
	unsignedBytes, err := UnsignedBytes(t.Unsigned)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	signedBytes, err := signedBytesFrom(unsignedBytes, t.Creds)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	t.SetBytes(unsignedBytes, signedBytes)
	return nil
}

func (t *Tx) SetBytes(unsignedBytes, signedBytes []byte) {
	t.TxID = hash.ComputeHash256Array(signedBytes)
	t.bytes = signedBytes
	t.Unsigned.SetBytes(unsignedBytes)
}

// ID returns the unique ID of this tx
func (t *Tx) ID() ids.ID {
	return t.TxID
}

// GossipID returns the unique ID that this tx should use for mempool gossip
func (t *Tx) GossipID() ids.ID {
	return t.TxID
}

// Bytes returns the binary representation of this tx
func (t *Tx) Bytes() []byte {
	return t.bytes
}

func (t *Tx) Size() int {
	return len(t.bytes)
}

// UTXOs returns the UTXOs transaction is producing.
func (t *Tx) UTXOs() []*lux.UTXO {
	u := utxoGetter{tx: t}
	// The visit error is explicitly dropped here because no error is ever
	// returned from the utxoGetter.
	_ = t.Unsigned.Visit(&u)
	return u.utxos
}

func (t *Tx) InputIDs() set.Set[ids.ID] {
	return t.Unsigned.InputIDs()
}

// sign attaches credentials for the provided signer sets over the unsigned
// bytes, then binds signed bytes = unsigned ‖ creds.
func (t *Tx) sign(signers [][]*secp256k1.PrivateKey, wrap func([][secp256k1.SignatureLen]byte) verify.Verifiable) error {
	unsignedBytes, err := UnsignedBytes(t.Unsigned)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	h := hash.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		sigs := make([][secp256k1.SignatureLen]byte, len(keys))
		for i, key := range keys {
			sig, err := key.SignHash(h)
			if err != nil {
				return fmt.Errorf("problem creating transaction: %w", err)
			}
			copy(sigs[i][:], sig)
		}
		t.Creds = append(t.Creds, &fxs.FxCredential{Credential: wrap(sigs)})
	}
	signedBytes, err := signedBytesFrom(unsignedBytes, t.Creds)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	t.SetBytes(unsignedBytes, signedBytes)
	return nil
}

func (t *Tx) SignSECP256K1Fx(signers [][]*secp256k1.PrivateKey) error {
	return t.sign(signers, func(sigs [][secp256k1.SignatureLen]byte) verify.Verifiable {
		return &secp256k1fx.Credential{Sigs: sigs}
	})
}

func (t *Tx) SignPropertyFx(signers [][]*secp256k1.PrivateKey) error {
	return t.sign(signers, func(sigs [][secp256k1.SignatureLen]byte) verify.Verifiable {
		return &propertyfx.Credential{Credential: secp256k1fx.Credential{Sigs: sigs}}
	})
}

func (t *Tx) SignNFTFx(signers [][]*secp256k1.PrivateKey) error {
	return t.sign(signers, func(sigs [][secp256k1.SignatureLen]byte) verify.Verifiable {
		return &nftfx.Credential{Credential: secp256k1fx.Credential{Sigs: sigs}}
	})
}
