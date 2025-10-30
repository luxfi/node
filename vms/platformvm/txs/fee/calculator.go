// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/luxfi/node/vms/platformvm/txs"
)

var ErrUnsupportedTx = errors.New("unsupported transaction type")

// Calculator calculates the minimum required fee, in nLUX, that an unsigned
// transaction must pay for valid inclusion into a block.
type Calculator interface {
	CalculateFee(tx txs.UnsignedTx) (uint64, error)
}

func (c *calculator) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) RegisterL1ValidatorTx(*txs.RegisterL1ValidatorTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) SetL1ValidatorWeightTx(*txs.SetL1ValidatorWeightTx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}

func (c *calculator) ConvertNetToL1Tx(*txs.ConvertNetToL1Tx) error {
	c.fee = c.staticCfg.TxFee
	return nil
}
