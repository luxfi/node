// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto"
	"testing"
	"time"
	
	"github.com/luxfi/metric"

	"github.com/stretchr/testify/require"


	"github.com/luxfi/node/chain/choices"

	"github.com/luxfi/database"

	"github.com/luxfi/database/memdb"

	"github.com/luxfi/ids"

	"github.com/luxfi/node/staking"

	"github.com/luxfi/node/vms/proposervm/block"
)

func testBlockState(require *require.Assertions, bs BlockState) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	b, err := block.Build(
		parentID,
		timestamp,
		pChainHeight,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(err)

	_, _, err = bs.GetBlock(b.ID())
	require.Equal(database.ErrNotFound, err)

	_, _, err = bs.GetBlock(b.ID())
	require.Equal(database.ErrNotFound, err)

	require.NoError(bs.PutBlock(b, choices.Accepted))

	fetchedBlock, fetchedStatus, err := bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(choices.Accepted, fetchedStatus)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())

	fetchedBlock, fetchedStatus, err = bs.GetBlock(b.ID())
	require.NoError(err)
	require.Equal(choices.Accepted, fetchedStatus)
	require.Equal(b.Bytes(), fetchedBlock.Bytes())
}

func TestBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs := NewBlockState(db)

	testBlockState(a, bs)
}

func TestMeteredBlockState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	bs, err := NewMeteredBlockState(db, "", metrics.NewNoOpMetrics("test").Registry())
	a.NoError(err)

	testBlockState(a, bs)
}
