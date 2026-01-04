// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package math provides safe arithmetic operations for the DEX VM.
// All implementations are re-exported from github.com/luxfi/math/safe.
package math

import (
	"math/big"

	"github.com/luxfi/math/safe"
)

// Error variables re-exported from safe package.
var (
	ErrOverflow       = safe.ErrOverflow
	ErrUnderflow      = safe.ErrUnderflow
	ErrDivisionByZero = safe.ErrDivisionByZero
)

// SafeMulU64 multiplies two uint64 values and returns an error on overflow.
func SafeMulU64(a, b uint64) (uint64, error) {
	return safe.Mul64(a, b)
}

// SafeAddU64 adds two uint64 values and returns an error on overflow.
func SafeAddU64(a, b uint64) (uint64, error) {
	return safe.Add64(a, b)
}

// SafeSubU64 subtracts two uint64 values and returns an error on underflow.
func SafeSubU64(a, b uint64) (uint64, error) {
	return safe.Sub(a, b)
}

// SafeDivU64 divides two uint64 values and returns an error on division by zero.
func SafeDivU64(a, b uint64) (uint64, error) {
	return safe.Div64(a, b)
}

// MulU64Big multiplies two uint64 values using big.Int to avoid overflow.
func MulU64Big(a, b uint64) *big.Int {
	return safe.MulBig(a, b)
}

// MulDivU64 computes (a * b) / c using big.Int to avoid intermediate overflow.
func MulDivU64(a, b, c uint64) (uint64, error) {
	return safe.MulDiv64(a, b, c)
}

// MulDivRoundUpU64 computes ceil((a * b) / c) using big.Int.
func MulDivRoundUpU64(a, b, c uint64) (uint64, error) {
	return safe.MulDivRoundUp64(a, b, c)
}

// NotionalValueU64 calculates price * quantity / precision safely.
func NotionalValueU64(price, quantity, precision uint64) (uint64, error) {
	return safe.MulDiv64(price, quantity, precision)
}

// BigMulDiv computes (a * b) / c using big.Int arithmetic.
func BigMulDiv(a, b, c *big.Int) *big.Int {
	return safe.BigMulDiv(a, b, c)
}

// BigMulDivRoundUp computes ceil((a * b) / c) using big.Int arithmetic.
func BigMulDivRoundUp(a, b, c *big.Int) *big.Int {
	return safe.BigMulDivRoundUp(a, b, c)
}

// CheckMulOverflowU64 returns true if a * b would overflow uint64.
func CheckMulOverflowU64(a, b uint64) bool {
	_, overflow := safe.SafeMul(a, b)
	return overflow
}

// CheckAddOverflowU64 returns true if a + b would overflow uint64.
func CheckAddOverflowU64(a, b uint64) bool {
	_, overflow := safe.SafeAdd(a, b)
	return overflow
}

// MinU64 returns the smaller of two uint64 values.
func MinU64(a, b uint64) uint64 {
	return safe.Min(a, b)
}

// MaxU64 returns the larger of two uint64 values.
func MaxU64(a, b uint64) uint64 {
	return safe.Max(a, b)
}

// ClampU64 clamps a value to the range [min, max].
func ClampU64(value, minVal, maxVal uint64) uint64 {
	return safe.Clamp(value, minVal, maxVal)
}

// AbsDiffU64 returns |a - b| without underflow.
func AbsDiffU64(a, b uint64) uint64 {
	return safe.AbsDiff(a, b)
}
