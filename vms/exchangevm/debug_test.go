// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package exchangevm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/node/vms/exchangevm/fxs"
	"github.com/luxfi/node/vms/exchangevm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
)

func TestDebugCodecTypes(t *testing.T) {
	require := require.New(t)

	// Create parser with secp256k1fx
	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(err)

	// Get the genesis codec
	gcm := parser.GenesisCodec()

	// Try to decode with debug output
	fmt.Printf("GenesisCodec manager: %+v\n", gcm)

	// Create a test genesis
	genesisBytes := newGenesisBytesTest(t)

	// Try to parse it
	genesis := &Genesis{}
	version, err := gcm.Unmarshal(genesisBytes, genesis)
	fmt.Printf("Unmarshal version: %d, error: %v\n", version, err)

	if err != nil {
		// Get more detail about the error
		fmt.Printf("Detailed error: %+v\n", err)
	}
}