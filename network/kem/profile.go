// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"fmt"
	"strings"
)

// ActiveProfile is the small subset of strict-PQ profile fields the
// network layer needs at boot to log the active posture and refuse
// peers that disagree. The full ChainSecurityProfile lives in
// consensus/config and is the authoritative source; this struct is the
// network-layer projection of that profile.
type ActiveProfile struct {
	// Name is the canonical profile name for log lines and audit reports.
	// Production strict-PQ deployments use "STRICT_E2E_PQ".
	Name string

	// NodeIdentity is the wire name of the per-node identity signature
	// scheme. Strict-PQ: "ML-DSA-65".
	NodeIdentity string

	// SessionKEM is the KEM byte for peer-to-peer session keys. Strict-PQ
	// default: KeyExchangeMLKEM768.
	SessionKEM KeyExchangeID

	// DKGKEM is the KEM byte for validator-to-validator DKG channels.
	// Strict-PQ mandates KeyExchangeMLKEM1024.
	DKGKEM KeyExchangeID

	// HashSuite is the wire name of the transcript / commitment hash
	// family. Strict-PQ: "SHA3_NIST".
	HashSuite string

	// PostQuantumE2E reports whether the profile asserts end-to-end PQ
	// (KEM, identity, threshold finality all PQ).
	PostQuantumE2E bool

	// ForbidClassicalKEM is the strict-PQ refusal switch: when true, any
	// peer offering an IsForbiddenInPQMode() KEM is refused at handshake.
	ForbidClassicalKEM bool
}

// StrictE2EPQ returns the canonical strict-PQ profile projection
// for the network layer. Mirrors the consensus-config profile of the
// same shape; values here are pinned so a node that boots without an
// explicit profile still advertises the strict posture by default.
func StrictE2EPQ() ActiveProfile {
	return ActiveProfile{
		Name:               "STRICT_E2E_PQ",
		NodeIdentity:       "ML-DSA-65",
		SessionKEM:         KeyExchangeMLKEM768,
		DKGKEM:             KeyExchangeMLKEM1024,
		HashSuite:          "SHA3_NIST",
		PostQuantumE2E:     true,
		ForbidClassicalKEM: true,
	}
}

// Banner returns the multi-line operator-readable banner that node
// startup MUST emit on boot under a strict-PQ profile. Fixed format so
// log scrapers can match deterministically.
//
//	SECURITY PROFILE: STRICT_E2E_PQ
//	NODE IDENTITY:    ML-DSA-65
//	SESSION KEM:      ML-KEM-768
//	DKG KEM:          ML-KEM-1024
//	HASH SUITE:       SHA3_NIST
//	POST-QUANTUM E2E: true
func (p ActiveProfile) Banner() string {
	var b strings.Builder
	fmt.Fprintf(&b, "SECURITY PROFILE: %s\n", p.Name)
	fmt.Fprintf(&b, "NODE IDENTITY:    %s\n", p.NodeIdentity)
	fmt.Fprintf(&b, "SESSION KEM:      %s\n", strings.ToUpper(p.SessionKEM.String()))
	fmt.Fprintf(&b, "DKG KEM:          %s\n", strings.ToUpper(p.DKGKEM.String()))
	fmt.Fprintf(&b, "HASH SUITE:       %s\n", p.HashSuite)
	fmt.Fprintf(&b, "POST-QUANTUM E2E: %t\n", p.PostQuantumE2E)
	return b.String()
}

// Validate refuses an ActiveProfile whose axes are internally inconsistent
// before the network layer trusts it. Mirrors the strict-PQ portion of
// ChainSecurityProfile.Validate.
func (p ActiveProfile) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("kem: ActiveProfile.Name is empty")
	}
	if !p.SessionKEM.IsPostQuantum() {
		return fmt.Errorf("kem: ActiveProfile.SessionKEM=%s is not post-quantum", p.SessionKEM)
	}
	if p.DKGKEM != KeyExchangeMLKEM1024 {
		return fmt.Errorf("kem: ActiveProfile.DKGKEM=%s, expected %s",
			p.DKGKEM, KeyExchangeMLKEM1024)
	}
	if !p.PostQuantumE2E {
		return fmt.Errorf("kem: ActiveProfile.PostQuantumE2E must be true on strict-PQ")
	}
	if !p.ForbidClassicalKEM {
		return fmt.Errorf("kem: ActiveProfile.ForbidClassicalKEM must be true on strict-PQ")
	}
	return nil
}
