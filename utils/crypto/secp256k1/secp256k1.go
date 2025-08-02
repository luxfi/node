// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/crypto"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/utils/cb58"
	"github.com/luxfi/node/utils/hashing"
)

const (
	// SignatureLen is the number of bytes in a secp2561k recoverable signature
	SignatureLen = 65

	// PrivateKeyLen is the number of bytes in a secp2561k recoverable private
	// key
	PrivateKeyLen = 32

	// PublicKeyLen is the number of bytes in a secp2561k recoverable public key
	PublicKeyLen = 33

	// from the decred library:
	// compactSigMagicOffset is a value used when creating the compact signature
	// recovery code inherited from Bitcoin and has no meaning, but has been
	// retained for compatibility.  For historical purposes, it was originally
	// picked to avoid a binary representation that would allow compact
	// signatures to be mistaken for other components.
	compactSigMagicOffset = 27

	PrivateKeyPrefix = "PrivateKey-"
	nullStr          = "null"
)

var (
	ErrInvalidSig              = errors.New("invalid signature")
	errCompressed              = errors.New("wasn't expecting a compressed key")
	errMissingQuotes           = errors.New("first and last characters should be quotes")
	errMissingKeyPrefix        = fmt.Errorf("private key missing %s prefix", PrivateKeyPrefix)
	errInvalidPrivateKeyLength = fmt.Errorf("private key has unexpected length, expected %d", PrivateKeyLen)
	errInvalidPublicKeyLength  = fmt.Errorf("public key has unexpected length, expected %d", PublicKeyLen)
	errInvalidSigLen           = errors.New("invalid signature length")
	errMutatedSig              = errors.New("signature was mutated from its original format")
)

func NewPrivateKey() (*PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &PrivateKey{sk: privateKey}, nil
}

func ToPublicKey(b []byte) (*PublicKey, error) {
	if len(b) != PublicKeyLen {
		return nil, errInvalidPublicKeyLength
	}

	// Decompress the public key using Geth's crypto library
	pubKey, err := crypto.DecompressPubkey(b)
	if err != nil {
		return nil, err
	}

	return &PublicKey{
		pk:    pubKey,
		bytes: b,
	}, nil
}

func ToPrivateKey(b []byte) (*PrivateKey, error) {
	if len(b) != PrivateKeyLen {
		return nil, errInvalidPrivateKeyLength
	}

	// Convert bytes to ECDSA private key
	privateKey, err := crypto.ToECDSA(b)
	if err != nil {
		return nil, err
	}

	return &PrivateKey{
		sk:    privateKey,
		bytes: b,
	}, nil
}

func RecoverPublicKey(msg, sig []byte) (*PublicKey, error) {
	return RecoverPublicKeyFromHash(hashing.ComputeHash256(msg), sig)
}

func RecoverPublicKeyFromHash(hash, sig []byte) (*PublicKey, error) {
	if err := verifySECP256K1RSignatureFormat(sig); err != nil {
		return nil, err
	}

	// The signature is in [r || s || v] format, but Geth expects [r || s || v]
	// where v is 0 or 1 (not 27 or 28)
	if len(sig) != SignatureLen {
		return nil, errInvalidSigLen
	}

	// Create a copy to avoid modifying the original
	sigCopy := make([]byte, SignatureLen)
	copy(sigCopy, sig)

	// Geth's crypto.Ecrecover expects the signature in [r || s || v] format
	// where v is 0 or 1 (recovery id), not 27 or 28
	rawPubkey, err := crypto.Ecrecover(hash, sigCopy)
	if err != nil {
		return nil, ErrInvalidSig
	}

	// Convert to public key
	pubKey, err := crypto.UnmarshalPubkey(rawPubkey)
	if err != nil {
		return nil, err
	}

	return &PublicKey{pk: pubKey}, nil
}

type RecoverCache struct {
	cache cache.Cacher[ids.ID, *PublicKey]
}

func NewRecoverCache(size int) *RecoverCache {
	return &RecoverCache{
		cache: lru.NewCache[ids.ID, *PublicKey](size),
	}
}

func (r *RecoverCache) RecoverPublicKey(msg, sig []byte) (*PublicKey, error) {
	return r.RecoverPublicKeyFromHash(hashing.ComputeHash256(msg), sig)
}

func (r *RecoverCache) RecoverPublicKeyFromHash(hash, sig []byte) (*PublicKey, error) {
	// TODO: This type should always be initialized by calling NewRecoverCache.
	if r == nil || r.cache == nil {
		return RecoverPublicKeyFromHash(hash, sig)
	}

	cacheBytes := make([]byte, len(hash)+len(sig))
	copy(cacheBytes, hash)
	copy(cacheBytes[len(hash):], sig)
	id := hashing.ComputeHash256Array(cacheBytes)
	if cachedPublicKey, ok := r.cache.Get(id); ok {
		return cachedPublicKey, nil
	}

	pubKey, err := RecoverPublicKeyFromHash(hash, sig)
	if err != nil {
		return nil, err
	}

	r.cache.Put(id, pubKey)
	return pubKey, nil
}

type PublicKey struct {
	pk    *ecdsa.PublicKey
	addr  ids.ShortID
	bytes []byte
}

func (k *PublicKey) Verify(msg, sig []byte) bool {
	return k.VerifyHash(hashing.ComputeHash256(msg), sig)
}

func (k *PublicKey) VerifyHash(hash, sig []byte) bool {
	pk, err := RecoverPublicKeyFromHash(hash, sig)
	if err != nil {
		return false
	}
	return k.Address() == pk.Address()
}

// ToECDSA returns the ecdsa representation of this public key
func (k *PublicKey) ToECDSA() *ecdsa.PublicKey {
	return k.pk
}

func (k *PublicKey) Address() ids.ShortID {
	if k.addr == ids.ShortEmpty {
		addr, err := ids.ToShortID(hashing.PubkeyBytesToAddress(k.Bytes()))
		if err != nil {
			panic(err)
		}
		k.addr = addr
	}
	return k.addr
}

func (k *PublicKey) EthAddress() common.Address {
	return crypto.PubkeyToAddress(*k.pk)
}

func (k *PublicKey) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = crypto.CompressPubkey(k.pk)
	}
	return k.bytes
}

type PrivateKey struct {
	sk    *ecdsa.PrivateKey
	pk    *PublicKey
	bytes []byte
}

func (k *PrivateKey) PublicKey() *PublicKey {
	if k.pk == nil {
		k.pk = &PublicKey{pk: &k.sk.PublicKey}
	}
	return k.pk
}

func (k *PrivateKey) Address() ids.ShortID {
	return k.PublicKey().Address()
}

func (k *PrivateKey) EthAddress() common.Address {
	return crypto.PubkeyToAddress(k.sk.PublicKey)
}

func (k *PrivateKey) Sign(msg []byte) ([]byte, error) {
	return k.SignHash(hashing.ComputeHash256(msg))
}

func (k *PrivateKey) SignHash(hash []byte) ([]byte, error) {
	// Sign the hash using Geth's crypto library
	sig, err := crypto.Sign(hash, k.sk)
	if err != nil {
		return nil, err
	}

	// Geth's crypto.Sign returns signature in [r || s || v] format where v is 0 or 1
	// We need to ensure it's in the correct format [r || s || v] for our API
	if len(sig) != SignatureLen {
		return nil, errInvalidSigLen
	}

	return sig, nil
}

// ToECDSA returns the ecdsa representation of this private key
func (k *PrivateKey) ToECDSA() *ecdsa.PrivateKey {
	return k.sk
}

func (k *PrivateKey) Bytes() []byte {
	if k.bytes == nil {
		k.bytes = crypto.FromECDSA(k.sk)
	}
	return k.bytes
}

func (k *PrivateKey) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of a SECP256K1 private key
	keyStr, _ := cb58.Encode(k.Bytes())
	return PrivateKeyPrefix + keyStr
}

func (k *PrivateKey) MarshalJSON() ([]byte, error) {
	return []byte(`"` + k.String() + `"`), nil
}

func (k *PrivateKey) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

func (k *PrivateKey) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	strNoQuotes := str[1:lastIndex]
	if !strings.HasPrefix(strNoQuotes, PrivateKeyPrefix) {
		return errMissingKeyPrefix
	}

	strNoPrefix := strNoQuotes[len(PrivateKeyPrefix):]
	keyBytes, err := cb58.Decode(strNoPrefix)
	if err != nil {
		return err
	}
	if len(keyBytes) != PrivateKeyLen {
		return errInvalidPrivateKeyLength
	}

	privateKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return err
	}

	*k = PrivateKey{
		sk:    privateKey,
		bytes: keyBytes,
	}
	return nil
}

func (k *PrivateKey) UnmarshalText(text []byte) error {
	return k.UnmarshalJSON(text)
}

// raw sig has format [v || r || s] whereas the sig has format [r || s || v]
func rawSigToSig(sig []byte) ([]byte, error) {
	if len(sig) != SignatureLen {
		return nil, errInvalidSigLen
	}
	recCode := sig[0]
	copy(sig, sig[1:])
	sig[SignatureLen-1] = recCode - compactSigMagicOffset
	return sig, nil
}

// sig has format [r || s || v] whereas the raw sig has format [v || r || s]
func sigToRawSig(sig []byte) ([]byte, error) {
	if len(sig) != SignatureLen {
		return nil, errInvalidSigLen
	}
	newSig := make([]byte, SignatureLen)
	newSig[0] = sig[SignatureLen-1] + compactSigMagicOffset
	copy(newSig[1:], sig)
	return newSig, nil
}

// verifies the signature format in format [r || s || v]
func verifySECP256K1RSignatureFormat(sig []byte) error {
	if len(sig) != SignatureLen {
		return errInvalidSigLen
	}

	// Check if S value is over half order (malleability check)
	// S value is bytes 32-64 of the signature
	s := new(big.Int).SetBytes(sig[32:64])

	// Half of the order of the secp256k1 curve
	halfOrder := new(big.Int).Rsh(crypto.S256().Params().N, 1)

	if s.Cmp(halfOrder) > 0 {
		return errMutatedSig
	}
	return nil
}
