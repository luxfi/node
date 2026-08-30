// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/formatting"

	avajson "github.com/luxfi/node/utils/json"
)

// buildGenesis's argument used to be a map of maps, so it had no layout and no
// schema. It is now a list and a struct, and the request it accepts is the same
// document it has always accepted — which is what this holds it to.
//
// The `initialState` object is the interesting half: its keys were never data.
// Two are the vocabulary and everything else has always been refused, so it is
// a struct, and the refusal moved to where a malformed request belongs.

const genesisRequest = `{` +
	`"networkID":"12345",` +
	`"genesisData":{` +
	`"asset1":{"name":"myFixedCapAsset","symbol":"MFCA","denomination":"8","initialState":{"fixedCap":[{"amount":"100000","address":"X-testing1lnk637g0edwnqc2tn8tel39652fswa3xk4r65e"}]},"memo":""},` +
	`"asset2":{"name":"myVarCapAsset","symbol":"MVCA","denomination":"0","initialState":{"variableCap":[{"threshold":"1","minters":["X-testing1lnk637g0edwnqc2tn8tel39652fswa3xk4r65e"]}]},"memo":""}` +
	`},` +
	`"encoding":"hex"}`

func TestBuildGenesisArgsWire(t *testing.T) {
	require := require.New(t)

	var args BuildGenesisArgs
	require.NoError(json.Unmarshal([]byte(genesisRequest), &args))

	// The alias is the object's key on the wire and lives inside the entry
	// here, in the key's own order.
	require.Len(args.GenesisData, 2)
	require.Equal("asset1", args.GenesisData[0].Alias)
	require.Equal("asset2", args.GenesisData[1].Alias)
	require.Len(args.GenesisData[0].InitialState.FixedCap, 1)
	require.Equal(avajson.Uint64(100000), args.GenesisData[0].InitialState.FixedCap[0].Amount)
	require.Len(args.GenesisData[1].InitialState.VariableCap, 1)
	require.Equal(formatting.Hex, args.Encoding)

	again, err := json.Marshal(args)
	require.NoError(err)
	require.Equal(genesisRequest, string(again), "the request document must survive the round trip")
}

// A key outside the vocabulary is refused rather than ignored. Ignoring it
// would build a genesis without the state that was asked for and say nothing.
func TestBuildGenesisRefusesUnknownState(t *testing.T) {
	require := require.New(t)

	var args BuildGenesisArgs
	err := json.Unmarshal([]byte(`{"genesisData":{"a":{"initialState":{"nft":[]}}}}`), &args)
	require.ErrorIs(err, errUnknownAssetType)
}
