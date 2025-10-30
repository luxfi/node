// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cggmp21

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// PaillierPublicKey represents a Paillier public key
type PaillierPublicKey struct {
	N      *big.Int // n = p*q
	NSq    *big.Int // n^2
	G      *big.Int // generator (typically n+1)
}

// PaillierPrivateKey represents a Paillier private key
type PaillierPrivateKey struct {
	PublicKey *PaillierPublicKey
	Lambda    *big.Int // lcm(p-1, q-1)
	Mu        *big.Int // modular multiplicative inverse
	P         *big.Int // prime p
	Q         *big.Int // prime q
}

// GeneratePaillierKeyPair generates a new Paillier keypair
func GeneratePaillierKeyPair(bits int) (*PaillierPrivateKey, *PaillierPublicKey, error) {
	// Generate two large primes p and q
	p, err := rand.Prime(rand.Reader, bits/2)
	if err != nil {
		return nil, nil, err
	}
	
	q, err := rand.Prime(rand.Reader, bits/2)
	if err != nil {
		return nil, nil, err
	}
	
	// Compute n = p*q
	n := new(big.Int).Mul(p, q)
	nSq := new(big.Int).Mul(n, n)
	
	// Compute lambda = lcm(p-1, q-1)
	pMinus1 := new(big.Int).Sub(p, big.NewInt(1))
	qMinus1 := new(big.Int).Sub(q, big.NewInt(1))
	
	gcd := new(big.Int).GCD(nil, nil, pMinus1, qMinus1)
	lambda := new(big.Int).Mul(pMinus1, qMinus1)
	lambda.Div(lambda, gcd)
	
	// Set g = n+1 (standard choice)
	g := new(big.Int).Add(n, big.NewInt(1))
	
	// Compute mu = (L(g^lambda mod n^2))^(-1) mod n
	// where L(x) = (x-1)/n
	gLambda := new(big.Int).Exp(g, lambda, nSq)
	l := L(gLambda, n)
	mu := new(big.Int).ModInverse(l, n)
	
	if mu == nil {
		return nil, nil, errors.New("failed to compute modular inverse")
	}
	
	pubKey := &PaillierPublicKey{
		N:   n,
		NSq: nSq,
		G:   g,
	}
	
	privKey := &PaillierPrivateKey{
		PublicKey: pubKey,
		Lambda:    lambda,
		Mu:        mu,
		P:         p,
		Q:         q,
	}
	
	return privKey, pubKey, nil
}

// Encrypt encrypts a plaintext using Paillier encryption
func (pub *PaillierPublicKey) Encrypt(plaintext *big.Int) (*big.Int, error) {
	// Check plaintext is in valid range
	if plaintext.Cmp(pub.N) >= 0 || plaintext.Sign() < 0 {
		return nil, errors.New("plaintext out of range")
	}
	
	// Generate random r where gcd(r, n) = 1
	var r *big.Int
	for {
		r, _ = rand.Int(rand.Reader, pub.N)
		if new(big.Int).GCD(nil, nil, r, pub.N).Cmp(big.NewInt(1)) == 0 {
			break
		}
	}
	
	// Compute ciphertext = g^m * r^n mod n^2
	gm := new(big.Int).Exp(pub.G, plaintext, pub.NSq)
	rn := new(big.Int).Exp(r, pub.N, pub.NSq)
	
	ciphertext := new(big.Int).Mul(gm, rn)
	ciphertext.Mod(ciphertext, pub.NSq)
	
	return ciphertext, nil
}

// Decrypt decrypts a ciphertext using Paillier decryption
func (priv *PaillierPrivateKey) Decrypt(ciphertext *big.Int) (*big.Int, error) {
	// Check ciphertext is in valid range
	if ciphertext.Cmp(priv.PublicKey.NSq) >= 0 || ciphertext.Sign() <= 0 {
		return nil, errors.New("ciphertext out of range")
	}
	
	// Compute plaintext = L(c^lambda mod n^2) * mu mod n
	cLambda := new(big.Int).Exp(ciphertext, priv.Lambda, priv.PublicKey.NSq)
	l := L(cLambda, priv.PublicKey.N)
	
	plaintext := new(big.Int).Mul(l, priv.Mu)
	plaintext.Mod(plaintext, priv.PublicKey.N)
	
	return plaintext, nil
}

// Add performs homomorphic addition of two ciphertexts
func (pub *PaillierPublicKey) Add(c1, c2 *big.Int) *big.Int {
	// E(m1) * E(m2) = E(m1 + m2)
	result := new(big.Int).Mul(c1, c2)
	result.Mod(result, pub.NSq)
	return result
}

// Multiply performs homomorphic multiplication by a constant
func (pub *PaillierPublicKey) Multiply(ciphertext, constant *big.Int) *big.Int {
	// E(m)^k = E(k*m)
	result := new(big.Int).Exp(ciphertext, constant, pub.NSq)
	return result
}

// L computes L(x) = (x-1)/n
func L(x, n *big.Int) *big.Int {
	return new(big.Int).Div(new(big.Int).Sub(x, big.NewInt(1)), n)
}

// ZKProof represents a zero-knowledge proof of plaintext knowledge
type ZKProof struct {
	E   *big.Int // Commitment
	Z   *big.Int // Response
	Pub *PaillierPublicKey
}

// ProveKnowledge creates a ZK proof of plaintext knowledge
func ProveKnowledge(pub *PaillierPublicKey, plaintext, randomness *big.Int) (*ZKProof, error) {
	// This is a simplified Schnorr-like proof
	// In production, use proper ZK proofs as specified in CGGMP21
	
	// Generate random values
	r1, _ := rand.Int(rand.Reader, pub.N)
	r2, _ := rand.Int(rand.Reader, pub.N)
	
	// Compute commitment e = g^r1 * r2^n mod n^2
	gr1 := new(big.Int).Exp(pub.G, r1, pub.NSq)
	r2n := new(big.Int).Exp(r2, pub.N, pub.NSq)
	e := new(big.Int).Mul(gr1, r2n)
	e.Mod(e, pub.NSq)
	
	// Compute challenge (in practice, use Fiat-Shamir)
	challenge := new(big.Int).SetBytes([]byte("challenge"))
	challenge.Mod(challenge, pub.N)
	
	// Compute response z = r1 + challenge * plaintext
	z := new(big.Int).Mul(challenge, plaintext)
	z.Add(z, r1)
	z.Mod(z, pub.N)
	
	return &ZKProof{
		E:   e,
		Z:   z,
		Pub: pub,
	}, nil
}

// VerifyKnowledge verifies a ZK proof of plaintext knowledge
func VerifyKnowledge(proof *ZKProof, ciphertext *big.Int) bool {
	// Simplified verification
	// In production, implement full verification as per CGGMP21
	
	// Recompute challenge
	challenge := new(big.Int).SetBytes([]byte("challenge"))
	challenge.Mod(challenge, proof.Pub.N)
	
	// Verify: g^z = e * c^challenge mod n^2
	gz := new(big.Int).Exp(proof.Pub.G, proof.Z, proof.Pub.NSq)
	
	cc := new(big.Int).Exp(ciphertext, challenge, proof.Pub.NSq)
	ec := new(big.Int).Mul(proof.E, cc)
	ec.Mod(ec, proof.Pub.NSq)
	
	return gz.Cmp(ec) == 0
}