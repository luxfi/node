// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"github.com/luxdefi/node/vms/avm/block"
	"github.com/luxdefi/node/vms/avm/fxs"
	"github.com/luxdefi/node/vms/nftfx"
	"github.com/luxdefi/node/vms/propertyfx"
	"github.com/luxdefi/node/vms/secp256k1fx"
)

const (
	SECP256K1FxIndex = 0
	NFTFxIndex       = 1
	PropertyFxIndex  = 2
)

// Parser to support serialization and deserialization
var Parser block.Parser

func init() {
	var err error
	Parser, err = block.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
		&nftfx.Fx{},
		&propertyfx.Fx{},
	})
	if err != nil {
		panic(err)
	}
}
