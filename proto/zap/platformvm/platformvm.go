// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package platformvm provides Platform VM types for L1 validator operations (ZAP implementation).
package platformvm

// L1ValidatorRegistrationJustification contains justification for L1 validator registration
type L1ValidatorRegistrationJustification struct {
	preimage isL1ValidatorRegistrationJustification_Preimage
}

type isL1ValidatorRegistrationJustification_Preimage interface {
	isL1ValidatorRegistrationJustification_Preimage()
}

type L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData struct {
	ConvertNetworkToL1TxData []byte
}

type L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage struct {
	RegisterL1ValidatorMessage []byte
}

func (*L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData) isL1ValidatorRegistrationJustification_Preimage()    {}
func (*L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage) isL1ValidatorRegistrationJustification_Preimage() {}

func (m *L1ValidatorRegistrationJustification) GetPreimage() isL1ValidatorRegistrationJustification_Preimage {
	return m.preimage
}

func (m *L1ValidatorRegistrationJustification) GetConvertNetworkToL1TxData() []byte {
	if x, ok := m.preimage.(*L1ValidatorRegistrationJustification_ConvertNetworkToL1TxData); ok {
		return x.ConvertNetworkToL1TxData
	}
	return nil
}

func (m *L1ValidatorRegistrationJustification) GetRegisterL1ValidatorMessage() []byte {
	if x, ok := m.preimage.(*L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage); ok {
		return x.RegisterL1ValidatorMessage
	}
	return nil
}

func (m *L1ValidatorRegistrationJustification) Reset() {
	*m = L1ValidatorRegistrationJustification{}
}

// L1ValidatorWeightJustification contains justification for L1 validator weight updates
type L1ValidatorWeightJustification struct {
	preimage isL1ValidatorWeightJustification_Preimage
}

type isL1ValidatorWeightJustification_Preimage interface {
	isL1ValidatorWeightJustification_Preimage()
}

type L1ValidatorWeightJustification_L1ValidatorWeightMessage struct {
	L1ValidatorWeightMessage []byte
}

func (*L1ValidatorWeightJustification_L1ValidatorWeightMessage) isL1ValidatorWeightJustification_Preimage() {}

func (m *L1ValidatorWeightJustification) GetPreimage() isL1ValidatorWeightJustification_Preimage {
	return m.preimage
}

func (m *L1ValidatorWeightJustification) GetL1ValidatorWeightMessage() []byte {
	if x, ok := m.preimage.(*L1ValidatorWeightJustification_L1ValidatorWeightMessage); ok {
		return x.L1ValidatorWeightMessage
	}
	return nil
}

func (m *L1ValidatorWeightJustification) Reset() {
	*m = L1ValidatorWeightJustification{}
}

// ChainIDIndex identifies a validator by chain ID and index
type ChainIDIndex struct {
	ChainId []byte
	Index   uint32
}

func (m *ChainIDIndex) GetChainId() []byte {
	if m != nil {
		return m.ChainId
	}
	return nil
}

func (m *ChainIDIndex) GetIndex() uint32 {
	if m != nil {
		return m.Index
	}
	return 0
}
