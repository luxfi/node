// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/geth/common"
	// "github.com/luxfi/evm/plugin/evm"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/crypto/keychain"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/hashing"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

const version = 0

var (
	_ Signer = (*txSigner)(nil)

	errUnknownInputType      = errors.New("unknown input type")
	errUnknownCredentialType = errors.New("unknown credential type")
	errUnknownOutputType     = errors.New("unknown output type")
	errInvalidUTXOSigIndex   = errors.New("invalid UTXO signature index")

	emptySig [secp256k1.SignatureLen]byte
)

type Signer interface {
	// SignAtomic adds as many missing signatures as possible to the provided
	// transaction.
	//
	// If there are already some signatures on the transaction, those signatures
	// will not be removed.
	//
	// If the signer doesn't have the ability to provide a required signature,
	// the signature slot will be skipped without reporting an error.
	SignAtomic(ctx context.Context, tx *Tx) error
}

type EthKeychain interface {
	// The returned Signer can provide a signature for [addr]
	GetEth(addr common.Address) (keychain.Signer, bool)
	// Returns the set of addresses for which the accessor keeps an associated
	// signer
	EthAddresses() set.Set[common.Address]
}

type SignerBackend interface {
	GetUTXO(ctx context.Context, chainID, utxoID ids.ID) (*lux.UTXO, error)
}

type txSigner struct {
	luxKC   keychain.Keychain
	ethKC   EthKeychain
	backend SignerBackend
}

func NewSigner(luxKC keychain.Keychain, ethKC EthKeychain, backend SignerBackend) Signer {
	return &txSigner{
		luxKC:   luxKC,
		ethKC:   ethKC,
		backend: backend,
	}
}

func (s *txSigner) SignAtomic(ctx context.Context, tx *Tx) error {
	switch utx := tx.UnsignedAtomicTx.(type) {
	case *UnsignedImportTx:
		signers, err := s.getImportSigners(ctx, utx.SourceChain, utx.ImportedInputs)
		if err != nil {
			return err
		}
		return sign(tx, true, signers)
	case *UnsignedExportTx:
		signers := s.getExportSigners(utx.Ins)
		return sign(tx, true, signers)
	default:
		return fmt.Errorf("%w: %T", errUnknownTxType, tx)
	}
}

func (s *txSigner) getImportSigners(ctx context.Context, sourceChainID ids.ID, ins []*lux.TransferableInput) ([][]keychain.Signer, error) {
	txSigners := make([][]keychain.Signer, len(ins))
	for credIndex, transferInput := range ins {
		input, ok := transferInput.In.(*secp256k1fx.TransferInput)
		if !ok {
			return nil, errUnknownInputType
		}

		inputSigners := make([]keychain.Signer, len(input.SigIndices))
		txSigners[credIndex] = inputSigners

		utxoID := transferInput.InputID()
		utxo, err := s.backend.GetUTXO(ctx, sourceChainID, utxoID)
		if err == database.ErrNotFound {
			// If we don't have access to the UTXO, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		if err != nil {
			return nil, err
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, errUnknownOutputType
		}

		for sigIndex, addrIndex := range input.SigIndices {
			if addrIndex >= uint32(len(out.Addrs)) {
				return nil, errInvalidUTXOSigIndex
			}

			addr := out.Addrs[addrIndex]
			key, ok := s.luxKC.Get(addr)
			if !ok {
				// If we don't have access to the key, then we can't sign this
				// transaction. However, we can attempt to partially sign it.
				continue
			}
			inputSigners[sigIndex] = key
		}
	}
	return txSigners, nil
}

func (s *txSigner) getExportSigners(ins []*EVMInput) [][]keychain.Signer {
	txSigners := make([][]keychain.Signer, len(ins))
	for credIndex, input := range ins {
		inputSigners := make([]keychain.Signer, 1)
		txSigners[credIndex] = inputSigners

		key, ok := s.ethKC.GetEth(input.Address)
		if !ok {
			// If we don't have access to the key, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		inputSigners[0] = key
	}
	return txSigners
}

func SignUnsignedAtomic(ctx context.Context, signer Signer, utx UnsignedAtomicTx) (*Tx, error) {
	tx := &Tx{UnsignedAtomicTx: utx}
	return tx, signer.SignAtomic(ctx, tx)
}

// TODO: remove [signHash] after the ledger supports signing all transactions.
func sign(tx *Tx, signHash bool, txSigners [][]keychain.Signer) error {
	unsignedBytes, err := Codec.Marshal(version, &tx.UnsignedAtomicTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal unsigned tx: %w", err)
	}
	unsignedHash := hashing.ComputeHash256(unsignedBytes)

	if expectedLen := len(txSigners); expectedLen != len(tx.Creds) {
		tx.Creds = make([]verify.Verifiable, expectedLen)
	}

	sigCache := make(map[ids.ShortID][secp256k1.SignatureLen]byte)
	for credIndex, inputSigners := range txSigners {
		credIntf := tx.Creds[credIndex]
		if credIntf == nil {
			credIntf = &secp256k1fx.Credential{}
			tx.Creds[credIndex] = credIntf
		}

		cred, ok := credIntf.(*secp256k1fx.Credential)
		if !ok {
			return errUnknownCredentialType
		}
		if expectedLen := len(inputSigners); expectedLen != len(cred.Sigs) {
			cred.Sigs = make([][secp256k1.SignatureLen]byte, expectedLen)
		}

		for sigIndex, signer := range inputSigners {
			if signer == nil {
				// If we don't have access to the key, then we can't sign this
				// transaction. However, we can attempt to partially sign it.
				continue
			}
			addr := signer.Address()
			if sig := cred.Sigs[sigIndex]; sig != emptySig {
				// If this signature has already been populated, we can just
				// copy the needed signature for the future.
				sigCache[addr] = sig
				continue
			}

			if sig, exists := sigCache[addr]; exists {
				// If this key has already produced a signature, we can just
				// copy the previous signature.
				cred.Sigs[sigIndex] = sig
				continue
			}

			var sig []byte
			if signHash {
				sig, err = signer.SignHash(unsignedHash)
			} else {
				sig, err = signer.Sign(unsignedBytes)
			}
			if err != nil {
				return fmt.Errorf("problem signing tx: %w", err)
			}
			copy(cred.Sigs[sigIndex][:], sig)
			sigCache[addr] = cred.Sigs[sigIndex]
		}
	}

	signedBytes, err := Codec.Marshal(version, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal tx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}
