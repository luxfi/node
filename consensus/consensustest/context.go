// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensustest

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/api/metrics"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/db/memdb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/node/consensus/validators/validatorstest"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls/signer/localsigner"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms/platformvm/warp"
)

var (
	PChainID    = constants.PlatformChainID
	XChainID    = ids.GenerateTestID()
	CChainID    = ids.GenerateTestID()
	LUXAssetID = ids.GenerateTestID()

	errMissing = errors.New("missing")

	_ consensus.Acceptor = noOpAcceptor{}
)

type noOpAcceptor struct{}

func (noOpAcceptor) Accept(*consensus.Context, ids.ID, []byte) error {
	return nil
}

func ConsensusContext(ctx *consensus.Context) *consensus.Context {
	ctx.PrimaryAlias = ctx.ChainID.String()
	ctx.Registerer = prometheus.NewRegistry()
	ctx.BlockAcceptor = noOpAcceptor{}
	ctx.TxAcceptor = noOpAcceptor{}
	ctx.VertexAcceptor = noOpAcceptor{}
	return ctx
}

func Context(tb testing.TB, chainID ids.ID) *consensus.Context {
	require := require.New(tb)

	secretKey, err := localsigner.New()
	require.NoError(err)
	publicKey := secretKey.PublicKey()

	memory := atomic.NewMemory(memdb.New())
	sharedMemory := memory.NewSharedMemory(chainID)

	aliaser := ids.NewAliaser()
	require.NoError(aliaser.Alias(PChainID, "P"))
	require.NoError(aliaser.Alias(PChainID, PChainID.String()))
	require.NoError(aliaser.Alias(XChainID, "X"))
	require.NoError(aliaser.Alias(XChainID, XChainID.String()))
	require.NoError(aliaser.Alias(CChainID, "C"))
	require.NoError(aliaser.Alias(CChainID, CChainID.String()))

	validatorState := &validatorstest.State{
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			switch chainID {
			case PChainID, XChainID, CChainID:
				return constants.PrimaryNetworkID, nil
			default:
				return ids.Empty, errMissing
			}
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		},
	}

	return &consensus.Context{
		NetworkID:       constants.UnitTestID,
		SubnetID:        constants.PrimaryNetworkID,
		ChainID:         chainID,
		NodeID:          ids.GenerateTestNodeID(),
		PublicKey:       publicKey,
		NetworkUpgrades: upgradetest.GetConfig(upgradetest.Latest),

		XChainID:    XChainID,
		CChainID:    CChainID,
		LUXAssetID: LUXAssetID,

		Log:          logging.NoLog{},
		SharedMemory: sharedMemory,
		BCLookup:     aliaser,
		Metrics:      metrics.NewPrefixGatherer(),

		WarpSigner: warp.NewSigner(secretKey, constants.UnitTestID, chainID),

		ValidatorState: validatorState,
		ChainDataDir:   tb.TempDir(),
	}
}
