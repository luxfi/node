package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254"
)

// CeremonyState holds the full state of a powers-of-tau ceremony.
type CeremonyState struct {
	Circuit        string         `json:"circuit"`
	NumConstraints int            `json:"numConstraints"`
	PowersNeeded   int            `json:"powersNeeded"`
	Participants   int            `json:"participants"`
	TauG1          []G1Point      `json:"tauG1"`
	TauG2          []G2Point      `json:"tauG2"`
	AlphaG1        []G1Point      `json:"alphaG1"`
	BetaG1         []G1Point      `json:"betaG1"`
	BetaG2         G2Point        `json:"betaG2"`
	Contributions  []Contribution `json:"contributions"`
}

// Contribution records a single participant's contribution.
type Contribution struct {
	Participant string `json:"participant"`
	Hash        string `json:"hash"`      // SHA-256 of randomness commitment
	PrevHash    string `json:"prevHash"`  // StateHash of previous contribution (empty for first)
	StateHash   string `json:"stateHash"` // SHA-256 of SRS state AFTER this contribution
	Timestamp   string `json:"timestamp"`
}

// G1Point is a JSON-serializable wrapper for bn254.G1Affine.
type G1Point struct {
	bn254.G1Affine
}

func (p G1Point) MarshalJSON() ([]byte, error) {
	b := p.G1Affine.Marshal()
	return json.Marshal(hex.EncodeToString(b))
}

func (p *G1Point) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("decode hex: %w", err)
	}
	return p.G1Affine.Unmarshal(b)
}

// G2Point is a JSON-serializable wrapper for bn254.G2Affine.
type G2Point struct {
	bn254.G2Affine
}

func (p G2Point) MarshalJSON() ([]byte, error) {
	b := p.G2Affine.Marshal()
	return json.Marshal(hex.EncodeToString(b))
}

func (p *G2Point) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("decode hex: %w", err)
	}
	return p.G2Affine.Unmarshal(b)
}

func newContribution(participant string, hash []byte, prevHash string, stateHash string) Contribution {
	return Contribution{
		Participant: participant,
		Hash:        hex.EncodeToString(hash),
		PrevHash:    prevHash,
		StateHash:   stateHash,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// computeStateHash computes SHA-256 over all SRS points for hash chain verification.
func computeStateHash(state *CeremonyState) string {
	h := sha256.New()
	for i := range state.TauG1 {
		h.Write(state.TauG1[i].Marshal())
	}
	for i := range state.TauG2 {
		h.Write(state.TauG2[i].Marshal())
	}
	for i := range state.AlphaG1 {
		h.Write(state.AlphaG1[i].Marshal())
	}
	for i := range state.BetaG1 {
		h.Write(state.BetaG1[i].Marshal())
	}
	h.Write(state.BetaG2.Marshal())
	return hex.EncodeToString(h.Sum(nil))
}
