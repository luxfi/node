// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"time"

	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"

	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/upgrade"
)

type Config struct {
	Upgrades upgrade.Config

	// NetworkID is the validator-set ID the proposer windower resolves the
	// schedule under — it MUST be the SAME ID the consensus cert side uses
	// (manager.go resolves it once: PrimaryNetworkID for native chains,
	// otherwise the L1's own chainID, falling back to primary only if that set is
	// empty). CRITICAL-3: hardcoding PrimaryNetworkID here made the windower call
	// GetValidatorSet(height, PrimaryNetworkID) on a sovereign L1 (Zoo/Hanzo/Pars,
	// whose validators live under their OWN networkID == EVM chainID), get an
	// EMPTY set, and degrade to ErrAnyoneCanPropose — so single-proposer silently
	// did not hold and the L1 equivocated exactly like the unfixed C-Chain, AND
	// the windower's set diverged from the cert's set (breaking determinism). The
	// zero value (ids.Empty) IS constants.PrimaryNetworkID, so a native chain that
	// resolves to primary is unchanged.
	NetworkID ids.ID

	// Configurable minimal delay among blocks issued consecutively
	MinBlkDelay time.Duration

	// Maximal number of block indexed.
	// Zero signals all blocks are indexed.
	NumHistoricalBlocks uint64

	// Block signer
	StakingLeafSigner crypto.Signer

	// Block certificate
	StakingCertLeaf *staking.Certificate

	// Strict-PQ block signer. When StakingMLDSASigner is non-nil the proposer
	// signs post-fork blocks with ML-DSA-65 and stamps the block's proposer
	// identity as the raw ML-DSA-65 public key, so Proposer() derives via
	// DeriveMLDSA and EQUALS the strict-PQ NodeID the windower elected (the
	// validator set is ML-DSA-keyed under strict-PQ). Nil ⇒ classical TLS-leaf
	// signing above. Exactly one of the two schemes is active per chain, chosen at
	// wiring time from the resolved ChainSecurityProfile.
	StakingMLDSASigner *mldsa.PrivateKey
	StakingMLDSAPub    []byte

	// Registerer for metric metrics
	Registerer metric.Registerer

	// Automining configuration
	AutominingEnabled bool
	// AutominingInterval is the interval between automatic block production
	AutominingInterval time.Duration
}
