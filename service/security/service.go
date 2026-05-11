// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package security

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/log"
	avajson "github.com/luxfi/node/utils/json"
)

// ErrNoProfile is returned when an RPC call asks for security data but
// the node booted without a SecurityProfile pin (legacy networks). The
// error message names the missing pin so an operator who deploys a
// strict-PQ wallet against a permissive backend sees the mismatch
// immediately.
var ErrNoProfile = errors.New(
	"security: node booted without a chain-wide SecurityProfile pin " +
		"(genesis carries no SecurityProfile{} block); RPC unavailable")

// Service is the JSON-RPC handler set registered under the lux
// namespace at /ext/lux. Methods are read-only; the underlying profile
// pointer is set once at construction and never mutated.
//
// Exposed methods:
//
//	lux_securityProfile  → ProfileReply
//	lux_blockSecurity    → BlockSecurityReply
//
// The "block" method returns the chain-wide envelope; per-block
// auth-scheme details remain in the VM's own block JSON (the VMs do
// not embed a single canonical block-level scheme byte today, so the
// summary is the most honest thing we can advertise).
type Service struct {
	log     log.Logger
	profile *consensusconfig.ChainSecurityProfile
}

// NewHandler constructs an http.Handler for the lux JSON-RPC namespace.
// Profile may be nil — see ErrNoProfile.
//
// The returned handler is suitable for APIServer.AddRoute(handler,
// "lux", "") so it lands at /ext/lux on the node's HTTP listener.
func NewHandler(logger log.Logger, profile *consensusconfig.ChainSecurityProfile) (http.Handler, error) {
	server := rpc.NewServer()
	codec := avajson.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")
	svc := &Service{log: logger, profile: profile}
	if err := server.RegisterService(svc, "lux"); err != nil {
		return nil, fmt.Errorf("security: RegisterService(\"lux\"): %w", err)
	}
	// RestHandler dispatches the /v1/block/{n}/security REST sidecar
	// over the same Service receiver. JSON-RPC handles the
	// /ext/lux POST surface; the REST handler delegates to the same
	// underlying methods so the two endpoints stay byte-identical
	// in semantics (one and only one way to compute the shape).
	mux := http.NewServeMux()
	mux.Handle("/", server)
	mux.HandleFunc("/v1/security/profile", svc.restProfile)
	mux.HandleFunc("/v1/security/block/", svc.restBlockSecurity)
	return mux, nil
}

// SecurityProfile is the lux_securityProfile JSON-RPC handler.
//
// Returns ErrNoProfile when the node booted without a pin. A populated
// reply names every field in ProfileReply; the entire shape is
// deterministic for a given (ProfileID, ProfileHash) pair, so a dApp
// can cache by hash and refresh only on hash change.
func (s *Service) SecurityProfile(_ *http.Request, _ *struct{}, reply *ProfileReply) error {
	s.log.Debug("API called",
		log.String("service", "lux"),
		log.String("method", "securityProfile"),
	)
	if s.profile == nil {
		return ErrNoProfile
	}
	*reply = buildProfileReply(s.profile)
	return nil
}

// BlockSecurityArgs is the JSON-RPC argument struct for
// lux_blockSecurity. The block-number argument is forward-compatible
// even though today the reply is chain-wide-constant; explorers wire
// the parameter so the API can evolve without bumping the namespace.
type BlockSecurityArgs struct {
	// BlockNumber is the explorer-visible block height. Unused by
	// the current implementation but reserved so the RPC shape is
	// stable when per-block details are added.
	BlockNumber avajson.Uint64 `json:"blockNumber"`
	// Chain is the chain alias ("P", "X", "C") the block belongs to.
	// Reserved for the same reason.
	Chain string `json:"chain,omitempty"`
}

// BlockSecurity is the lux_blockSecurity JSON-RPC handler. Returns the
// chain-wide security envelope for the named block.
//
// Today the reply is constant for a given chain (the chain-wide
// profile applies to every block since the locked-profile is pinned
// at genesis). Per-block backend variance is not part of the chain's
// posture; if a chain accepts multiple backends, explorers fetch the
// cert envelope directly for the exact backend used.
func (s *Service) BlockSecurity(_ *http.Request, _ *BlockSecurityArgs, reply *BlockSecurityReply) error {
	s.log.Debug("API called",
		log.String("service", "lux"),
		log.String("method", "blockSecurity"),
	)
	if s.profile == nil {
		return ErrNoProfile
	}
	*reply = buildBlockSecurityReply(s.profile)
	return nil
}

// restProfile is the GET /v1/security/profile sidecar. Same body as
// lux_securityProfile; useful for explorers that don't speak
// JSON-RPC. Refuses every non-GET method so callers can't smuggle
// state through the read-only endpoint.
func (s *Service) restProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.profile == nil {
		http.Error(w, ErrNoProfile.Error(), http.StatusServiceUnavailable)
		return
	}
	reply := buildProfileReply(s.profile)
	writeJSON(w, reply)
}

// restBlockSecurity is the GET /v1/security/block/{n} sidecar. The
// path suffix is taken as the block number; chain alias defaults to
// the platform chain (callers can re-route per-chain at the
// chain-manager layer if needed).
func (s *Service) restBlockSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.profile == nil {
		http.Error(w, ErrNoProfile.Error(), http.StatusServiceUnavailable)
		return
	}
	reply := buildBlockSecurityReply(s.profile)
	writeJSON(w, reply)
}

// buildProfileReply is the single point that translates a
// *ChainSecurityProfile into the wire-stable ProfileReply shape.
// Every RPC method and the REST sidecar route through this helper —
// there is no second copy of the translation logic.
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

// writeJSON serialises v as JSON to w with the Content-Type header
// set. Single canonical helper so the REST sidecars share encoding
// behaviour (no per-endpoint drift).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		// Body may have been partially written; nothing further to
		// do at this layer. Frameworks log encoder failures at the
		// HTTP listener boundary.
		_ = err
	}
}

