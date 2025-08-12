package platformvm

// Platform VM protobuf definitions
type PlatformVM struct {
	// Add protobuf-generated fields as needed
}

// L1ValidatorRegistrationJustification represents justification for L1 validator registration
type L1ValidatorRegistrationJustification struct {
	TypeId   uint32
	Preimage interface{}
}

// ProtoMessage marks this as a protobuf message
func (j *L1ValidatorRegistrationJustification) ProtoMessage() {}

// Reset stub for proto.Message interface
func (j *L1ValidatorRegistrationJustification) Reset() {}

// String stub for proto.Message interface  
func (j *L1ValidatorRegistrationJustification) String() string { return "" }

// Justification type wrappers
type L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData struct {
	ConvertSubnetToL1TxData *SubnetIDIndex
}

type L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage struct {
	RegisterL1ValidatorMessage []byte
}

// SubnetIDIndex represents the index of a subnet ID
type SubnetIDIndex struct {
	SubnetID []byte `json:"subnetId"`
	Index    uint32 `json:"index"`
}