// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/math"
)

var ErrInsufficientFunds = errors.New("insufficient funds")

type FlowChecker struct {
	consumed, produced map[ids.ID]uint64
	errs               []error
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
	value[assetID], err = math.Add64(value[assetID], amount)
	if err != nil {
		fc.errs = append(fc.errs, err)
	}
}

func (fc *FlowChecker) Verify() error {
	if len(fc.errs) == 0 {
		for assetID, producedAssetAmount := range fc.produced {
			consumedAssetAmount := fc.consumed[assetID]
			if producedAssetAmount > consumedAssetAmount {
				fc.errs = append(fc.errs, ErrInsufficientFunds)
				break
			}
		}
	}
	return errors.Join(fc.errs...)
}
