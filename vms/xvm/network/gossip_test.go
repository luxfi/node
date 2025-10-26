<<<<<<< HEAD:vms/avm/network/gossip_test.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/network/gossip_test.go
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

<<<<<<< HEAD:vms/avm/network/gossip_test.go
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms/avm/fxs"
	"github.com/luxfi/node/vms/avm/txs"
	"github.com/luxfi/node/vms/avm/txs/mempool"
=======
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"

	"github.com/luxfi/consensus/core"

	"github.com/luxfi/node/vms/xvm/fxs"

	"github.com/luxfi/node/vms/xvm/txs"

	"github.com/luxfi/node/vms/xvm/txs/mempool"

>>>>>>> origin/regenesis-runtime-replay:vms/xvm/network/gossip_test.go
	"github.com/luxfi/node/vms/components/lux"

	"github.com/luxfi/node/vms/secp256k1fx"
)

var _ TxVerifier = (*testVerifier)(nil)

type testVerifier struct {
	err error
}

func (v testVerifier) VerifyTx(*txs.Tx) error {
	return v.err
}

func TestMarshaller(t *testing.T) {
	require := require.New(t)

	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(err)

	marhsaller := txParser{
		parser: parser,
	}

	want := &txs.Tx{Unsigned: &txs.BaseTx{}}
	require.NoError(want.Initialize(parser.Codec()))

	bytes, err := marhsaller.MarshalGossip(want)
	require.NoError(err)

	got, err := marhsaller.UnmarshalGossip(bytes)
	require.NoError(err)
	require.Equal(want.GossipID(), got.GossipID())
}

func TestGossipMempoolAdd(t *testing.T) {
	require := require.New(t)

<<<<<<< HEAD:vms/avm/network/gossip_test.go
	metrics := prometheus.NewRegistry()
=======
	metrics := metric.NewNoOp().Registry()
	toEngine := make(chan core.MessageType, 1)
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/network/gossip_test.go

	baseMempool, err := mempool.New("", metrics)
	require.NoError(err)

	mempool, err := newGossipMempool(
		baseMempool,
		metrics,
		nil,
		testVerifier{},
		DefaultConfig.ExpectedBloomFilterElements,
		DefaultConfig.ExpectedBloomFilterFalsePositiveProbability,
		DefaultConfig.MaxBloomFilterFalsePositiveProbability,
	)
	require.NoError(err)

	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: lux.BaseTx{
				Ins: []*lux.TransferableInput{},
			},
		},
		TxID: ids.GenerateTestID(),
	}

	require.NoError(mempool.Add(tx))
	require.True(mempool.bloom.Has(tx))
}

func TestGossipMempoolAddVerified(t *testing.T) {
	require := require.New(t)

<<<<<<< HEAD:vms/avm/network/gossip_test.go
	metrics := prometheus.NewRegistry()
=======
	metrics := metric.NewNoOp().Registry()
	toEngine := make(chan core.MessageType, 1)
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/network/gossip_test.go

	baseMempool, err := mempool.New("", metrics)
	require.NoError(err)

	mempool, err := newGossipMempool(
		baseMempool,
		metrics,
		nil,
		testVerifier{
			err: errTest, // We shouldn't be attempting to verify the tx in this flow
		},
		DefaultConfig.ExpectedBloomFilterElements,
		DefaultConfig.ExpectedBloomFilterFalsePositiveProbability,
		DefaultConfig.MaxBloomFilterFalsePositiveProbability,
	)
	require.NoError(err)

	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: lux.BaseTx{
				Ins: []*lux.TransferableInput{},
			},
		},
		TxID: ids.GenerateTestID(),
	}

	require.NoError(mempool.AddWithoutVerification(tx))
	require.True(mempool.bloom.Has(tx))
}
