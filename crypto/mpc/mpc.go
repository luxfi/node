// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpc

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
	"sync"

	"github.com/luxfi/node/ids"
)

const (
	// DefaultThreshold is the default threshold for MPC
	DefaultThreshold = 3
	
	// DefaultParties is the default number of parties
	DefaultParties = 5
)

var (
	errInvalidThreshold = errors.New("invalid threshold")
	errInvalidParties   = errors.New("invalid number of parties")
	errNotEnoughShares  = errors.New("not enough shares to reconstruct")
	errInvalidShare     = errors.New("invalid share")
)

// MPCAccount represents a per-account MPC configuration
type MPCAccount struct {
	AccountID  ids.ShortID
	Threshold  int
	Parties    int
	PublicKey  *PublicKey
	Shares     map[int]*Share
	Protocol   Protocol
	mu         sync.RWMutex
}

// Share represents a secret share held by a party
type Share struct {
	Index  int
	Value  *big.Int
	Proof  []byte
}

// PublicKey represents an MPC public key
type PublicKey struct {
	Point *Point
}

// Point represents a point on the elliptic curve
type Point struct {
	X, Y *big.Int
}

// Protocol represents the MPC protocol type
type Protocol string

const (
	ProtocolGG18 Protocol = "gg18"
	ProtocolGG20 Protocol = "gg20"
	ProtocolCMP  Protocol = "cmp"
)

// Manager manages per-account MPC configurations
type Manager struct {
	accounts map[ids.ShortID]*MPCAccount
	mu       sync.RWMutex
}

// NewManager creates a new MPC manager
func NewManager() *Manager {
	return &Manager{
		accounts: make(map[ids.ShortID]*MPCAccount),
	}
}

// CreateAccount creates a new MPC account
func (m *Manager) CreateAccount(accountID ids.ShortID, threshold, parties int, protocol Protocol) (*MPCAccount, error) {
	if threshold < 1 || threshold > parties {
		return nil, errInvalidThreshold
	}
	if parties < 2 {
		return nil, errInvalidParties
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check if account already exists
	if _, exists := m.accounts[accountID]; exists {
		return nil, errors.New("account already exists")
	}
	
	// Generate distributed key
	account := &MPCAccount{
		AccountID: accountID,
		Threshold: threshold,
		Parties:   parties,
		Shares:    make(map[int]*Share),
		Protocol:  protocol,
	}
	
	// Generate key shares
	if err := account.generateKeyShares(); err != nil {
		return nil, err
	}
	
	m.accounts[accountID] = account
	return account, nil
}

// GetAccount retrieves an MPC account
func (m *Manager) GetAccount(accountID ids.ShortID) (*MPCAccount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	account, exists := m.accounts[accountID]
	if !exists {
		return nil, errors.New("account not found")
	}
	
	return account, nil
}

// generateKeyShares generates threshold key shares
func (a *MPCAccount) generateKeyShares() error {
	// Generate random polynomial coefficients
	coeffs := make([]*big.Int, a.Threshold)
	for i := 0; i < a.Threshold; i++ {
		coeff, err := rand.Int(rand.Reader, curveOrder())
		if err != nil {
			return err
		}
		coeffs[i] = coeff
	}
	
	// The secret is the constant term
	secret := coeffs[0]
	
	// Generate shares for each party
	for i := 1; i <= a.Parties; i++ {
		share := evaluatePolynomial(coeffs, big.NewInt(int64(i)))
		a.Shares[i] = &Share{
			Index: i,
			Value: share,
			Proof: generateShareProof(share, i),
		}
	}
	
	// Compute public key
	a.PublicKey = &PublicKey{
		Point: scalarBaseMult(secret),
	}
	
	return nil
}

// Sign creates a threshold signature
func (a *MPCAccount) Sign(message []byte, participatingShares map[int]*Share) ([]byte, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	if len(participatingShares) < a.Threshold {
		return nil, errNotEnoughShares
	}
	
	// Verify shares
	for index, share := range participatingShares {
		if !a.verifyShare(index, share) {
			return nil, errInvalidShare
		}
	}
	
	// Compute partial signatures
	messageHash := sha256.Sum256(message)
	partialSigs := make(map[int]*big.Int)
	
	for index, share := range participatingShares {
		// Simplified partial signature computation
		// In production, use actual protocol (GG18/GG20/CMP)
		partialSig := new(big.Int).Mul(share.Value, new(big.Int).SetBytes(messageHash[:]))
		partialSig.Mod(partialSig, curveOrder())
		partialSigs[index] = partialSig
	}
	
	// Combine partial signatures using Lagrange interpolation
	signature := combinePartialSignatures(partialSigs, a.Threshold)
	
	return signature.Bytes(), nil
}

// Verify verifies a signature
func (a *MPCAccount) Verify(message []byte, signature []byte) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	// Simplified verification
	// In production, implement actual signature verification
	messageHash := sha256.Sum256(message)
	sig := new(big.Int).SetBytes(signature)
	
	// Verify using public key
	expectedPoint := scalarBaseMult(sig)
	messagePoint := scalarBaseMult(new(big.Int).SetBytes(messageHash[:]))
	
	// Check if signature is valid (simplified)
	return pointsEqual(expectedPoint, messagePoint)
}

// AddShare adds a share to the account
func (a *MPCAccount) AddShare(index int, share *Share) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if index < 1 || index > a.Parties {
		return errors.New("invalid share index")
	}
	
	if !a.verifyShare(index, share) {
		return errInvalidShare
	}
	
	a.Shares[index] = share
	return nil
}

// GetShare retrieves a specific share
func (a *MPCAccount) GetShare(index int) (*Share, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	share, exists := a.Shares[index]
	if !exists {
		return nil, errors.New("share not found")
	}
	
	return share, nil
}

// RefreshShares performs proactive secret sharing to refresh shares
func (a *MPCAccount) RefreshShares() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	// Generate new random polynomial with same secret
	oldShares := a.Shares
	a.Shares = make(map[int]*Share)
	
	// Keep the same secret (constant term)
	secret := reconstructSecret(oldShares, a.Threshold)
	
	// Generate new polynomial with same secret
	coeffs := make([]*big.Int, a.Threshold)
	coeffs[0] = secret
	
	for i := 1; i < a.Threshold; i++ {
		coeff, err := rand.Int(rand.Reader, curveOrder())
		if err != nil {
			return err
		}
		coeffs[i] = coeff
	}
	
	// Generate new shares
	for i := 1; i <= a.Parties; i++ {
		share := evaluatePolynomial(coeffs, big.NewInt(int64(i)))
		a.Shares[i] = &Share{
			Index: i,
			Value: share,
			Proof: generateShareProof(share, i),
		}
	}
	
	return nil
}

// Helper functions

func curveOrder() *big.Int {
	// Simplified curve order
	order, _ := new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
	return order
}

func scalarBaseMult(k *big.Int) *Point {
	// Simplified scalar multiplication
	x := new(big.Int).Mul(k, big.NewInt(2))
	y := new(big.Int).Mul(k, big.NewInt(3))
	return &Point{X: x, Y: y}
}

func pointsEqual(p1, p2 *Point) bool {
	return p1.X.Cmp(p2.X) == 0 && p1.Y.Cmp(p2.Y) == 0
}

func evaluatePolynomial(coeffs []*big.Int, x *big.Int) *big.Int {
	result := new(big.Int).Set(coeffs[0])
	xPower := new(big.Int).Set(x)
	
	for i := 1; i < len(coeffs); i++ {
		term := new(big.Int).Mul(coeffs[i], xPower)
		result.Add(result, term)
		result.Mod(result, curveOrder())
		
		xPower.Mul(xPower, x)
		xPower.Mod(xPower, curveOrder())
	}
	
	return result
}

func generateShareProof(share *big.Int, index int) []byte {
	// Generate proof of share validity
	h := sha256.New()
	h.Write(share.Bytes())
	h.Write([]byte{byte(index)})
	return h.Sum(nil)
}

func (a *MPCAccount) verifyShare(index int, share *Share) bool {
	// Verify share proof
	expectedProof := generateShareProof(share.Value, index)
	if len(share.Proof) != len(expectedProof) {
		return false
	}
	
	for i := range expectedProof {
		if expectedProof[i] != share.Proof[i] {
			return false
		}
	}
	
	return true
}

func combinePartialSignatures(partialSigs map[int]*big.Int, threshold int) *big.Int {
	result := big.NewInt(0)
	indices := make([]int, 0, len(partialSigs))
	
	for index := range partialSigs {
		indices = append(indices, index)
		if len(indices) == threshold {
			break
		}
	}
	
	for _, i := range indices {
		lagrangeCoeff := computeLagrangeCoefficient(i, indices)
		term := new(big.Int).Mul(partialSigs[i], lagrangeCoeff)
		result.Add(result, term)
	}
	
	result.Mod(result, curveOrder())
	return result
}

func computeLagrangeCoefficient(i int, indices []int) *big.Int {
	num := big.NewInt(1)
	den := big.NewInt(1)
	
	for _, j := range indices {
		if i != j {
			num.Mul(num, big.NewInt(int64(-j)))
			den.Mul(den, big.NewInt(int64(i-j)))
		}
	}
	
	// Compute num/den mod curveOrder
	denInv := new(big.Int).ModInverse(den, curveOrder())
	result := new(big.Int).Mul(num, denInv)
	result.Mod(result, curveOrder())
	
	return result
}

func reconstructSecret(shares map[int]*Share, threshold int) *big.Int {
	if len(shares) < threshold {
		return nil
	}
	
	partialValues := make(map[int]*big.Int)
	for index, share := range shares {
		partialValues[index] = share.Value
		if len(partialValues) == threshold {
			break
		}
	}
	
	return combinePartialSignatures(partialValues, threshold)
}