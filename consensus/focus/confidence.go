// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package focus

import (
	"github.com/luxfi/ids"
)

// Confidence represents focus intensity for beta-round tracking
// (Previously known as Confidence in Avalanche consensus)
//
// As photonic consensus progresses, the "focus" intensifies
// through beta consecutive rounds of agreement, eventually
// achieving finality when fully focused.
type Confidence interface {
	// RecordPoll records the result of a photon sampling round
	RecordPoll(successful bool, choice ids.ID)

	// GetConfidence returns current confidence level (0 to Beta)
	GetConfidence() int

	// IsFocused returns true if fully focused (beta rounds achieved)
	IsFocused() bool

	// GetBeta returns the beta parameter
	GetBeta() int

	// GetChoice returns the choice we're focusing on
	GetChoice() ids.ID

	// Reset clears confidence state
	Reset()
}

// FocusState represents the current focusing state
// (Previously ConfidenceState)
type FocusState struct {
	Choice           ids.ID // The choice being focused on
	ConsecutivePolls int    // Number of consecutive successful polls
	Beta             int    // Required consecutive polls for focus
	TotalPolls       int    // Total polls conducted
	SuccessfulPolls  int    // Total successful polls
}

// IsFocused returns true if we've achieved focus
func (f *FocusState) IsFocused() bool {
	return f.ConsecutivePolls >= f.Beta
}

// GetIntensity returns focus intensity as percentage (0-100)
func (f *FocusState) GetIntensity() int {
	if f.Beta == 0 {
		return 100
	}
	intensity := (f.ConsecutivePolls * 100) / f.Beta
	if intensity > 100 {
		intensity = 100
	}
	return intensity
}

// FocusType identifies the type of focus tracking
type FocusType int

const (
	BinaryFocus FocusType = iota
	UnaryFocus
	MultiChoiceFocus
)