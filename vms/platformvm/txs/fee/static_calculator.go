// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var (
	_ Calculator  = (*staticCalculator)(nil)
	_ txs.Visitor = (*staticVisitor)(nil)
)

func NewSimpleStaticCalculator(config StaticConfig) Calculator {
	return &staticCalculator{
		config: config,
	}
}

type staticCalculator struct {
	config StaticConfig
}

func (c *staticCalculator) CalculateFee(tx txs.UnsignedTx) (uint64, error) {
	v := staticVisitor{
		config: c.config,
	}
	err := tx.Visit(&v)
	return v.fee, err
}

type staticVisitor struct {
	// inputs
	config StaticConfig

	// outputs
	fee uint64
}

func (*staticVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrUnsupportedTx
}

func (*staticVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrUnsupportedTx
}

func (c *staticVisitor) AddValidatorTx(*txs.AddValidatorTx) error {
	c.fee = c.config.AddPrimaryNetworkValidatorFee
	return nil
}

// Removed in regenesis
// func (c *staticVisitor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
// 	c.fee = c.config.AddSubnetValidatorFee
// 	return nil
// }

func (c *staticVisitor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	c.fee = c.config.AddPrimaryNetworkDelegatorFee
	return nil
}

func (c *staticVisitor) CreateChainTx(*txs.CreateChainTx) error {
	c.fee = c.config.CreateBlockchainTxFee
	return nil
}

// Removed in regenesis
// func (c *staticVisitor) CreateSubnetTx(*txs.CreateSubnetTx) error {
// 	c.fee = c.config.CreateSubnetTxFee
// 	return nil
// }

// Removed in regenesis
// func (c *staticVisitor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
// 	c.fee = c.config.TxFee
// 	return nil
// }

// Removed in regenesis
// func (c *staticVisitor) TransformSubnetTx(*txs.TransformSubnetTx) error {
// 	c.fee = c.config.TransformSubnetTxFee
// 	return nil
// }

// Removed in regenesis
// func (c *staticVisitor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
// 	c.fee = c.config.TxFee
// 	return nil
// }

func (c *staticVisitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if tx.Net != constants.PrimaryNetworkID {
		c.fee = c.config.TxFee // Use TxFee since AddSubnetValidatorFee was removed in regenesis
	} else {
		c.fee = c.config.AddPrimaryNetworkValidatorFee
	}
	return nil
}

func (c *staticVisitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if tx.Net != constants.PrimaryNetworkID {
		c.fee = c.config.TxFee // Use TxFee since AddSubnetDelegatorFee was removed in regenesis
	} else {
		c.fee = c.config.AddPrimaryNetworkDelegatorFee
	}
	return nil
}

func (c *staticVisitor) BaseTx(*txs.BaseTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) ImportTx(*txs.ImportTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) ExportTx(*txs.ExportTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) ConvertNetToL1Tx(*txs.ConvertNetToL1Tx) error {
	c.fee = c.config.TxFee // Use TxFee since TransformSubnetTxFee was removed in regenesis
	return nil
}

func (c *staticVisitor) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (c *staticVisitor) RegisterL1ValidatorTx(*txs.RegisterL1ValidatorTx) error {
	c.fee = c.config.TxFee // Use TxFee since AddSubnetValidatorFee was removed in regenesis
	return nil
}

func (c *staticVisitor) SetL1ValidatorWeightTx(*txs.SetL1ValidatorWeightTx) error {
	c.fee = c.config.TxFee
	return nil
}

func (v *staticVisitor) AddNetValidatorTx(*txs.AddNetValidatorTx) error {
	v.fee = v.config.AddNetValidatorFee
	return nil
}

func (v *staticVisitor) CreateNetTx(*txs.CreateNetTx) error {
	v.fee = v.config.CreateNetTxFee
	return nil
}

func (v *staticVisitor) RemoveNetValidatorTx(*txs.RemoveNetValidatorTx) error {
	v.fee = v.config.TxFee
	return nil
}

func (v *staticVisitor) TransformNetTx(*txs.TransformNetTx) error {
	v.fee = v.config.TransformNetTxFee
	return nil
}

func (v *staticVisitor) TransferNetOwnershipTx(*txs.TransferNetOwnershipTx) error {
	v.fee = v.config.TxFee
	return nil
}
