// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// Tx object layout: unsigned bytes ptr @0, signature 65B @8. size 73.
const (
	offTxUnsigned  = 0
	offTxSignature = 8
	sizeTx         = 73
)

type Tx struct {
	Unsigned  `json:"unsigned"`
	Signature [secp256k1.SignatureLen]byte `json:"signature"`
}

// Marshal encodes the signed tx: the unsigned tx bytes (self-describing via its
// own kind byte) plus the fixed signature.
func (tx *Tx) Marshal() ([]byte, error) {
	unsignedBytes, err := tx.Unsigned.Marshal()
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + sizeTx + len(unsignedBytes))
	ob := b.StartObject(sizeTx)
	ob.SetBytes(offTxUnsigned, unsignedBytes)
	ob.SetBytesFixed(offTxSignature, tx.Signature[:])
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func Parse(bytes []byte) (*Tx, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	unsigned, err := parseUnsigned(obj.Bytes(offTxUnsigned))
	if err != nil {
		return nil, err
	}
	tx := &Tx{Unsigned: unsigned}
	copy(tx.Signature[:], obj.BytesFixedSlice(offTxSignature, sigLen))
	return tx, nil
}

func Sign(utx Unsigned, key *secp256k1.PrivateKey) (*Tx, error) {
	unsignedBytes, err := utx.Marshal()
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}

	tx := &Tx{
		Unsigned: utx,
	}
	copy(tx.Signature[:], sig[:])
	return tx, nil
}

func (tx *Tx) ID() (ids.ID, error) {
	bytes, err := tx.Marshal()
	return hash.ComputeHash256Array(bytes), err
}

func (tx *Tx) SenderID() (ids.ShortID, error) {
	unsignedBytes, err := tx.Unsigned.Marshal()
	if err != nil {
		return ids.ShortEmpty, err
	}

	pk, err := secp256k1.RecoverPublicKey(unsignedBytes, tx.Signature[:])
	if err != nil {
		return ids.ShortEmpty, err
	}
	addr := pk.Address()
	return ids.ShortID(addr), nil
}
