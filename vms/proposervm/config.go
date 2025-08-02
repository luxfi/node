// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/v2/staking"
	"github.com/luxfi/node/v2/upgrade"
)

type Config struct {
	Upgrades upgrade.Config

	// Configurable minimal delay among blocks issued consecutively
	MinBlkDelay time.Duration

	// Maximal number of block indexed.
	// Zero signals all blocks are indexed.
	NumHistoricalBlocks uint64

	// Block signer
	StakingLeafSigner crypto.Signer

	// Block certificate
	StakingCertLeaf *staking.Certificate

	// Registerer for prometheus metrics
	Registerer prometheus.Registerer
}
