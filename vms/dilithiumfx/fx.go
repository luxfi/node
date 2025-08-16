// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// Post-quantum cryptography support - Dilithium signatures for X-Chain

package dilithiumfx

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/hashing"
	"github.com/luxfi/node/utils/timer/mockable"
)

const (
	defaultCacheSize = 256
	
	// Dilithium3 parameters (NIST Level 3 security)
	Dilithium3PublicKeyLen = 1952
	Dilithium3SecretKeyLen = 4016
	Dilithium3SignatureLen = 3293
)

var (
	_ ids.ID // explicitly use ids to avoid unused import warning
)

var (
	ErrWrongVMType                      = errors.New("wrong vm type")
	ErrWrongTxType                      = errors.New("wrong tx type")
	ErrWrongOpType                      = errors.New("wrong operation type")
	ErrWrongUTXOType                    = errors.New("wrong utxo type")
	ErrWrongInputType                   = errors.New("wrong input type")
	ErrWrongCredentialType              = errors.New("wrong credential type")
	ErrWrongOwnerType                   = errors.New("wrong owner type")
	ErrMismatchedAmounts                = errors.New("utxo amount and input amount are not equal")
	ErrWrongNumberOfUTXOs               = errors.New("wrong number of utxos for the operation")
	ErrTimelocked                       = errors.New("output is time locked")
	ErrTooManySigners                   = errors.New("input has more signers than expected")
	ErrTooFewSigners                    = errors.New("input has less signers than expected")
	ErrInputOutputIndexOutOfBounds      = errors.New("input referenced a nonexistent address in the output")
	ErrInputCredentialSignersMismatch   = errors.New("input expected a different number of signers than provided in the credential")
	ErrWrongSig                         = errors.New("wrong signature")
	ErrInvalidDilithiumSignature        = errors.New("invalid Dilithium signature")
	ErrInvalidDilithiumPublicKey        = errors.New("invalid Dilithium public key")
)

// VM defines the interface for Dilithium fx VM
type VM interface {
	CodecRegistry() codec.Registry
	Clock() *mockable.Clock
	Logger() log.Logger
}

// DilithiumFx describes the Dilithium post-quantum signature feature extension
// This provides quantum-resistant signatures for X-Chain UTXOs using lattice-based cryptography
type DilithiumFx struct {
	VerifyCache  *VerifyCache
	VM           VM
	bootstrapped bool
}

// VerifyCache caches Dilithium signature verifications
type VerifyCache struct {
	cache map[string]bool
	size  int
}

func NewVerifyCache(size int) *VerifyCache {
	return &VerifyCache{
		cache: make(map[string]bool, size),
		size:  size,
	}
}

func (fx *DilithiumFx) Initialize(vmIntf interface{}) error {
	if err := fx.InitializeVM(vmIntf); err != nil {
		return err
	}

	log := fx.VM.Logger()
	log.Debug("initializing Dilithium fx")

	cache := NewVerifyCache(defaultCacheSize)
	fx.VerifyCache = cache
	
	c := fx.VM.CodecRegistry()
	return errors.Join(
		c.RegisterType(&DilithiumTransferInput{}),
		c.RegisterType(&DilithiumMintOutput{}),
		c.RegisterType(&DilithiumTransferOutput{}),
		c.RegisterType(&DilithiumMintOperation{}),
		c.RegisterType(&DilithiumCredential{}),
	)
}

func (fx *DilithiumFx) InitializeVM(vmIntf interface{}) error {
	vm, ok := vmIntf.(VM)
	if !ok {
		return ErrWrongVMType
	}
	fx.VM = vm
	return nil
}

func (*DilithiumFx) Bootstrapping() error {
	return nil
}

func (fx *DilithiumFx) Bootstrapped() error {
	fx.bootstrapped = true
	return nil
}

// VerifyPermission verifies that a Dilithium signature proves ownership
func (fx *DilithiumFx) VerifyPermission(txIntf, inIntf, credIntf, ownerIntf interface{}) error {
	tx, ok := txIntf.(UnsignedTx)
	if !ok {
		return ErrWrongTxType
	}
	
	in, ok := inIntf.(*DilithiumInput)
	if !ok {
		return ErrWrongInputType
	}
	
	cred, ok := credIntf.(*DilithiumCredential)
	if !ok {
		return ErrWrongCredentialType
	}
	
	owner, ok := ownerIntf.(*DilithiumOutputOwners)
	if !ok {
		return ErrWrongOwnerType
	}

	if err := fx.verifyDilithiumSignature(tx, in, cred, owner); err != nil {
		return err
	}

	return fx.verifyMultisigDilithium(tx, owner, cred)
}

func (fx *DilithiumFx) verifyDilithiumSignature(tx UnsignedTx, in *DilithiumInput, cred *DilithiumCredential, owner *DilithiumOutputOwners) error {
	// Get the message to be signed (transaction hash)
	txBytes, err := fx.VM.CodecRegistry().Marshal(codecVersion, &tx)
	if err != nil {
		return err
	}
	txHash := hashing.ComputeHash256(txBytes)

	// Check cache first
	cacheKey := string(txHash) + string(cred.Sig)
	if cached, ok := fx.VerifyCache.cache[cacheKey]; ok {
		if cached {
			return nil
		}
		return ErrInvalidDilithiumSignature
	}

	// Verify Dilithium signature
	valid := verifyDilithium3(txHash, cred.Sig, owner.DilithiumPublicKey)
	
	// Cache result
	fx.VerifyCache.cache[cacheKey] = valid
	
	if !valid {
		return ErrInvalidDilithiumSignature
	}
	
	return nil
}

func (fx *DilithiumFx) verifyMultisigDilithium(tx UnsignedTx, owner *DilithiumOutputOwners, cred *DilithiumCredential) error {
	// Check threshold requirements
	if len(cred.Sigs) < int(owner.Threshold) {
		return ErrTooFewSigners
	}
	
	// Verify each signature
	txBytes, err := fx.VM.CodecRegistry().Marshal(codecVersion, &tx)
	if err != nil {
		return err
	}
	txHash := hashing.ComputeHash256(txBytes)
	
	validSigs := 0
	for i, sig := range cred.Sigs {
		if i >= len(owner.DilithiumPublicKeys) {
			break
		}
		
		if verifyDilithium3(txHash, sig, owner.DilithiumPublicKeys[i]) {
			validSigs++
			if validSigs >= int(owner.Threshold) {
				return nil
			}
		}
	}
	
	if validSigs < int(owner.Threshold) {
		return ErrTooFewSigners
	}
	
	return nil
}

// Stub function - would be implemented by integrating actual Dilithium library
func verifyDilithium3(hash, sig, pubKey []byte) bool {
	// TODO: Integrate with Go Dilithium library
	// This would call the actual Dilithium3 verification
	return false
}

// UnsignedTx represents an unsigned transaction
type UnsignedTx interface {
	verify.Verifiable
	Bytes() []byte
}

// DilithiumInput represents an input that references a Dilithium-protected UTXO
type DilithiumInput struct {
	// SigIndices specifies which signatures to use from the credential
	SigIndices []uint32 `serialize:"true" json:"signatureIndices"`
}

// DilithiumCredential represents a credential with Dilithium signatures
type DilithiumCredential struct {
	// Single signature mode
	Sig []byte `serialize:"true" json:"signature"`
	
	// Multisig mode
	Sigs [][]byte `serialize:"true" json:"signatures"`
}

// DilithiumOutputOwners specifies who can spend an output using Dilithium signatures
type DilithiumOutputOwners struct {
	Locktime  uint64   `serialize:"true" json:"locktime"`
	Threshold uint32   `serialize:"true" json:"threshold"`
	
	// Single public key for simple ownership
	DilithiumPublicKey []byte `serialize:"true" json:"dilithiumPublicKey"`
	
	// Multiple public keys for threshold multisig
	DilithiumPublicKeys [][]byte `serialize:"true" json:"dilithiumPublicKeys"`
}

// DilithiumTransferInput represents an input that spends a Dilithium-protected UTXO
type DilithiumTransferInput struct {
	Amt   uint64         `serialize:"true" json:"amount"`
	Input DilithiumInput `serialize:"true"`
}

// DilithiumTransferOutput represents an output protected by Dilithium signatures
type DilithiumTransferOutput struct {
	Amt    uint64                `serialize:"true" json:"amount"`
	Owners DilithiumOutputOwners `serialize:"true"`
}

// DilithiumMintOutput represents a mintable output protected by Dilithium
type DilithiumMintOutput struct {
	Owners DilithiumOutputOwners `serialize:"true"`
}

// DilithiumMintOperation represents a mint operation with Dilithium protection
type DilithiumMintOperation struct {
	MintInput      DilithiumInput          `serialize:"true"`
	MintOutput     DilithiumMintOutput     `serialize:"true"`
	TransferOutput DilithiumTransferOutput `serialize:"true"`
}

const codecVersion = 0