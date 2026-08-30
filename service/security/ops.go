// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The security posture this chain runs under.
//
// Both operations are reads of an immutable value — the profile is resolved
// once at boot and never mutated — so both are GETs, which is also the whole of
// their authorization: the node's rule answers a read to anyone. That is what
// this namespace has always done, and converting it changes nothing about who
// can reach it. It is deliberately ungated: a chain's posture is what a wallet
// checks BEFORE it trusts the chain, so a node that would not state it is a node
// nobody can verify.
//
// THE FIELD NAMES ARE snake_case AND STAY THAT WAY. Every other namespace on
// this node spells them camelCase, and this one does not, because audit
// pipelines, wallet posture banners and block explorers already grep on
// `profile_id` and `post_quantum_end_to_end`. The wire is what it is; renaming
// 28 fields to look tidy would break every one of those readers for nothing.

package security

import (
	"context"

	"github.com/luxfi/log"
	"github.com/zap-proto/zip"

	avajson "github.com/luxfi/node/utils/json"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// Ops is this service's typed operations. The paths are relative to where the
// app is mounted, which the node decides — a service does not name its own
// address.
func (s *Service) Ops() *zip.App {
	app := zip.New(zip.Config{
		AppName:               "security",
		Logger:                s.log,
		DisableStartupMessage: true,
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux node security",
			Description: "The chain-wide security profile a Lux node runs under: which signature, KEM and proof schemes it accepts, and which it forbids.",
		},
	})
	zip.Get(app, "/profile", s.securityProfile)
	zip.Get(app, "/block/profile", s.blockSecurity)
	return app
}

// BlockSecurityArgs asks about the posture in force at one block.
type BlockSecurityArgs struct {
	// BlockNumber is the explorer-visible block height. The profile is pinned at
	// genesis so today's answer is the same at every height; the argument is
	// carried so per-block detail can be added without a new namespace.
	BlockNumber avajson.Uint64 `json:"blockNumber"`
	// Chain is the chain alias ("P", "X", "C") the block belongs to. Reserved
	// for the same reason.
	Chain string `json:"chain,omitempty"`
}

// SecurityProfile is the chain-wide profile this node enforces: the scheme on
// every axis, and every primitive it forbids. The shape is fixed for a given
// (profile_id, profile_hash) pair, so a caller can cache by hash and refetch
// only when the hash changes.
//
// Response: {"profile_id": 2, "profile_name": "STRICT_PQ", "profile_hash": "0x00", "post_quantum_end_to_end": true, "nist_friendly": true, "lux_canonical": true, "hash_suite": "SHA3_NIST", "wallet_scheme": "ML_DSA_65", "tx_scheme": "ML_DSA_65", "contract_auth": "ML_DSA_65", "validator_scheme": "ML_DSA_65", "finality_scheme": "PULSAR_M", "high_value_scheme": "SLH_DSA", "proof_policy": "PQ_ONLY", "key_exchange": "ML_KEM_768", "high_value_kem": "ML_KEM_1024", "recovery_scheme": "SLH_DSA", "forbid_ecdsa_wallets": true, "forbid_ecdsa_contract_auth": true, "forbid_bls_contract_auth": true, "forbid_classical_kem": true, "require_typed_tx_auth": true, "forbid_pairings": true, "forbid_kzg": true, "forbid_trusted_setup": true, "forbid_classical_snarks": true, "forbid_dev_proofs": true, "forbid_fallbacks": true}
func (s *Service) securityProfile(_ context.Context, _ *struct{}) (*ProfileReply, error) {
	s.log.Debug("API called",
		log.String("service", "security"),
		log.String("method", "securityProfile"),
	)
	if s.profile == nil {
		return nil, ErrNoProfile
	}
	reply := buildProfileReply(s.profile)
	return &reply, nil
}

// BlockSecurity is the security envelope in force at a block: which profile
// applies, whether its finality signature is Pulsar-M, and which proof backend
// is allowed. A chain that accepts several backends states only the first here;
// the exact backend a block used is in that block's own certificate.
//
// Example: {"blockNumber": "1", "chain": "P"}
// Response: {"security_profile_id": 2, "security_profile_name": "STRICT_PQ", "pulsarm_signature_valid": true, "proof_backend_id": 1, "proof_backend_name": "PLONKY3", "post_quantum_end_to_end": true}
func (s *Service) blockSecurity(_ context.Context, _ *BlockSecurityArgs) (*BlockSecurityReply, error) {
	s.log.Debug("API called",
		log.String("service", "security"),
		log.String("method", "blockSecurity"),
	)
	if s.profile == nil {
		return nil, ErrNoProfile
	}
	reply := buildBlockSecurityReply(s.profile)
	return &reply, nil
}
