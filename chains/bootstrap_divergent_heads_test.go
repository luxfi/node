package chains

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRefute_DivergentHeadsStillNameAFrontier reproduces the EXACT lux-testnet 96368 head
// distribution (36,060 / 36,169 / 36,841 / 36,841 / 30,622 with the deciding node at 36,060)
// and proves the claim "a live chain whose validators sit on different heads never fetches a
// single block" is false: the ancestor-tolerant tally names the highest ⅔-backed common block
// above the node's own height, so syncOnce gets FrontierNamed and descends.
func TestRefute_DivergentHeadsStillNameAFrontier(t *testing.T) {
	const w uint64 = 100
	const own = 36060

	refs, byID := refChain(own) // genesis..36060, the deciding node's own accepted chain
	prev := refs[own]
	var at36169, at36841 BlockRef
	for h := own + 1; h <= 36841; h++ {
		c := childRef(prev)
		byID[c.ID] = c
		prev = c
		if h == 36169 {
			at36169 = c
		}
	}
	at36841 = prev
	require.Equal(t, uint64(36841), at36841.Height)
	require.Equal(t, uint64(36169), at36169.Height)

	beacons := nodeIDs(4) // the four peers the live node had connected (connectedPeers=4)
	policy := &BootstrapPolicy{
		TrustedBeacons:    equalBeacons(beacons, w),
		MinResponses:      3,
		MinResponders:     bootstrapMinAgreeingBeacons,
		MinFrontierHeight: own,
		Tip:               refs[own].ID,
		NamingWindow:      bootstrapNamingWindow,
		MaxAnchors:        maxNamingAnchors,
		Source:            &stubAncestry{byID: byID},
	}
	replies := []BeaconReply{
		reply(beacons[0], at36169.ID, w),
		reply(beacons[1], at36841.ID, w),
		reply(beacons[2], at36841.ID, w),
		reply(beacons[3], refs[30622].ID, w), // the lagging node, below own height
	}

	f, err := policy.AcceptsFrontier(context.Background(), replies)
	require.NoError(t, err, "four peers on FOUR different heads must still name a frontier")
	require.NotNil(t, f)
	require.Equal(t, at36169.ID, f.ID, "the highest ⅔-backed common block is 36169")
	require.Equal(t, uint64(36169), f.Height)
}
