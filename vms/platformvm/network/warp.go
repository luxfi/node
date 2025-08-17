// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	// "google.golang.org/protobuf/proto" // Commented out until L1 validators are implemented

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/common"
	"github.com/luxfi/node/network/p2p/lp118"
	"github.com/luxfi/node/proto/pb/platformvm"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/platformvm/warp/payload"
)

const (
	ErrFailedToParseWarpAddressedCall = iota + 1
	ErrWarpAddressedCallHasSourceAddress
	ErrFailedToParseWarpAddressedCallPayload
	ErrUnsupportedWarpAddressedCallPayloadType

	ErrFailedToParseJustification
	ErrConversionDoesNotExist
	ErrMismatchedConversionID

	ErrInvalidJustificationType
	ErrFailedToParseSubnetID
	ErrMismatchedValidationID
	ErrValidationDoesNotExist
	ErrValidationExists
	ErrFailedToParseRegisterL1Validator
	ErrValidationCouldBeRegistered

	ErrImpossibleNonce
	ErrWrongNonce
	ErrWrongWeight
)

// L1ValidatorInfo contains the minimal fields needed from state.L1Validator
type L1ValidatorInfo interface {
	GetMinNonce() uint64
	GetWeight() uint64
}

// StateReader defines the minimal interface needed from state.Chain
// to avoid import cycles
type StateReader interface {
	GetL1Validator(validationID ids.ID) (L1ValidatorInfo, error)
	GetTimestamp() time.Time
	// GetSubnetToL1Conversion(subnetID ids.ID) (interface{}, error) // Uncommented when needed
	// HasExpiry(entry interface{}) (bool, error) // Uncommented when needed
}

var _ lp118.Verifier = (*signatureRequestVerifier)(nil)

type signatureRequestVerifier struct {
	stateLock sync.Locker
	state     StateReader
}

func (s signatureRequestVerifier) Verify(
	_ context.Context,
	unsignedMessage *warp.UnsignedMessage,
	justification []byte,
) *common.AppError {
	msg, err := payload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseWarpAddressedCall,
			Message: "failed to parse warp addressed call: " + err.Error(),
		}
	}
	if len(msg.SourceAddress) != 0 {
		return &common.AppError{
			Code:    ErrWarpAddressedCallHasSourceAddress,
			Message: "source address should be empty",
		}
	}

	payloadIntf, err := message.Parse(msg.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseWarpAddressedCallPayload,
			Message: "failed to parse warp addressed call payload: " + err.Error(),
		}
	}

	switch payload := payloadIntf.(type) {
	case *message.SubnetToL1Conversion:
		return s.verifySubnetToL1Conversion(payload, justification)
	case *message.L1ValidatorRegistration:
		return s.verifyL1ValidatorRegistration(payload, justification)
	case *message.L1ValidatorWeight:
		return s.verifyL1ValidatorWeight(payload)
	default:
		return &common.AppError{
			Code:    ErrUnsupportedWarpAddressedCallPayloadType,
			Message: fmt.Sprintf("unsupported warp addressed call payload type: %T", payloadIntf),
		}
	}
}

func (s signatureRequestVerifier) verifySubnetToL1Conversion(
	msg *message.SubnetToL1Conversion,
	justification []byte,
) *common.AppError {
	// Parse the justification as subnetID
	_, err := ids.ToID(justification) // subnetID will be used when L1 conversion is implemented
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseJustification,
			Message: "failed to parse justification: " + err.Error(),
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// conversion, err := s.state.GetSubnetToL1Conversion(subnetID)
	// if err == database.ErrNotFound {
	// 	return &common.AppError{
	// 		Code:    ErrConversionDoesNotExist,
	// 		Message: fmt.Sprintf("subnet %q has not been converted", subnetID),
	// 	}
	// }
	// if err != nil {
	// 	return &common.AppError{
	// 		Code:    common.ErrUndefined.Code,
	// 		Message: "failed to get subnet conversionID: " + err.Error(),
	// 	}
	// }

	// if msg.ID != conversion.ConversionID {
	// 	return &common.AppError{
	// 		Code:    ErrMismatchedConversionID,
	// 		Message: fmt.Sprintf("provided conversionID %q != expected conversionID %q", msg.ID, conversion.ConversionID),
	// 	}
	// }

	// Temporary placeholder until L1 conversion is implemented
	return nil
}

func (s signatureRequestVerifier) verifyL1ValidatorRegistration(
	msg *message.L1ValidatorRegistration,
	justificationBytes []byte,
) *common.AppError {
	if msg.Registered {
		return s.verifyL1ValidatorRegistered(msg.ValidationID)
	}

	// var justification platformvm.L1ValidatorRegistrationJustification
	// if err := proto.Unmarshal(justificationBytes, &justification); err != nil {
	// 	return &common.AppError{
	// 		Code:    ErrFailedToParseJustification,
	// 		Message: "failed to parse justification: " + err.Error(),
	// 	}
	// }

	// switch preimage := justification.GetPreimage().(type) {
	// case *platformvm.L1ValidatorRegistrationJustification_ConvertSubnetToL1TxData:
	// 	return s.verifySubnetValidatorNotCurrentlyRegistered(msg.ValidationID, preimage.ConvertSubnetToL1TxData)
	// case *platformvm.L1ValidatorRegistrationJustification_RegisterL1ValidatorMessage:
	// 	return s.verifySubnetValidatorCanNotValidate(msg.ValidationID, preimage.RegisterL1ValidatorMessage)
	// default:
	// 	return &common.AppError{
	// 		Code:    ErrInvalidJustificationType,
	// 		Message: fmt.Sprintf("invalid justification type: %T", justification.Preimage),
	// 	}
	// }

	// Temporary placeholder until L1 validation is implemented
	return nil
}

// verifyL1ValidatorRegistered verifies that the validationID is currently a
// validator.
func (s signatureRequestVerifier) verifyL1ValidatorRegistered(
	validationID ids.ID,
) *common.AppError {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// Verify that the validator exists
	_, err := s.state.GetL1Validator(validationID)
	if err == database.ErrNotFound {
		return &common.AppError{
			Code:    ErrValidationDoesNotExist,
			Message: fmt.Sprintf("validation %q does not exist", validationID),
		}
	}
	if err != nil {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get L1 validator: " + err.Error(),
		}
	}
	return nil
}

// verifySubnetValidatorNotCurrentlyRegistered verifies that the validationID
// could only correspond to a validator from a ConvertSubnetToL1Tx and that it
// is not currently a validator.
func (s signatureRequestVerifier) verifySubnetValidatorNotCurrentlyRegistered(
	validationID ids.ID,
	justification *platformvm.SubnetIDIndex,
) *common.AppError {
	// subnetID, err := ids.ToID(justification.GetSubnetId())
	// if err != nil {
	// 	return &common.AppError{
	// 		Code:    ErrFailedToParseSubnetID,
	// 		Message: "failed to parse subnetID: " + err.Error(),
	// 	}
	// }

	// justificationID := subnetID.Append(justification.GetIndex())
	// if validationID != justificationID {
	// 	return &common.AppError{
	// 		Code:    ErrMismatchedValidationID,
	// 		Message: fmt.Sprintf("validationID %q != justificationID %q", validationID, justificationID),
	// 	}
	// }

	// s.stateLock.Lock()
	// defer s.stateLock.Unlock()

	// // Verify that the provided subnetID has been converted.
	// _, err = s.state.GetSubnetToL1Conversion(subnetID)
	// if err == database.ErrNotFound {
	// 	return &common.AppError{
	// 		Code:    ErrConversionDoesNotExist,
	// 		Message: fmt.Sprintf("subnet %q has not been converted", subnetID),
	// 	}
	// }
	// if err != nil {
	// 	return &common.AppError{
	// 		Code:    common.ErrUndefined.Code,
	// 		Message: "failed to get subnet conversionID: " + err.Error(),
	// 	}
	// }

	// // Verify that the validator is not in the current state
	// _, err = s.state.GetL1Validator(validationID)
	// if err == nil {
	// 	return &common.AppError{
	// 		Code:    ErrValidationExists,
	// 		Message: fmt.Sprintf("validation %q exists", validationID),
	// 	}
	// }
	// if err != database.ErrNotFound {
	// 	return &common.AppError{
	// 		Code:    common.ErrUndefined.Code,
	// 		Message: "failed to lookup L1 validator: " + err.Error(),
	// 	}
	// }

	// Either the validator was removed or it was never registered as part of
	// the subnet conversion.
	return nil
}

// verifySubnetValidatorCanNotValidate verifies that the validationID is not
// currently and can never become a validator.
func (s signatureRequestVerifier) verifySubnetValidatorCanNotValidate(
	validationID ids.ID,
	justificationBytes []byte,
) *common.AppError {
	justification, err := message.ParseRegisterL1Validator(justificationBytes)
	if err != nil {
		return &common.AppError{
			Code:    ErrFailedToParseRegisterL1Validator,
			Message: "failed to parse RegisterL1Validator justification: " + err.Error(),
		}
	}

	justificationID := justification.ValidationID()
	if validationID != justificationID {
		return &common.AppError{
			Code:    ErrMismatchedValidationID,
			Message: fmt.Sprintf("validationID %q != justificationID %q", validationID, justificationID),
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	// Verify that the validator does not currently exist
	_, err = s.state.GetL1Validator(validationID)
	if err == nil {
		return &common.AppError{
			Code:    ErrValidationExists,
			Message: fmt.Sprintf("validation %q exists", validationID),
		}
	}
	if err != database.ErrNotFound {
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to lookup L1 validator: " + err.Error(),
		}
	}

	currentTimeUnix := uint64(s.state.GetTimestamp().Unix())
	if justification.Expiry <= currentTimeUnix {
		return nil // The expiry time has passed
	}

	// // If the validation ID was successfully registered and then removed, it can
	// // never be re-used again even if its expiry has not yet passed.
	// hasExpiry, err := s.state.HasExpiry(state.ExpiryEntry{
	// 	Timestamp:    justification.Expiry,
	// 	ValidationID: validationID,
	// })
	// if err != nil {
	// 	return &common.AppError{
	// 		Code:    common.ErrUndefined.Code,
	// 		Message: "failed to lookup expiry: " + err.Error(),
	// 	}
	// }
	// if !hasExpiry {
	// 	return &common.AppError{
	// 		Code:    ErrValidationCouldBeRegistered,
	// 		Message: fmt.Sprintf("validation %q can be registered until %d", validationID, justification.Expiry),
	// 	}
	// }

	return nil // The validator has been removed
}

func (s signatureRequestVerifier) verifyL1ValidatorWeight(
	msg *message.L1ValidatorWeight,
) *common.AppError {
	if msg.Nonce == math.MaxUint64 {
		return &common.AppError{
			Code:    ErrImpossibleNonce,
			Message: "impossible nonce",
		}
	}

	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	l1Validator, err := s.state.GetL1Validator(msg.ValidationID)
	switch {
	case err == database.ErrNotFound:
		// If the peer is attempting to verify that the weight of the validator
		// is 0, they should be requesting a [message.L1ValidatorRegistration]
		// with Registered set to false.
		return &common.AppError{
			Code:    ErrValidationDoesNotExist,
			Message: fmt.Sprintf("validation %q does not exist", msg.ValidationID),
		}
	case err != nil:
		return &common.AppError{
			Code:    common.ErrUndefined.Code,
			Message: "failed to get L1 validator: " + err.Error(),
		}
	case msg.Nonce+1 != l1Validator.GetMinNonce():
		return &common.AppError{
			Code:    ErrWrongNonce,
			Message: fmt.Sprintf("provided nonce %d != expected nonce (%d - 1)", msg.Nonce, l1Validator.GetMinNonce()),
		}
	case msg.Weight != l1Validator.GetWeight():
		return &common.AppError{
			Code:    ErrWrongWeight,
			Message: fmt.Sprintf("provided weight %d != expected weight %d", msg.Weight, l1Validator.GetWeight()),
		}
	default:
		return nil // The nonce and weight are correct
	}
}
