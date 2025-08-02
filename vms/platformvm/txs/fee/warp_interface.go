// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

// WarpSignature is an interface for warp signatures used in fee calculations.
// This interface avoids importing the warp package directly.
type WarpSignature interface {
	NumSigners() (int, error)
}

// WarpMessage is an interface for warp messages used in fee calculations.
// This interface avoids importing the warp package directly.
type WarpMessage interface {
	GetSignature() WarpSignature
}

// WarpMessageParser is a function that parses warp messages.
// This allows the warp package to register its parser without creating a circular dependency.
type WarpMessageParser func([]byte) (WarpMessage, error)