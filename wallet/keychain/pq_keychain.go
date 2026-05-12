// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keychain

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/crypto/mlkem"
	"github.com/luxfi/crypto/ring"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/crypto/slhdsa"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

var (
	ErrInvalidKeyType = errors.New("invalid key type")
	ErrKeyNotFound    = errors.New("key not found")
)

// KeyType represents the type of cryptographic key
type KeyType uint8

const (
	// Classical cryptography
	KeyTypeSecp256k1 KeyType = iota
	KeyTypeBLS               // BLS signatures for consensus

	// Post-quantum cryptography (NIST FIPS standards)
	KeyTypeMLDSA44   // FIPS 204 - ML-DSA-44
	KeyTypeMLDSA65   // FIPS 204 - ML-DSA-65
	KeyTypeMLDSA87   // FIPS 204 - ML-DSA-87
	KeyTypeSLHDSA128 // FIPS 205 - SLH-DSA-128
	KeyTypeSLHDSA192 // FIPS 205 - SLH-DSA-192
	KeyTypeSLHDSA256 // FIPS 205 - SLH-DSA-256

	// Key encapsulation (FIPS 203)
	KeyTypeMLKEM512  // ML-KEM-512
	KeyTypeMLKEM768  // ML-KEM-768
	KeyTypeMLKEM1024 // ML-KEM-1024

	// Privacy-preserving
	KeyTypeRingtail // Ring signatures

	// Hybrid modes (classical + post-quantum)
	KeyTypeHybridSecp256k1MLDSA44
	KeyTypeHybridSecp256k1SLHDSA128
	KeyTypeHybridBLSMLDSA44
)

// PQSigner implements Signer with post-quantum support
type PQSigner struct {
	keyType KeyType
	address ids.ShortID

	// Classical keys
	secp256k1Key *secp256k1.PrivateKey

	// BLS keys (for consensus)
	blsKey *bls.SecretKey

	// Post-quantum signature keys
	mldsaKey  interface{} // Can be *mldsa.PrivateKey44/65/87
	slhdsaKey interface{} // Can be *slhdsa.PrivateKey128/192/256

	// Post-quantum key encapsulation (ML-KEM)
	mlkemKey     *mlkem.PrivateKey
	mlkemPubKey  *mlkem.PublicKey
	mlkemMode    mlkem.Mode

	// Ring signatures (privacy-preserving)
	ringSigner ring.Signer
	ringScheme ring.Scheme

	// For hybrid modes, we store both
	hybridClassical *secp256k1.PrivateKey
	hybridPQ        interface{}
	hybridBLS       *bls.SecretKey
}

// SignHash signs a hash with the appropriate algorithm
func (s *PQSigner) SignHash(hash []byte) ([]byte, error) {
	switch s.keyType {
	case KeyTypeSecp256k1:
		if s.secp256k1Key == nil {
			return nil, ErrInvalidKeyType
		}
		return s.secp256k1Key.SignHash(hash)

	case KeyTypeBLS:
		if s.blsKey == nil {
			return nil, ErrInvalidKeyType
		}
		sig, err := s.blsKey.Sign(hash)
		if err != nil {
			return nil, err
		}
		return bls.SignatureToBytes(sig), nil

	case KeyTypeMLDSA44, KeyTypeMLDSA65, KeyTypeMLDSA87:
		if key, ok := s.mldsaKey.(*mldsa.PrivateKey); ok {
			sig, err := key.Sign(rand.Reader, hash, crypto.Hash(0))
			if err != nil {
				return nil, err
			}
			return sig, nil
		}
		return nil, ErrInvalidKeyType

	case KeyTypeSLHDSA128, KeyTypeSLHDSA192, KeyTypeSLHDSA256:
		if key, ok := s.slhdsaKey.(*slhdsa.PrivateKey); ok {
			sig, err := key.Sign(rand.Reader, hash, crypto.Hash(0))
			if err != nil {
				return nil, err
			}
			return sig, nil
		}
		return nil, ErrInvalidKeyType

	case KeyTypeRingtail:
		// Ring signatures require a ring of public keys
		// For SignHash without a ring, we return an error
		// Use SignRing for ring signature operations
		return nil, errors.New("corona requires ring members - use SignRing method")

	case KeyTypeHybridSecp256k1MLDSA44:
		// Hybrid mode: concatenate both signatures
		if s.hybridClassical == nil || s.hybridPQ == nil {
			return nil, ErrInvalidKeyType
		}

		classicalSig, err := s.hybridClassical.SignHash(hash)
		if err != nil {
			return nil, err
		}

		if key, ok := s.hybridPQ.(*mldsa.PrivateKey); ok {
			pqSig, err := key.Sign(rand.Reader, hash, crypto.Hash(0))
			if err != nil {
				return nil, err
			}
			// Concatenate signatures with length prefixes
			result := make([]byte, 0, 2+len(classicalSig)+2+len(pqSig))
			result = append(result, byte(len(classicalSig)>>8), byte(len(classicalSig)))
			result = append(result, classicalSig...)
			result = append(result, byte(len(pqSig)>>8), byte(len(pqSig)))
			result = append(result, pqSig...)
			return result, nil
		}
		return nil, ErrInvalidKeyType

	case KeyTypeHybridBLSMLDSA44:
		// Hybrid BLS + ML-DSA mode
		if s.hybridBLS == nil || s.hybridPQ == nil {
			return nil, ErrInvalidKeyType
		}

		blsSig, err := s.hybridBLS.Sign(hash)
		if err != nil {
			return nil, err
		}
		blsSigBytes := bls.SignatureToBytes(blsSig)

		if key, ok := s.hybridPQ.(*mldsa.PrivateKey); ok {
			pqSig, err := key.Sign(rand.Reader, hash, crypto.Hash(0))
			if err != nil {
				return nil, err
			}
			// Concatenate signatures with length prefixes
			result := make([]byte, 0, 2+len(blsSigBytes)+2+len(pqSig))
			result = append(result, byte(len(blsSigBytes)>>8), byte(len(blsSigBytes)))
			result = append(result, blsSigBytes...)
			result = append(result, byte(len(pqSig)>>8), byte(len(pqSig)))
			result = append(result, pqSig...)
			return result, nil
		}
		return nil, ErrInvalidKeyType

	default:
		return nil, ErrInvalidKeyType
	}
}

// Sign signs a message with the appropriate algorithm
func (s *PQSigner) Sign(msg []byte) ([]byte, error) {
	switch s.keyType {
	case KeyTypeSecp256k1:
		// secp256k1 needs a 32-byte hash
		if s.secp256k1Key == nil {
			return nil, ErrInvalidKeyType
		}
		hash := sha256.New()
		hash.Write(msg)
		return s.secp256k1Key.SignHash(hash.Sum(nil))

	case KeyTypeBLS:
		// BLS signs message directly
		if s.blsKey == nil {
			return nil, ErrInvalidKeyType
		}
		sig, err := s.blsKey.Sign(msg)
		if err != nil {
			return nil, err
		}
		return bls.SignatureToBytes(sig), nil

	case KeyTypeRingtail:
		// Ring signatures require a ring of public keys
		return nil, errors.New("corona requires ring members - use SignRing method")

	case KeyTypeHybridSecp256k1MLDSA44, KeyTypeHybridSecp256k1SLHDSA128:
		// For hybrid, we need to hash for the classical part
		hash := sha256.New()
		hash.Write(msg)
		hashBytes := hash.Sum(nil)

		// Sign with both algorithms
		if s.hybridClassical == nil || s.hybridPQ == nil {
			return nil, ErrInvalidKeyType
		}

		classicalSig, err := s.hybridClassical.SignHash(hashBytes)
		if err != nil {
			return nil, err
		}

		var pqSig []byte
		switch pq := s.hybridPQ.(type) {
		case *mldsa.PrivateKey:
			pqSig, err = pq.Sign(rand.Reader, msg, crypto.Hash(0))
		case *slhdsa.PrivateKey:
			pqSig, err = pq.Sign(rand.Reader, msg, crypto.Hash(0))
		default:
			return nil, ErrInvalidKeyType
		}
		if err != nil {
			return nil, err
		}

		// Concatenate signatures with length prefixes
		result := make([]byte, 0, 2+len(classicalSig)+2+len(pqSig))
		result = append(result, byte(len(classicalSig)>>8), byte(len(classicalSig)))
		result = append(result, classicalSig...)
		result = append(result, byte(len(pqSig)>>8), byte(len(pqSig)))
		result = append(result, pqSig...)
		return result, nil

	case KeyTypeHybridBLSMLDSA44:
		// Hybrid BLS + ML-DSA mode
		if s.hybridBLS == nil || s.hybridPQ == nil {
			return nil, ErrInvalidKeyType
		}

		blsSig, err := s.hybridBLS.Sign(msg)
		if err != nil {
			return nil, err
		}
		blsSigBytes := bls.SignatureToBytes(blsSig)

		if key, ok := s.hybridPQ.(*mldsa.PrivateKey); ok {
			pqSig, err := key.Sign(rand.Reader, msg, crypto.Hash(0))
			if err != nil {
				return nil, err
			}
			// Concatenate signatures with length prefixes
			result := make([]byte, 0, 2+len(blsSigBytes)+2+len(pqSig))
			result = append(result, byte(len(blsSigBytes)>>8), byte(len(blsSigBytes)))
			result = append(result, blsSigBytes...)
			result = append(result, byte(len(pqSig)>>8), byte(len(pqSig)))
			result = append(result, pqSig...)
			return result, nil
		}
		return nil, ErrInvalidKeyType

	default:
		// PQ algorithms sign the message directly
		return s.SignHash(msg)
	}
}

// Address returns the address associated with this signer
func (s *PQSigner) Address() ids.ShortID {
	return s.address
}

// SignRing creates a ring signature for the given message using the provided ring of public keys.
// The signer's public key must be included in the ring at signerIndex.
func (s *PQSigner) SignRing(message []byte, ringPubKeys [][]byte, signerIndex int) (ring.RingSignature, error) {
	if s.keyType != KeyTypeRingtail {
		return nil, errors.New("SignRing only supported for Corona key type")
	}
	if s.ringSigner == nil {
		return nil, ErrInvalidKeyType
	}

	return s.ringSigner.Sign(message, ringPubKeys, signerIndex)
}

// KeyImage returns the key image for linkability (ring signatures only).
// Returns nil for non-ring signature key types.
func (s *PQSigner) KeyImage() []byte {
	if s.ringSigner != nil {
		return s.ringSigner.KeyImage()
	}
	return nil
}

// RingScheme returns the ring signature scheme used (for Corona keys).
func (s *PQSigner) RingScheme() ring.Scheme {
	return s.ringScheme
}

// PublicKey returns the public key bytes for this signer.
func (s *PQSigner) PublicKey() []byte {
	switch s.keyType {
	case KeyTypeSecp256k1:
		if s.secp256k1Key != nil {
			return s.secp256k1Key.PublicKey().CompressedBytes()
		}
	case KeyTypeBLS:
		if s.blsKey != nil {
			return bls.PublicKeyToCompressedBytes(s.blsKey.PublicKey())
		}
	case KeyTypeMLDSA44, KeyTypeMLDSA65, KeyTypeMLDSA87:
		if key, ok := s.mldsaKey.(*mldsa.PrivateKey); ok {
			return key.PublicKey.Bytes()
		}
	case KeyTypeSLHDSA128, KeyTypeSLHDSA192, KeyTypeSLHDSA256:
		if key, ok := s.slhdsaKey.(*slhdsa.PrivateKey); ok {
			return key.PublicKey.Bytes()
		}
	case KeyTypeMLKEM512, KeyTypeMLKEM768, KeyTypeMLKEM1024:
		if s.mlkemPubKey != nil {
			return s.mlkemPubKey.Bytes()
		}
	case KeyTypeRingtail:
		if s.ringSigner != nil {
			return s.ringSigner.PublicKey()
		}
	}
	return nil
}

// Encapsulate generates a shared secret and ciphertext for the given public key.
// Only valid for ML-KEM key types.
func (s *PQSigner) Encapsulate(recipientPubKey *mlkem.PublicKey) (ciphertext, sharedSecret []byte, err error) {
	if s.keyType != KeyTypeMLKEM512 && s.keyType != KeyTypeMLKEM768 && s.keyType != KeyTypeMLKEM1024 {
		return nil, nil, errors.New("Encapsulate only supported for ML-KEM key types")
	}
	return recipientPubKey.Encapsulate()
}

// Decapsulate recovers the shared secret from a ciphertext.
// Only valid for ML-KEM key types.
func (s *PQSigner) Decapsulate(ciphertext []byte) (sharedSecret []byte, err error) {
	if s.keyType != KeyTypeMLKEM512 && s.keyType != KeyTypeMLKEM768 && s.keyType != KeyTypeMLKEM1024 {
		return nil, errors.New("Decapsulate only supported for ML-KEM key types")
	}
	if s.mlkemKey == nil {
		return nil, ErrInvalidKeyType
	}
	return s.mlkemKey.Decapsulate(ciphertext)
}

// BLSPublicKey returns the BLS public key (for BLS or hybrid BLS key types).
func (s *PQSigner) BLSPublicKey() *bls.PublicKey {
	switch s.keyType {
	case KeyTypeBLS:
		if s.blsKey != nil {
			return s.blsKey.PublicKey()
		}
	case KeyTypeHybridBLSMLDSA44:
		if s.hybridBLS != nil {
			return s.hybridBLS.PublicKey()
		}
	}
	return nil
}

// KeyType returns the key type of this signer.
func (s *PQSigner) KeyType() KeyType {
	return s.keyType
}

// PQKeychain implements Keychain with post-quantum support
type PQKeychain struct {
	keysByAddress map[ids.ShortID]*PQSigner
	addressSet    set.Set[ids.ShortID]
	defaultType   KeyType
}

// NewPQKeychain creates a new post-quantum keychain
func NewPQKeychain(defaultType KeyType) *PQKeychain {
	return &PQKeychain{
		keysByAddress: make(map[ids.ShortID]*PQSigner),
		addressSet:    set.NewSet[ids.ShortID](0),
		defaultType:   defaultType,
	}
}

// AddSecp256k1 adds a secp256k1 key to the keychain
func (kc *PQKeychain) AddSecp256k1(key *secp256k1.PrivateKey) ids.ShortID {
	pk := key.PublicKey()
	addr := pk.Address()
	shortAddr, _ := ids.ToShortID(addr[:])

	signer := &PQSigner{
		keyType:      KeyTypeSecp256k1,
		address:      shortAddr,
		secp256k1Key: key,
	}

	kc.keysByAddress[shortAddr] = signer
	kc.addressSet.Add(shortAddr)
	return shortAddr
}

// AddMLDSA adds an ML-DSA key to the keychain
func (kc *PQKeychain) AddMLDSA(key *mldsa.PrivateKey, keyType KeyType) ids.ShortID {
	// Generate address from public key bytes
	pubKeyBytes := key.Bytes()
	addrBytes := ids.ShortID{}
	copy(addrBytes[:], pubKeyBytes[:20]) // Use first 20 bytes as address

	signer := &PQSigner{
		keyType:  keyType,
		address:  addrBytes,
		mldsaKey: key,
	}

	kc.keysByAddress[addrBytes] = signer
	kc.addressSet.Add(addrBytes)
	return addrBytes
}

// AddSLHDSA adds an SLH-DSA key to the keychain
func (kc *PQKeychain) AddSLHDSA(key *slhdsa.PrivateKey, keyType KeyType) ids.ShortID {
	pubKeyBytes := key.Bytes()
	addrBytes := ids.ShortID{}
	copy(addrBytes[:], pubKeyBytes[:20])

	signer := &PQSigner{
		keyType:   keyType,
		address:   addrBytes,
		slhdsaKey: key,
	}

	kc.keysByAddress[addrBytes] = signer
	kc.addressSet.Add(addrBytes)
	return addrBytes
}

// AddBLS adds a BLS key to the keychain
func (kc *PQKeychain) AddBLS(key *bls.SecretKey) ids.ShortID {
	pubKey := key.PublicKey()
	pubKeyBytes := bls.PublicKeyToCompressedBytes(pubKey)

	// Generate address from public key bytes (first 20 bytes of hash)
	hash := sha256.Sum256(pubKeyBytes)
	addrBytes := ids.ShortID{}
	copy(addrBytes[:], hash[:20])

	signer := &PQSigner{
		keyType: KeyTypeBLS,
		address: addrBytes,
		blsKey:  key,
	}

	kc.keysByAddress[addrBytes] = signer
	kc.addressSet.Add(addrBytes)
	return addrBytes
}

// AddMLKEM adds an ML-KEM key pair to the keychain for key encapsulation
func (kc *PQKeychain) AddMLKEM(pubKey *mlkem.PublicKey, privKey *mlkem.PrivateKey, mode mlkem.Mode) ids.ShortID {
	pubKeyBytes := pubKey.Bytes()

	// Generate address from public key bytes
	hash := sha256.Sum256(pubKeyBytes)
	addrBytes := ids.ShortID{}
	copy(addrBytes[:], hash[:20])

	var keyType KeyType
	switch mode {
	case mlkem.MLKEM512:
		keyType = KeyTypeMLKEM512
	case mlkem.MLKEM768:
		keyType = KeyTypeMLKEM768
	case mlkem.MLKEM1024:
		keyType = KeyTypeMLKEM1024
	default:
		keyType = KeyTypeMLKEM768 // Default to MLKEM768
	}

	signer := &PQSigner{
		keyType:     keyType,
		address:     addrBytes,
		mlkemKey:    privKey,
		mlkemPubKey: pubKey,
		mlkemMode:   mode,
	}

	kc.keysByAddress[addrBytes] = signer
	kc.addressSet.Add(addrBytes)
	return addrBytes
}

// AddRingtail adds a ring signature key to the keychain
// scheme specifies which ring signature scheme to use (LSAG or LatticeLSAG)
func (kc *PQKeychain) AddRingtail(signer ring.Signer, scheme ring.Scheme) ids.ShortID {
	pubKeyBytes := signer.PublicKey()

	// Generate address from public key bytes
	hash := sha256.Sum256(pubKeyBytes)
	addrBytes := ids.ShortID{}
	copy(addrBytes[:], hash[:20])

	pqSigner := &PQSigner{
		keyType:    KeyTypeRingtail,
		address:    addrBytes,
		ringSigner: signer,
		ringScheme: scheme,
	}

	kc.keysByAddress[addrBytes] = pqSigner
	kc.addressSet.Add(addrBytes)
	return addrBytes
}

// AddHybrid adds a hybrid classical+PQ key pair
func (kc *PQKeychain) AddHybrid(classical *secp256k1.PrivateKey, pq interface{}) ids.ShortID {
	// Generate address from classical key for compatibility
	pk := classical.PublicKey()
	addr := pk.Address()
	shortAddr, _ := ids.ToShortID(addr[:])

	var keyType KeyType
	switch pq.(type) {
	case *mldsa.PrivateKey:
		keyType = KeyTypeHybridSecp256k1MLDSA44
	case *slhdsa.PrivateKey:
		keyType = KeyTypeHybridSecp256k1SLHDSA128
	default:
		return ids.ShortEmpty
	}

	signer := &PQSigner{
		keyType:         keyType,
		address:         shortAddr,
		hybridClassical: classical,
		hybridPQ:        pq,
	}

	kc.keysByAddress[shortAddr] = signer
	kc.addressSet.Add(shortAddr)
	return shortAddr
}

// AddHybridBLS adds a hybrid BLS + ML-DSA key pair
// This combines BLS for aggregatable consensus signatures with ML-DSA for post-quantum security
func (kc *PQKeychain) AddHybridBLS(blsKey *bls.SecretKey, pqKey *mldsa.PrivateKey) ids.ShortID {
	// Generate address from BLS public key
	pubKey := blsKey.PublicKey()
	pubKeyBytes := bls.PublicKeyToCompressedBytes(pubKey)
	hash := sha256.Sum256(pubKeyBytes)
	addrBytes := ids.ShortID{}
	copy(addrBytes[:], hash[:20])

	signer := &PQSigner{
		keyType:   KeyTypeHybridBLSMLDSA44,
		address:   addrBytes,
		hybridBLS: blsKey,
		hybridPQ:  pqKey,
	}

	kc.keysByAddress[addrBytes] = signer
	kc.addressSet.Add(addrBytes)
	return addrBytes
}

// GenerateKey generates a new key of the default type
func (kc *PQKeychain) GenerateKey() (ids.ShortID, error) {
	switch kc.defaultType {
	case KeyTypeSecp256k1:
		key, err := secp256k1.NewPrivateKey()
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddSecp256k1(key), nil

	case KeyTypeMLDSA44:
		key, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA44)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddMLDSA(key, KeyTypeMLDSA44), nil

	case KeyTypeMLDSA65:
		key, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddMLDSA(key, KeyTypeMLDSA65), nil

	case KeyTypeMLDSA87:
		key, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA87)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddMLDSA(key, KeyTypeMLDSA87), nil

	case KeyTypeSLHDSA128:
		key, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_128s)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddSLHDSA(key, KeyTypeSLHDSA128), nil

	case KeyTypeSLHDSA192:
		key, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_192s)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddSLHDSA(key, KeyTypeSLHDSA192), nil

	case KeyTypeSLHDSA256:
		key, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_256s)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddSLHDSA(key, KeyTypeSLHDSA256), nil

	case KeyTypeBLS:
		key, err := bls.NewSecretKey()
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddBLS(key), nil

	case KeyTypeMLKEM512:
		pubKey, privKey, err := mlkem.GenerateKey(mlkem.MLKEM512)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddMLKEM(pubKey, privKey, mlkem.MLKEM512), nil

	case KeyTypeMLKEM768:
		pubKey, privKey, err := mlkem.GenerateKey(mlkem.MLKEM768)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddMLKEM(pubKey, privKey, mlkem.MLKEM768), nil

	case KeyTypeMLKEM1024:
		pubKey, privKey, err := mlkem.GenerateKey(mlkem.MLKEM1024)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddMLKEM(pubKey, privKey, mlkem.MLKEM1024), nil

	case KeyTypeRingtail:
		// Default to LSAG (secp256k1-based) ring signatures
		// Use GenerateRingtailKey with specific scheme if needed
		signer, err := ring.NewSigner(ring.LSAG)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddRingtail(signer, ring.LSAG), nil

	case KeyTypeHybridSecp256k1MLDSA44:
		classical, err := secp256k1.NewPrivateKey()
		if err != nil {
			return ids.ShortEmpty, err
		}
		pq, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA44)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddHybrid(classical, pq), nil

	case KeyTypeHybridSecp256k1SLHDSA128:
		classical, err := secp256k1.NewPrivateKey()
		if err != nil {
			return ids.ShortEmpty, err
		}
		pq, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_128s)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddHybrid(classical, pq), nil

	case KeyTypeHybridBLSMLDSA44:
		blsKey, err := bls.NewSecretKey()
		if err != nil {
			return ids.ShortEmpty, err
		}
		pqKey, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA44)
		if err != nil {
			return ids.ShortEmpty, err
		}
		return kc.AddHybridBLS(blsKey, pqKey), nil

	default:
		return ids.ShortEmpty, fmt.Errorf("unsupported key type: %v", kc.defaultType)
	}
}

// GenerateRingtailKey generates a new ring signature key with a specific scheme.
// scheme can be ring.LSAG (secp256k1-based) or ring.LatticeLSAG (post-quantum).
func (kc *PQKeychain) GenerateRingtailKey(scheme ring.Scheme) (ids.ShortID, error) {
	signer, err := ring.NewSigner(scheme)
	if err != nil {
		return ids.ShortEmpty, err
	}
	return kc.AddRingtail(signer, scheme), nil
}

// Addresses returns all addresses in the keychain
func (kc *PQKeychain) Addresses() []ids.ShortID {
	addrs := make([]ids.ShortID, 0, kc.addressSet.Len())
	for addr := range kc.addressSet {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Get returns the signer for the given address
func (kc *PQKeychain) Get(addr ids.ShortID) (Signer, bool) {
	signer, exists := kc.keysByAddress[addr]
	if !exists {
		return nil, false
	}
	return signer, true
}

// GetPQSigner returns the PQ signer for advanced operations
func (kc *PQKeychain) GetPQSigner(addr ids.ShortID) (*PQSigner, bool) {
	signer, exists := kc.keysByAddress[addr]
	return signer, exists
}

// SetDefaultType sets the default key type for new keys
func (kc *PQKeychain) SetDefaultType(keyType KeyType) {
	kc.defaultType = keyType
}
