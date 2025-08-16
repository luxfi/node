// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/hashing"
)

type Tx struct {
	Unsigned  `serialize:"true" json:"unsigned"`
	Signature [secp256k1.SignatureLen]byte `serialize:"true" json:"signature"`
}

func Parse(bytes []byte) (*Tx, error) {
	tx := &Tx{}
	_, err := Codec.Unmarshal(bytes, tx)
	return tx, err
}

func Sign(utx Unsigned, key *secp256k1.PrivateKey) (*Tx, error) {
	unsignedBytes, err := Codec.Marshal(CodecVersion, &utx)
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
	bytes, err := Codec.Marshal(CodecVersion, tx)
	return hashing.ComputeHash256Array(bytes), err
}

func (tx *Tx) SenderID() (ids.ShortID, error) {
	unsignedBytes, err := Codec.Marshal(CodecVersion, &tx.Unsigned)
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
