// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package math provides safe arithmetic operations for the DEX VM.
package math

import (
	"errors"
	"math"
	"math/big"
)

var (
	// ErrOverflow indicates an arithmetic overflow.
	ErrOverflow = errors.New("arithmetic overflow")

	// ErrDivisionByZero indicates division by zero.
	ErrDivisionByZero = errors.New("division by zero")

	// Common big.Int values for performance.
	bigZero = big.NewInt(0)
	bigOne  = big.NewInt(1)

	// maxUint64 as big.Int for overflow checking.
	maxUint64Big = new(big.Int).SetUint64(math.MaxUint64)
)

// SafeMulU64 multiplies two uint64 values and returns an error on overflow.
func SafeMulU64(a, b uint64) (uint64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	result := a * b
	if result/a != b {
		return 0, ErrOverflow
	}
	return result, nil
}

// SafeAddU64 adds two uint64 values and returns an error on overflow.
func SafeAddU64(a, b uint64) (uint64, error) {
	result := a + b
	if result < a {
		return 0, ErrOverflow
	}
	return result, nil
}

// SafeSubU64 subtracts two uint64 values and returns an error on underflow.
func SafeSubU64(a, b uint64) (uint64, error) {
	if b > a {
		return 0, ErrOverflow
	}
	return a - b, nil
}

// SafeDivU64 divides two uint64 values and returns an error on division by zero.
func SafeDivU64(a, b uint64) (uint64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	return a / b, nil
}

// MulU64Big multiplies two uint64 values using big.Int to avoid overflow.
// Returns the result as a big.Int.
func MulU64Big(a, b uint64) *big.Int {
	bigA := new(big.Int).SetUint64(a)
	bigB := new(big.Int).SetUint64(b)
	return new(big.Int).Mul(bigA, bigB)
}

// MulDivU64 computes (a * b) / c using big.Int to avoid intermediate overflow.
// Returns an error if c is zero or if the result overflows uint64.
func MulDivU64(a, b, c uint64) (uint64, error) {
	if c == 0 {
		return 0, ErrDivisionByZero
	}

	// Use big.Int for the intermediate calculation
	bigA := new(big.Int).SetUint64(a)
	bigB := new(big.Int).SetUint64(b)
	bigC := new(big.Int).SetUint64(c)

	result := new(big.Int).Mul(bigA, bigB)
	result.Div(result, bigC)

	// Check if result fits in uint64
	if result.Cmp(maxUint64Big) > 0 {
		return 0, ErrOverflow
	}

	return result.Uint64(), nil
}

// MulDivRoundUpU64 computes ceil((a * b) / c) using big.Int.
// Returns an error if c is zero or if the result overflows uint64.
func MulDivRoundUpU64(a, b, c uint64) (uint64, error) {
	if c == 0 {
		return 0, ErrDivisionByZero
	}

	bigA := new(big.Int).SetUint64(a)
	bigB := new(big.Int).SetUint64(b)
	bigC := new(big.Int).SetUint64(c)

	product := new(big.Int).Mul(bigA, bigB)
	
	// Round up: (product + c - 1) / c
	product.Add(product, new(big.Int).Sub(bigC, bigOne))
	result := product.Div(product, bigC)

	if result.Cmp(maxUint64Big) > 0 {
		return 0, ErrOverflow
	}

	return result.Uint64(), nil
}

// NotionalValueU64 calculates price * quantity / precision safely.
// This is a common operation in orderbooks.
func NotionalValueU64(price, quantity, precision uint64) (uint64, error) {
	return MulDivU64(price, quantity, precision)
}

// BigMulDiv computes (a * b) / c using big.Int arithmetic.
// All inputs and output are *big.Int. Returns nil if c is zero.
func BigMulDiv(a, b, c *big.Int) *big.Int {
	if c == nil || c.Sign() == 0 {
		return nil
	}
	result := new(big.Int).Mul(a, b)
	return result.Div(result, c)
}

// BigMulDivRoundUp computes ceil((a * b) / c) using big.Int arithmetic.
func BigMulDivRoundUp(a, b, c *big.Int) *big.Int {
	if c == nil || c.Sign() == 0 {
		return nil
	}
	product := new(big.Int).Mul(a, b)
	// Round up: (product + c - 1) / c
	cMinusOne := new(big.Int).Sub(c, bigOne)
	product.Add(product, cMinusOne)
	return product.Div(product, c)
}

// CheckMulOverflowU64 returns true if a * b would overflow uint64.
func CheckMulOverflowU64(a, b uint64) bool {
	if a == 0 || b == 0 {
		return false
	}
	return a > math.MaxUint64/b
}

// CheckAddOverflowU64 returns true if a + b would overflow uint64.
func CheckAddOverflowU64(a, b uint64) bool {
	return a > math.MaxUint64-b
}

// MinU64 returns the smaller of two uint64 values.
func MinU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// MaxU64 returns the larger of two uint64 values.
func MaxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// ClampU64 clamps a value to the range [min, max].
func ClampU64(value, min, max uint64) uint64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// AbsDiffU64 returns |a - b| without underflow.
func AbsDiffU64(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
