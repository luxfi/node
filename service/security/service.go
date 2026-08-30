// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package security

import (
	"encoding/hex"
	"errors"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/log"
)

// ErrNoProfile is returned when a call asks for security data but the node
// booted without a SecurityProfile pin (legacy networks). The error message
// names the missing pin so an operator who deploys a strict-PQ wallet against a
// permissive backend sees the mismatch immediately.
var ErrNoProfile = errors.New(
	"security: node booted without a chain-wide SecurityProfile pin " +
		"(genesis carries no SecurityProfile{} block); RPC unavailable")

// Service answers for the chain-wide security posture this node runs under.
// Read-only: the profile is set once at construction and never mutated. Its
// operations are registered by [Service.Ops].
type Service struct {
	log     log.Logger
	profile *consensusconfig.ChainSecurityProfile
}

// New builds the security service. Profile may be nil — see [ErrNoProfile].
func New(logger log.Logger, profile *consensusconfig.ChainSecurityProfile) *Service {
	return &Service{log: logger, profile: profile}
}

// buildProfileReply is the single point that translates a
// *ChainSecurityProfile into the wire-stable ProfileReply shape.
func buildProfileReply(p *consensusconfig.ChainSecurityProfile) ProfileReply {
	hashHex := "0x" + hex.EncodeToString(p.ProfileHash[:])
	return ProfileReply{
		ProfileID:               p.ProfileID,
		ProfileName:             p.ProfileName,
		ProfileHash:             hashHex,
		PostQuantumEndToEnd:     isPostQuantumEndToEnd(p),
		NISTFriendly:            p.HashSuiteID == consensusconfig.HashSuiteSHA3NIST,
		LuxCanonical:            isLuxCanonical(p),
		HashSuite:               hashSuiteName(p.HashSuiteID),
		WalletScheme:            renderName(p.WalletSchemeID.String()),
		TxScheme:                renderName(p.TxSchemeID.String()),
		ContractAuth:            contractAuthName(p),
		ValidatorScheme:         validatorSchemeName(p),
		FinalityScheme:          renderName(p.FinalitySchemeID.String()),
		HighValueScheme:         renderName(p.HighValueSchemeID.String()),
		ProofPolicy:             renderName(p.ProofPolicyID.String()),
		KeyExchange:             renderName(p.KeyExchangeID.String()),
		HighValueKEM:            renderName(p.HighValueKEM.String()),
		RecoveryScheme:          renderName(p.RecoverySchemeID.String()),
		ForbidECDSAWallets:      p.ForbidECDSAWallets,
		ForbidECDSAContractAuth: p.ForbidECDSAContractAuth,
		ForbidBLSContractAuth:   p.ForbidBLSContractAuth,
		ForbidClassicalKEM:      p.ForbidClassicalKEM,
		RequireTypedTxAuth:      p.RequireTypedTxAuth,
		ForbidPairings:          p.ForbidPairings,
		ForbidKZG:               p.ForbidKZG,
		ForbidTrustedSetup:      p.ForbidTrustedSetup,
		ForbidClassicalSNARKs:   p.ForbidClassicalSNARKs,
		ForbidDevProofs:         p.ForbidDevProofs,
		ForbidFallbacks:         p.ForbidFallbacks,
	}
}

// buildBlockSecurityReply is the single point that translates a
// *ChainSecurityProfile into the per-block envelope shape.
func buildBlockSecurityReply(p *consensusconfig.ChainSecurityProfile) BlockSecurityReply {
	var backendID consensusconfig.ProofBackendID
	if len(p.AllowedProofBackends) > 0 {
		backendID = p.AllowedProofBackends[0]
	}
	return BlockSecurityReply{
		SecurityProfileID:     p.ProfileID,
		SecurityProfileName:   p.ProfileName,
		PulsarMSignatureValid: p.FinalitySchemeID.IsPulsarM(),
		ProofBackendID:        uint32(backendID),
		ProofBackendName:      proofBackendName(backendID),
		PostQuantumEndToEnd:   isPostQuantumEndToEnd(p),
	}
}
