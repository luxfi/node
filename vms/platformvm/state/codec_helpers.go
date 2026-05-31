// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"sync"

	"github.com/luxfi/codec"
	"github.com/luxfi/log"
)

// probe is a flat struct chosen so its codec walk is the empty walk
// (size 0) and therefore Marshal cannot fail for any reason other than
// the requested version being absent. This is what makes the probe a
// reliable boolean test of "is version V registered on c".
type probe struct{}

// multiVersionUnmarshal is the canonical state-side codec read entry
// point. It wraps codec.Manager.Unmarshal with a one-shot post-condition
// check: if the codec is missing the v0 read-only slot map, a structured
// warning is logged the FIRST time it is observed, so a canary boot of
// a v0-on-disk database surfaces every missing-version slot in a single
// log scrape rather than failing piecemeal across iterations.
//
// This is a soft guard, not a hard one — successful Unmarshal of a v1
// payload through a v1-only codec still succeeds. The warning fires
// when (a) a single-version codec is observed AND (b) the failure mode
// it implies (v0 prefix → ErrUnknownVersion) would have triggered on a
// pre-codec-v1 database row. The intent is to make the v1.28.x
// codec-migration convergence iteration-light: future patches can grep
// for this log line in canary stderr and see every remaining gap.
//
// Implementation note: we identify a codec as "missing the v0 slot" by
// attempting to Marshal a one-byte probe at CodecVersionV0. codec.Manager
// returns ErrUnknownVersion if and only if version 0 is not registered
// in its map. The probe is cheap (a 10-byte Marshal at most) and only
// happens on the first observation of each distinct codec pointer.
func multiVersionUnmarshal(c codec.Manager, b []byte, dest interface{}) (uint16, error) {
	checkMultiVersion(c)
	return c.Unmarshal(b, dest)
}

var (
	multiVersionProbeOnce sync.Map // codec.Manager -> struct{}
)

func checkMultiVersion(c codec.Manager) {
	if _, observed := multiVersionProbeOnce.LoadOrStore(c, struct{}{}); observed {
		return
	}
	// Probe: can this codec Marshal at v0? An empty struct has zero
	// codec-walk size, so the only way Marshal can fail is if version 0
	// itself is not registered — which is exactly the failure mode we
	// want the warning to fire on.
	if _, err := c.Marshal(0, probe{}); err != nil {
		log.Warn("state-side codec is single-version; reads of v0-prefixed bytes will fail",
			"err", err,
			"hint", "register the v0 read-only slot map on this codec.Manager",
		)
	}
}
