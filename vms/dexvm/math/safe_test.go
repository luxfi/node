// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"
	"math/big"
	"testing"
)

func TestSafeMulU64(t *testing.T) {
	tests := []struct {
		name    string
		a, b    uint64
		want    uint64
		wantErr bool
	}{
		{"zero * zero", 0, 0, 0, false},
		{"zero * max", 0, math.MaxUint64, 0, false},
		{"1 * 1", 1, 1, 1, false},
		{"small values", 100, 200, 20000, false},
		{"near overflow", math.MaxUint64 / 2, 2, math.MaxUint64 - 1, false},
		{"overflow", math.MaxUint64, 2, 0, true},
		{"overflow large", 1 << 33, 1 << 33, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeMulU64(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeMulU64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("SafeMulU64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeAddU64(t *testing.T) {
	tests := []struct {
		name    string
		a, b    uint64
		want    uint64
		wantErr bool
	}{
		{"zero + zero", 0, 0, 0, false},
		{"1 + 1", 1, 1, 2, false},
		{"max + 0", math.MaxUint64, 0, math.MaxUint64, false},
		{"overflow", math.MaxUint64, 1, 0, true},
		{"overflow large", math.MaxUint64/2 + 1, math.MaxUint64/2 + 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeAddU64(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeAddU64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("SafeAddU64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeSubU64(t *testing.T) {
	tests := []struct {
		name    string
		a, b    uint64
		want    uint64
		wantErr bool
	}{
		{"5 - 3", 5, 3, 2, false},
		{"5 - 5", 5, 5, 0, false},
		{"0 - 0", 0, 0, 0, false},
		{"underflow", 3, 5, 0, true},
		{"underflow from zero", 0, 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeSubU64(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeSubU64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("SafeSubU64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMulDivU64(t *testing.T) {
	tests := []struct {
		name    string
		a, b, c uint64
		want    uint64
		wantErr bool
	}{
		{"simple", 100, 200, 10, 2000, false},
		{"division by zero", 100, 200, 0, 0, true},
		{"large intermediate", math.MaxUint64, 2, 2, math.MaxUint64, false},
		{"would overflow without big.Int", 1e18, 1e18, 1e18, 1e18, false},
		{"result overflow", math.MaxUint64, math.MaxUint64, 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MulDivU64(tt.a, tt.b, tt.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("MulDivU64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("MulDivU64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotionalValueU64(t *testing.T) {
	// Test price * quantity / 1e18 (common DEX scaling)
	precision := uint64(1e18)

	tests := []struct {
		name     string
		price    uint64
		quantity uint64
		want     uint64
		wantErr  bool
	}{
		{
			name:     "1 ETH at 2000 USDC",
			price:    2000,                      // 2000 USDC (integer, no decimals in price)
			quantity: 1_000_000_000_000_000_000, // 1 ETH with 18 decimals
			want:     2000,                      // (2000 * 10^18) / 10^18 = 2000
			wantErr:  false,
		},
		{
			name:     "0.5 ETH at 2000 USDC",
			price:    2000,                    // 2000 USDC (integer)
			quantity: 500_000_000_000_000_000, // 0.5 ETH (0.5 * 10^18)
			want:     1000,                    // (2000 * 0.5 * 10^18) / 10^18 = 1000
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NotionalValueU64(tt.price, tt.quantity, precision)
			if (err != nil) != tt.wantErr {
				t.Errorf("NotionalValueU64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("NotionalValueU64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBigMulDiv(t *testing.T) {
	a := big.NewInt(1e18)
	b := big.NewInt(2000)
	c := big.NewInt(1e18)

	result := BigMulDiv(a, b, c)
	if result.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("BigMulDiv() = %v, want 2000", result)
	}

	// Test division by zero
	result = BigMulDiv(a, b, big.NewInt(0))
	if result != nil {
		t.Errorf("BigMulDiv() with zero divisor should return nil")
	}
}

func TestCheckMulOverflowU64(t *testing.T) {
	tests := []struct {
		name string
		a, b uint64
		want bool
	}{
		{"no overflow", 100, 200, false},
		{"overflow", math.MaxUint64, 2, true},
		{"zero", 0, math.MaxUint64, false},
		{"boundary", math.MaxUint64/2 + 1, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckMulOverflowU64(tt.a, tt.b); got != tt.want {
				t.Errorf("CheckMulOverflowU64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAbsDiffU64(t *testing.T) {
	tests := []struct {
		name string
		a, b uint64
		want uint64
	}{
		{"a > b", 10, 3, 7},
		{"a < b", 3, 10, 7},
		{"a == b", 5, 5, 0},
		{"zero", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AbsDiffU64(tt.a, tt.b); got != tt.want {
				t.Errorf("AbsDiffU64() = %v, want %v", got, tt.want)
			}
		})
	}
}
