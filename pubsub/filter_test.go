// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pubsub

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/address"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/node/pubsub/bloom"
)

func TestAddAddressesParseAddresses(t *testing.T) {
	require := require.New(t)

	chainAlias := "X"
	hrp := constants.GetHRP(5)

	addrID := ids.ShortID{1}
	addrStr, err := address.Format(chainAlias, hrp, addrID[:])
	require.NoError(err)

	msg := &AddAddresses{JSONAddresses: apitypes.JSONAddresses{
		Addresses: []string{
			addrStr,
		},
	}}

	require.NoError(msg.parseAddresses())

	require.Len(msg.addressIds, 1)
	require.Equal(addrID[:], msg.addressIds[0])
}

func TestFilterParamUpdateMulti(t *testing.T) {
	require := require.New(t)

	fp := NewFilterParam()

	addr1 := []byte("abc")
	addr2 := []byte("def")
	addr3 := []byte("xyz")

	require.NoError(fp.Add(addr1, addr2, addr3))
	require.Len(fp.set, 3)
	require.Contains(fp.set, string(addr1))
	require.Contains(fp.set, string(addr2))
	require.Contains(fp.set, string(addr3))
}

func TestFilterParam(t *testing.T) {
	require := require.New(t)

	mapFilter := bloom.NewMap()

	fp := NewFilterParam()
	fp.SetFilter(mapFilter)

	addr := ids.GenerateTestShortID()
	require.NoError(fp.Add(addr[:]))
	require.True(fp.Check(addr[:]))
	delete(fp.set, string(addr[:]))

	mapFilter.Add(addr[:])
	require.True(fp.Check(addr[:]))
	require.False(fp.Check([]byte("bye")))
}

func TestNewBloom(t *testing.T) {
	cm := &NewBloom{}
	require.False(t, cm.IsParamsValid())
}

// TestNewBloomIsParamsValidRejectsUnrepresentableMaxElements pins the guard on
// the wire value that connection.handleNewBloom narrows to an int. int(2^64-1)
// is -1, which drives bloom.OptimalEntries to its minimum and yields an 8-bit
// filter that reports a match for nearly every key.
func TestNewBloomIsParamsValidRejectsUnrepresentableMaxElements(t *testing.T) {
	require := require.New(t)

	require.True((&NewBloom{MaxElements: 1000, CollisionProb: 0.1}).IsParamsValid())
	require.False((&NewBloom{MaxElements: 0, CollisionProb: 0.1}).IsParamsValid())
	require.False((&NewBloom{MaxElements: math.MaxUint64, CollisionProb: 0.1}).IsParamsValid())
	require.False((&NewBloom{MaxElements: math.MaxInt64 + 1, CollisionProb: 0.1}).IsParamsValid())
}
