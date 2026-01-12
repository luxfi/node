// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"math/big"

	"github.com/luxfi/fhe"
)

// PrecompileContext provides context for FHE precompile execution
type PrecompileContext struct {
	Store  *CiphertextStore
	Caller [20]byte // msg.sender
}

// FHEAdd performs homomorphic addition: result = a + b
func FHEAdd(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	// Perform homomorphic addition
	result, err := ctx.Store.Evaluator().Add(ctA, ctB)
	if err != nil {
		return nil, err
	}

	// Store result and return handle
	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHESub performs homomorphic subtraction: result = a - b
func FHESub(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Sub(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEMul performs homomorphic multiplication: result = a * b
func FHEMul(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Mul(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEDiv performs homomorphic division: result = a / b
func FHEDiv(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Div(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHERem performs homomorphic remainder: result = a % b
func FHERem(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Rem(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHENeg performs homomorphic negation: result = -a
func FHENeg(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}

	ct, err := ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Neg(ct)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHELe performs less-than-or-equal comparison: result = (a <= b)
func FHELe(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Le(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHELt performs less-than comparison: result = (a < b)
func FHELt(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Lt(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEGe performs greater-than-or-equal comparison: result = (a >= b)
func FHEGe(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Ge(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEGt performs greater-than comparison: result = (a > b)
func FHEGt(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Gt(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEEq performs equality comparison: result = (a == b)
func FHEEq(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Eq(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHENe performs not-equal comparison: result = (a != b)
func FHENe(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Ne(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEMin returns the minimum of two encrypted values
func FHEMin(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Min(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEMax returns the maximum of two encrypted values
func FHEMax(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Max(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEAnd performs bitwise AND: result = a & b
func FHEAnd(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().And(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEOr performs bitwise OR: result = a | b
func FHEOr(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Or(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEXor performs bitwise XOR: result = a ^ b
func FHEXor(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handleA, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleB, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}

	ctA, err := ctx.Store.Load(handleA)
	if err != nil {
		return nil, err
	}
	ctB, err := ctx.Store.Load(handleB)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Xor(ctA, ctB)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHENot performs bitwise NOT: result = ~a
func FHENot(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}

	ct, err := ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Not(ct)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEShl performs left shift: result = a << bits
func FHEShl(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	bits, err := ParseUint64(input, 32)
	if err != nil {
		return nil, err
	}

	ct, err := ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Shl(ct, int(bits))
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHEShr performs right shift: result = a >> bits
func FHEShr(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	bits, err := ParseUint64(input, 32)
	if err != nil {
		return nil, err
	}

	ct, err := ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Shr(ct, int(bits))
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// FHERotl performs left rotation: result = rotl(a, bits)
// NOTE: Not directly supported by luxfi/fhe library
func FHERotl(ctx *PrecompileContext, input []byte) ([]byte, error) {
	// Rotl not directly supported by the FHE library
	// In production, this would use a combination of Shl, Shr, and Or
	_ = input
	return nil, ErrOperationNotSupported
}

// FHERotr performs right rotation: result = rotr(a, bits)
// NOTE: Not directly supported by luxfi/fhe library
func FHERotr(ctx *PrecompileContext, input []byte) ([]byte, error) {
	// Rotr not directly supported by the FHE library
	// In production, this would use a combination of Shr, Shl, and Or
	_ = input
	return nil, ErrOperationNotSupported
}

// FHESelect performs conditional selection: result = condition ? ifTrue : ifFalse
func FHESelect(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 96 {
		return nil, ErrInvalidInput
	}

	handleCond, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}
	handleTrue, err := ParseHandle(input, 32)
	if err != nil {
		return nil, err
	}
	handleFalse, err := ParseHandle(input, 64)
	if err != nil {
		return nil, err
	}

	ctCond, err := ctx.Store.Load(handleCond)
	if err != nil {
		return nil, err
	}
	ctTrue, err := ctx.Store.Load(handleTrue)
	if err != nil {
		return nil, err
	}
	ctFalse, err := ctx.Store.Load(handleFalse)
	if err != nil {
		return nil, err
	}

	result, err := ctx.Store.Evaluator().Select(ctCond, ctTrue, ctFalse)
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(result, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// TrivialEncrypt creates a ciphertext from a plaintext value
func TrivialEncrypt(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	// Parse plaintext value (first 32 bytes)
	var value big.Int
	value.SetBytes(input[0:32])

	// Parse type (second 32-byte slot)
	fheType, err := ParseType(input, 32)
	if err != nil {
		return nil, err
	}

	var ct *fhe.RadixCiphertext

	// Encrypt based on type
	if fheType <= FheUint64 {
		// For types up to 64 bits, use uint64
		ct, err = ctx.Store.Encryptor().EncryptUint64(value.Uint64(), fheType.ToLibType())
	} else {
		// For larger types, use big.Int
		ct, err = ctx.Store.Encryptor().EncryptBigInt(&value, fheType.ToLibType())
	}
	if err != nil {
		return nil, err
	}

	resultHandle, err := ctx.Store.Store(ct, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// VerifyCiphertext verifies a ciphertext from external input
func VerifyCiphertext(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	// Parse the serialized ciphertext from input
	// First 32 bytes: ciphertext data length
	// Next 32 bytes: type
	// Remaining: serialized ciphertext

	dataLen, err := ParseUint64(input, 0)
	if err != nil {
		return nil, err
	}

	fheType, err := ParseType(input, 32)
	if err != nil {
		return nil, err
	}

	if uint64(len(input)) < 64+dataLen {
		return nil, ErrInvalidInput
	}

	ctData := input[64 : 64+dataLen]

	// Deserialize and validate the ciphertext
	ct, err := deserializeRadixCiphertext(ctData, ctx.Store.Params())
	if err != nil {
		return nil, err
	}

	// Verify type matches
	if fhe.FheUintType(ct.Type()) != fheType.ToLibType() {
		return nil, ErrTypeMismatch
	}

	// Store the verified ciphertext
	resultHandle, err := ctx.Store.Store(ct, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// Cast converts a ciphertext to a different type
// NOTE: Cast is not directly supported by luxfi/fhe IntegerEvaluator
// In production, this would use BitwiseEvaluator.CastTo
func Cast(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}

	_, err = ParseType(input, 32)
	if err != nil {
		return nil, err
	}

	_, err = ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	// Cast not supported on IntegerEvaluator - requires BitwiseEvaluator
	// Return error for now, pending full implementation
	return nil, ErrOperationNotSupported
}

// Reencrypt creates a new ciphertext encrypted for a different public key
func Reencrypt(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 64 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}

	// Target public key (32 bytes)
	var targetPK [32]byte
	copy(targetPK[:], input[32:64])

	ct, err := ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	// In production, this would use the threshold network to reencrypt
	// For now, we create a new handle for the same ciphertext
	// The actual reencryption would happen via Warp messaging to T-Chain

	resultHandle, err := ctx.Store.Store(ct, ctx.Caller)
	if err != nil {
		return nil, err
	}

	return EncodeHandle(resultHandle), nil
}

// OptReencrypt performs optimistic reencryption (faster, less secure)
func OptReencrypt(ctx *PrecompileContext, input []byte) ([]byte, error) {
	// Same as Reencrypt for now - optimistic version would skip some checks
	return Reencrypt(ctx, input)
}

// Decrypt requests threshold decryption of a ciphertext
// Returns a placeholder - actual decryption happens asynchronously via T-Chain
func Decrypt(ctx *PrecompileContext, input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrInvalidInput
	}

	handle, err := ParseHandle(input, 0)
	if err != nil {
		return nil, err
	}

	ct, err := ctx.Store.Load(handle)
	if err != nil {
		return nil, err
	}

	// In production, this would:
	// 1. Submit decryption request to T-Chain via Warp
	// 2. Wait for threshold signatures
	// 3. Return the plaintext

	// For testing, use local decryption
	value := ctx.Store.Decryptor().DecryptBigInt(ct)

	return EncodeUint256(value.Bytes()), nil
}
