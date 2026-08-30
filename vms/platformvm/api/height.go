// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"math"
	"strconv"

	"github.com/luxfi/node/utils/json"
)

// Height is a block height, or the request to use whatever height the next
// proposal will be built at.
//
// On the JSON edge that is three spellings of one field — a quoted number, the
// word "proposed", and null for "leave it as it is". In memory and on the plane
// it is one u64 with ProposedHeight as its sentinel, which is what it has always
// been: the second spelling is the reserved value and the third is the absence
// of the field. So this crosses as a u64 and needs nothing added to it. What a
// reader of the schema needs is to be TOLD which value is the sentinel, and that
// is said in the doc comment of every method that takes one.
type Height json.Uint64

const (
	// Proposed is the word the reserved value is written as.
	Proposed           = "proposed"
	ProposedHeightJSON = `"` + Proposed + `"`
	// ProposedHeight is the reserved height: MaxUint64 means "the height the
	// next proposal will be built at" rather than a height. Supplying it as a
	// number is refused, so the sentinel can never collide with a real height.
	ProposedHeight = math.MaxUint64
)

var errInvalidHeight = errors.New("invalid height")

func (h Height) MarshalJSON() ([]byte, error) {
	if h == ProposedHeight {
		return []byte(ProposedHeightJSON), nil
	}
	return json.Uint64(h).MarshalJSON()
}

func (h *Height) UnmarshalJSON(b []byte) error {
	// First check for known string values
	switch string(b) {
	case json.Null:
		return nil
	case ProposedHeightJSON:
		*h = ProposedHeight
		return nil
	}

	// Otherwise, unmarshal as a uint64
	if err := (*json.Uint64)(h).UnmarshalJSON(b); err != nil {
		return errInvalidHeight
	}

	// MaxUint64 is reserved for proposed height, so return an error if supplied
	// numerically.
	if uint64(*h) == ProposedHeight {
		*h = 0
		return errInvalidHeight
	}
	return nil
}

// MarshalText and UnmarshalText are the same three spellings, said where there
// is no JSON to read them from. A URL carries text, so a read served as a GET
// asks for a height with ?height=proposed or ?height=42 — and a height read as
// the number its kind is takes the second and silently drops the first, which
// is the whole reserved value.
//
// Absence has no text spelling: a URL omits the parameter, and the field keeps
// the zero it had. That is what null means on the JSON side.
func (h Height) MarshalText() ([]byte, error) {
	if h == ProposedHeight {
		return []byte(Proposed), nil
	}
	return []byte(strconv.FormatUint(uint64(h), 10)), nil
}

func (h *Height) UnmarshalText(text []byte) error {
	if string(text) == Proposed {
		*h = ProposedHeight
		return nil
	}
	at, err := strconv.ParseUint(string(text), 10, 64)
	// MaxUint64 is reserved, so supplying it as a number is refused here too:
	// the sentinel can never collide with a height someone asked for.
	if err != nil || at == ProposedHeight {
		return errInvalidHeight
	}
	*h = Height(at)
	return nil
}

func (h Height) IsProposed() bool {
	return h == ProposedHeight
}
