// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ringtail

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"

	"github.com/luxfi/node/ids"
)

const (
	// RingSize is the default size of the anonymity set
	DefaultRingSize = 16

	// SignatureSize is the size of a Ringtail signature in bytes
	SignatureSize = 32 * DefaultRingSize
)

var (
	errInvalidRingSize    = errors.New("invalid ring size")
	errInvalidPublicKey   = errors.New("invalid public key")
	errInvalidSignature   = errors.New("invalid signature")
	errRingNotComplete    = errors.New("ring is not complete")
)

// PublicKey represents a Ringtail public key
type PublicKey struct {
	Point *Point
}

// PrivateKey represents a Ringtail private key
type PrivateKey struct {
	Scalar *big.Int
}

// Point represents a point on the elliptic curve
type Point struct {
	X, Y *big.Int
}

// RingSignature represents a Ringtail ring signature
type RingSignature struct {
	C0         *big.Int
	S          []*big.Int
	KeyImage   *Point
	RingPubKeys []*PublicKey
}

// Factory implements a factory for Ringtail keys
type Factory struct{}

// NewPrivateKey generates a new private key
func (*Factory) NewPrivateKey() (*PrivateKey, error) {
	scalar, err := rand.Int(rand.Reader, curveOrder())
	if err != nil {
		return nil, err
	}
	
	return &PrivateKey{Scalar: scalar}, nil
}

// ToPublicKey converts a private key to its corresponding public key
func (*Factory) ToPublicKey(privKey *PrivateKey) (*PublicKey, error) {
	if privKey == nil {
		return nil, errors.New("nil private key")
	}
	
	point := scalarBaseMult(privKey.Scalar)
	return &PublicKey{Point: point}, nil
}

// Sign creates a ring signature
func (priv *PrivateKey) Sign(message []byte, ring []*PublicKey) (*RingSignature, error) {
	if len(ring) != DefaultRingSize {
		return nil, errInvalidRingSize
	}
	
	// Find the signer's position in the ring
	pubKey := priv.PublicKey()
	signerIndex := -1
	for i, key := range ring {
		if key.Equal(pubKey) {
			signerIndex = i
			break
		}
	}
	
	if signerIndex == -1 {
		return nil, errors.New("signer's public key not found in ring")
	}
	
	// Generate key image
	keyImage := generateKeyImage(priv)
	
	// Initialize signature components
	c := make([]*big.Int, DefaultRingSize)
	s := make([]*big.Int, DefaultRingSize)
	
	// Generate random responses for all except signer
	for i := 0; i < DefaultRingSize; i++ {
		if i != signerIndex {
			s[i], _ = rand.Int(rand.Reader, curveOrder())
		}
	}
	
	// Start the ring computation
	// Generate random nonce
	k, _ := rand.Int(rand.Reader, curveOrder())
	
	// Compute L_i and R_i for all ring members
	L := make([]*Point, DefaultRingSize)
	R := make([]*Point, DefaultRingSize)
	
	// For the signer
	L[signerIndex] = scalarBaseMult(k)
	R[signerIndex] = scalarMult(hashToPoint(ring[signerIndex].Point), k)
	
	// Compute c_{i+1} starting from signer
	c[(signerIndex+1)%DefaultRingSize] = hashPoints(message, L[signerIndex], R[signerIndex])
	
	// Complete the ring
	for i := (signerIndex + 1) % DefaultRingSize; i != signerIndex; i = (i + 1) % DefaultRingSize {
		L[i] = addPoints(
			scalarBaseMult(s[i]),
			scalarMult(ring[i].Point, c[i]),
		)
		R[i] = addPoints(
			scalarMult(hashToPoint(ring[i].Point), s[i]),
			scalarMult(keyImage, c[i]),
		)
		
		c[(i+1)%DefaultRingSize] = hashPoints(message, L[i], R[i])
	}
	
	// Complete the signature for the signer
	s[signerIndex] = new(big.Int).Sub(k, new(big.Int).Mul(c[signerIndex], priv.Scalar))
	s[signerIndex].Mod(s[signerIndex], curveOrder())
	
	return &RingSignature{
		C0:          c[0],
		S:           s,
		KeyImage:    keyImage,
		RingPubKeys: ring,
	}, nil
}

// Verify verifies a ring signature
func (sig *RingSignature) Verify(message []byte) bool {
	if len(sig.S) != DefaultRingSize || len(sig.RingPubKeys) != DefaultRingSize {
		return false
	}
	
	// Recompute c values
	c := make([]*big.Int, DefaultRingSize)
	c[0] = sig.C0
	
	for i := 0; i < DefaultRingSize; i++ {
		// Compute L_i = s_i * G + c_i * P_i
		L := addPoints(
			scalarBaseMult(sig.S[i]),
			scalarMult(sig.RingPubKeys[i].Point, c[i]),
		)
		
		// Compute R_i = s_i * H(P_i) + c_i * I
		R := addPoints(
			scalarMult(hashToPoint(sig.RingPubKeys[i].Point), sig.S[i]),
			scalarMult(sig.KeyImage, c[i]),
		)
		
		// Compute next c value
		if i < DefaultRingSize-1 {
			c[i+1] = hashPoints(message, L, R)
		} else {
			// For the last iteration, check if we get back to c[0]
			computedC0 := hashPoints(message, L, R)
			return computedC0.Cmp(sig.C0) == 0
		}
	}
	
	return false
}

// PublicKey returns the public key corresponding to the private key
func (priv *PrivateKey) PublicKey() *PublicKey {
	return &PublicKey{Point: scalarBaseMult(priv.Scalar)}
}

// Equal checks if two public keys are equal
func (pub *PublicKey) Equal(other *PublicKey) bool {
	return pub.Point.X.Cmp(other.Point.X) == 0 && pub.Point.Y.Cmp(other.Point.Y) == 0
}

// Bytes returns the byte representation of the public key
func (pub *PublicKey) Bytes() []byte {
	return append(pub.Point.X.Bytes(), pub.Point.Y.Bytes()...)
}

// Address returns the address of the public key
func (pub *PublicKey) Address() ids.ShortID {
	hash := sha256.Sum256(pub.Bytes())
	addr, _ := ids.ToShortID(hash[:])
	return addr
}

// Helper functions for elliptic curve operations
func curveOrder() *big.Int {
	// Simplified curve order for demonstration
	// In production, use actual curve parameters
	order, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
	return order
}

func scalarBaseMult(k *big.Int) *Point {
	// Simplified scalar multiplication with base point
	// In production, use actual elliptic curve operations
	x := new(big.Int).Mul(k, big.NewInt(2))
	y := new(big.Int).Mul(k, big.NewInt(3))
	return &Point{X: x, Y: y}
}

func scalarMult(p *Point, k *big.Int) *Point {
	// Simplified scalar multiplication
	// In production, use actual elliptic curve operations
	x := new(big.Int).Mul(p.X, k)
	y := new(big.Int).Mul(p.Y, k)
	return &Point{X: x, Y: y}
}

func addPoints(p1, p2 *Point) *Point {
	// Simplified point addition
	// In production, use actual elliptic curve operations
	x := new(big.Int).Add(p1.X, p2.X)
	y := new(big.Int).Add(p1.Y, p2.Y)
	return &Point{X: x, Y: y}
}

func hashToPoint(p *Point) *Point {
	// Hash a point to another point on the curve
	h := sha256.Sum256(append(p.X.Bytes(), p.Y.Bytes()...))
	x := new(big.Int).SetBytes(h[:16])
	y := new(big.Int).SetBytes(h[16:])
	return &Point{X: x, Y: y}
}

func hashPoints(message []byte, points ...*Point) *big.Int {
	h := sha256.New()
	h.Write(message)
	for _, p := range points {
		h.Write(p.X.Bytes())
		h.Write(p.Y.Bytes())
	}
	return new(big.Int).SetBytes(h.Sum(nil))
}

func generateKeyImage(priv *PrivateKey) *Point {
	// Key image = x * H(P)
	pubKey := priv.PublicKey()
	hashedPoint := hashToPoint(pubKey.Point)
	return scalarMult(hashedPoint, priv.Scalar)
}