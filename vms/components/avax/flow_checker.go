// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"errors"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/math"
	"github.com/luxfi/node/utils/wrappers"
)

var ErrInsufficientFunds = errors.New("insufficient funds")

type FlowChecker struct {
	consumed, produced map[ids.ID]uint64
	errs               wrappers.Errs
}

func NewFlowChecker() *FlowChecker {
	return &FlowChecker{
		consumed: make(map[ids.ID]uint64),
		produced: make(map[ids.ID]uint64),
	}
}

func (fc *FlowChecker) Consume(assetID ids.ID, amount uint64) {
	fc.add(fc.consumed, assetID, amount)
}

func (fc *FlowChecker) Produce(assetID ids.ID, amount uint64) {
	fc.add(fc.produced, assetID, amount)
}

func (fc *FlowChecker) add(value map[ids.ID]uint64, assetID ids.ID, amount uint64) {
	var err error
	value[assetID], err = math.Add(value[assetID], amount)
	fc.errs.Add(err)
}

func (fc *FlowChecker) Verify() error {
	if !fc.errs.Errored() {
		for assetID, producedAssetAmount := range fc.produced {
			consumedAssetAmount := fc.consumed[assetID]
			if producedAssetAmount > consumedAssetAmount {
				fc.errs.Add(ErrInsufficientFunds)
				break
			}
		}
	}
	return fc.errs.Err
}
