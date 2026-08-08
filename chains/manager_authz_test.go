// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/chains/ownership"
	"github.com/luxfi/constants"
	"github.com/luxfi/container/buffer"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/nets"
)

// committee stands in for the M-Chain: it returns the attestation recorded for a
// node, or an error. A threshold ECDSA signature verifies exactly like a
// single-key one, so a single test key models a real 3-of-5 ceremony faithfully
// and none of this needs a live M-Chain.
type committee struct {
	key *secp256k1.PrivateKey
	// held maps a node to the claim the committee has attested for it. A node
	// absent here has no entitlement — the ordinary case for most validators.
	held map[[20]byte]ownership.Claim
	err  error
	// quorum/signers model the ceremony's policy; defaults are set by newCommittee.
	quorum  int
	signers int
	// tamper, when set, edits the attestation AFTER it is signed — the shape of a
	// relayer or operator altering content in flight.
	tamper func(*ownership.Attestation)
}

func newCommittee(t *testing.T) *committee {
	t.Helper()
	key, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	return &committee{
		key:     key,
		held:    map[[20]byte]ownership.Claim{},
		quorum:  3,
		signers: 3,
	}
}

// attest records that the committee has verified `claim` — the M-Chain-side act.
func (c *committee) attest(claim ownership.Claim) {
	c.held[claim.Node] = claim
}

// Entitlement satisfies the manager's narrow reader over the M-Chain VM.
func (c *committee) Entitlement(node [20]byte) (*ownership.Attestation, error) {
	if c.err != nil {
		return nil, c.err
	}
	claim, ok := c.held[node]
	if !ok {
		// No entitlement on record. The M-Chain answers truthfully; it does not
		// error, because "nothing attested" is a valid, complete answer.
		return &ownership.Attestation{}, nil
	}
	return c.sign(claim), nil
}

// sign produces the attestation the committee would return for a claim.
func (c *committee) sign(claim ownership.Claim) *ownership.Attestation {
	payload := ownership.Payload(claim.Subject(), claim.Root(), claim.Block)
	sig, err := c.key.SignHash(payload[:])
	if err != nil {
		panic(err)
	}
	att := &ownership.Attestation{
		Claim:     claim,
		Epoch:     claim.Block,
		Signers:   c.signers,
		Quorum:    c.quorum,
		Signature: sig,
		GroupKey:  c.key.PublicKey().CompressedBytes(),
	}
	if c.tamper != nil {
		c.tamper(att)
	}
	return att
}

// claimFor is a realistic Validator-tier claim: the token IS the slot, and the
// collection is the live Genesis ERC-721 on Ethereum mainnet.
func claimFor(node ids.NodeID, token uint64) ownership.Claim {
	c := ownership.Claim{Chain: 1, Token: token, Block: 25443474, Node: [20]byte(node)}
	copy(c.Collection[:], []byte{
		0x31, 0xe0, 0xF9, 0x19, 0xC6, 0x7c, 0xeD, 0xd2, 0xBc, 0x3E,
		0x29, 0x43, 0x40, 0xDc, 0x90, 0x07, 0x35, 0x81, 0x03, 0x11,
	})
	for i := range c.Owner {
		c.Owner[i] = byte(0xA0 + i)
	}
	return c
}

// newManager builds a chain manager whose entitlement check is wired to `mChain`.
// When mChain is non-nil it is installed as the M-Chain's tracked VM AND marked
// bootstrapped, since a restricted chain may only be decided once the M-Chain's
// committed state is readable.
func newManager(
	nodeID ids.NodeID,
	mChainID ids.ID,
	restricted set.Set[ids.ID],
	critical set.Set[ids.ID],
	mChain entitlement,
) *manager {
	netsTracker, err := NewNets(ids.GenerateTestNodeID(), map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	})
	if err != nil {
		panic(err)
	}
	m := &manager{
		chains:             make(map[ids.ID]*chainInfo),
		restrictedAttempts: make(map[ids.ID]int),
	}
	m.Log = log.NewNoOpLogger()
	m.Nets = netsTracker
	m.NodeID = nodeID
	m.MChainID = mChainID
	m.RestrictedChains = restricted
	m.CriticalChains = critical
	if mChain != nil {
		m.chains[mChainID] = &chainInfo{Name: "M-Chain", VM: mChain}
		sb, _ := netsTracker.GetOrCreate(constants.PrimaryNetworkID)
		sb.AddChain(mChainID)
		sb.Bootstrapped(mChainID)
	}
	return m
}

// dexManager is the D-Chain shape node.restrictedChainsFor produces for dexvm,
// with the operator opt-in ON so these tests exercise the entitlement half.
func dexManager(t *testing.T, c *committee) (*manager, ids.NodeID, ids.ID) {
	t.Helper()
	nodeID := ids.GenerateTestNodeID()
	mChainID := ids.GenerateTestID()
	dChainID := ids.GenerateTestID()
	m := newManager(nodeID, mChainID, set.Of(dChainID), nil, c)
	m.DChainID = dChainID
	m.DexValidator = true
	return m, nodeID, dChainID
}

// TestUnrestrictedChainsAreUntouched pins the no-op property: with an empty
// restricted set every chain behaves exactly as it did before entitlement
// existed. This is what makes the check safe to ship to a running network.
func TestUnrestrictedChainsAreUntouched(t *testing.T) {
	c := newCommittee(t)
	m := newManager(ids.GenerateTestNodeID(), ids.GenerateTestID(), nil, nil, c)

	authorized, ready := m.authorizeChainActivation(ids.GenerateTestID())
	require.True(t, ready)
	require.True(t, authorized, "an unrestricted chain must never be withheld")
}

// TestCriticalChainsAreNeverRestricted proves P/C/X/Q cannot be withheld even if
// someone lists them as restricted. Withholding a foundational chain would brick
// the node rather than decline an optional one.
func TestCriticalChainsAreNeverRestricted(t *testing.T) {
	critical := ids.GenerateTestID()
	m := newManager(ids.GenerateTestNodeID(), ids.GenerateTestID(),
		set.Of(critical), set.Of(critical), newCommittee(t))

	authorized, ready := m.authorizeChainActivation(critical)
	require.True(t, ready)
	require.True(t, authorized, "a critical chain must never be withheld")
}

// TestOwnershipAuthorizes is the headline positive: a node the committee has
// attested for IS entitled to the D-Chain.
func TestOwnershipAuthorizes(t *testing.T) {
	c := newCommittee(t)
	m, nodeID, dChainID := dexManager(t, c)
	c.attest(claimFor(nodeID, 7))

	authorized, ready := m.authorizeChainActivation(dChainID)
	require.True(t, ready)
	require.True(t, authorized, "an ownership attestation naming this node authorizes the D-Chain")
}

// TestAbsenceRefuses is the headline negative. Each subtest is a distinct way an
// entitlement can be missing or forged, and every one must refuse — decided
// (ready=true), so the caller opts out cleanly instead of retrying forever.
func TestAbsenceRefuses(t *testing.T) {
	t.Run("no attestation on record", func(t *testing.T) {
		c := newCommittee(t)
		m, _, dChainID := dexManager(t, c)

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "a node with no attestation is not entitled")
	})

	t.Run("a peer's attestation is not borrowable", func(t *testing.T) {
		c := newCommittee(t)
		m, _, dChainID := dexManager(t, c)
		// The committee attested a DIFFERENT node. m must not activate on it.
		c.attest(claimFor(ids.GenerateTestNodeID(), 7))

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "an attestation for another node must not authorize this one")
	})

	t.Run("claim tampered after signing", func(t *testing.T) {
		c := newCommittee(t)
		m, nodeID, dChainID := dexManager(t, c)
		c.attest(claimFor(nodeID, 7))
		// Upgrade the claim to a different token on the way out. The signature is
		// authentic for the ORIGINAL claim, so re-pointing it at richer content must
		// fail — this is what stops a relayer editing an attestation in flight.
		c.tamper = func(a *ownership.Attestation) { a.Claim.Token = 8 }

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "an edited claim must not survive its signature")
	})

	t.Run("attestation bound to a different collection", func(t *testing.T) {
		c := newCommittee(t)
		m, nodeID, dChainID := dexManager(t, c)
		c.attest(claimFor(nodeID, 7))
		c.tamper = func(a *ownership.Attestation) { a.Claim.Collection[0] ^= 0xff }

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "swapping the collection must not survive the signature")
	})

	t.Run("below quorum", func(t *testing.T) {
		c := newCommittee(t)
		m, nodeID, dChainID := dexManager(t, c)
		c.attest(claimFor(nodeID, 7))
		c.signers = c.quorum - 1

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "fewer signers than the policy requires is not a committee decision")
	})

	t.Run("quorum claimed as zero", func(t *testing.T) {
		c := newCommittee(t)
		m, nodeID, dChainID := dexManager(t, c)
		c.attest(claimFor(nodeID, 7))
		c.quorum, c.signers = 0, 0

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "a zero threshold must never read as satisfied")
	})
}

// TestDexValidatorAloneIsNotSufficient is the point of the whole change. The
// opt-in flag states what the operator WANTS; the attestation states what they are
// entitled to. Neither alone activates the D-Chain.
func TestDexValidatorAloneIsNotSufficient(t *testing.T) {
	t.Run("flag on, no entitlement => refused", func(t *testing.T) {
		c := newCommittee(t)
		m, _, dChainID := dexManager(t, c)
		m.DexValidator = true

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized,
			"--dex-validator=true must NOT be sufficient on its own")
	})

	t.Run("flag off, entitled => refused", func(t *testing.T) {
		c := newCommittee(t)
		m, nodeID, dChainID := dexManager(t, c)
		m.DexValidator = false
		c.attest(claimFor(nodeID, 7))

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "the opt-in remains necessary even for an entitled node")
	})

	t.Run("flag on and entitled => activated", func(t *testing.T) {
		c := newCommittee(t)
		m, nodeID, dChainID := dexManager(t, c)
		m.DexValidator = true
		c.attest(claimFor(nodeID, 7))

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized)
	})

	t.Run("flag off does not affect a non-D restricted chain", func(t *testing.T) {
		c := newCommittee(t)
		nodeID := ids.GenerateTestNodeID()
		mChainID := ids.GenerateTestID()
		other := ids.GenerateTestID()
		m := newManager(nodeID, mChainID, set.Of(other), nil, c)
		m.DChainID = ids.GenerateTestID()
		m.DexValidator = false // irrelevant to `other`
		c.attest(claimFor(nodeID, 7))

		authorized, ready := m.authorizeChainActivation(other)
		require.True(t, ready)
		require.True(t, authorized, "the dex opt-in must gate ONLY the D-Chain")
	})
}

// TestUnconfiguredRefusesRatherThanGuessing covers the two ways the entitlement
// cannot be read at all. Both must refuse — and refuse DECIDED, so the operator
// gets a clean opt-out and a logged reason rather than a silent false forever.
func TestUnconfiguredRefusesRatherThanGuessing(t *testing.T) {
	t.Run("no M-Chain in genesis", func(t *testing.T) {
		dChainID := ids.GenerateTestID()
		m := newManager(ids.GenerateTestNodeID(), ids.Empty, set.Of(dChainID), nil, nil)
		m.DChainID = dChainID
		m.DexValidator = true

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready, "an unprovable entitlement is decided, not retried forever")
		require.False(t, authorized, "no M-Chain means no entitlement, so refuse")
	})

	t.Run("M-Chain present but exposes no reader", func(t *testing.T) {
		nodeID := ids.GenerateTestNodeID()
		mChainID := ids.GenerateTestID()
		dChainID := ids.GenerateTestID()
		// A VM that does not satisfy the entitlement interface — the shape a
		// plugin-loaded M-Chain presents until it exposes the accessor.
		m := newManager(nodeID, mChainID, set.Of(dChainID), nil, nil)
		m.DChainID = dChainID
		m.DexValidator = true
		m.chains[mChainID] = &chainInfo{Name: "M-Chain", VM: struct{}{}}
		sb, _ := m.Nets.GetOrCreate(constants.PrimaryNetworkID)
		sb.AddChain(mChainID)
		sb.Bootstrapped(mChainID)

		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "an unreadable M-Chain must refuse, not fail open")
	})
}

// TestNotBootstrappedDefers proves the check waits rather than deciding while the
// M-Chain's state is not yet valid to read. Deciding here would opt every node out
// of every restricted chain on every restart.
func TestNotBootstrappedDefers(t *testing.T) {
	c := newCommittee(t)
	nodeID := ids.GenerateTestNodeID()
	mChainID := ids.GenerateTestID()
	dChainID := ids.GenerateTestID()
	m := newManager(nodeID, mChainID, set.Of(dChainID), nil, nil)
	m.DChainID = dChainID
	m.DexValidator = true
	// Present and readable, but NOT marked bootstrapped.
	m.chains[mChainID] = &chainInfo{Name: "M-Chain", VM: c}

	authorized, ready := m.authorizeChainActivation(dChainID)
	require.False(t, ready, "an un-bootstrapped M-Chain must defer, never decide")
	require.False(t, authorized)
}

// TestReadErrorDefers proves a transient failure retries instead of permanently
// opting the node out of a chain it may well be entitled to.
func TestReadErrorDefers(t *testing.T) {
	c := newCommittee(t)
	m, nodeID, dChainID := dexManager(t, c)
	c.attest(claimFor(nodeID, 7))
	c.err = errors.New("state unavailable")

	authorized, ready := m.authorizeChainActivation(dChainID)
	require.False(t, ready, "a read error is a defer, not a decline")
	require.False(t, authorized)
}

// TestDeferCapOptsOut proves the park loop is bounded: an M-Chain that never
// converges cannot park a chain forever.
func TestDeferCapOptsOut(t *testing.T) {
	m := newManager(ids.GenerateTestNodeID(), ids.GenerateTestID(), nil, nil, nil)
	parked := ChainParameters{ID: ids.GenerateTestID()}

	for i := 0; i < maxRestrictedActivationAttempts; i++ {
		require.True(t, m.deferRestrictedChain(parked), "attempt %d must still retry", i+1)
	}
	require.False(t, m.deferRestrictedChain(parked), "the cap must eventually opt out")
}

// TestRetryDrainsParked proves parked chains are re-queued when the M-Chain
// converges — the drain that keeps a deferred chain from being lost.
func TestRetryDrainsParked(t *testing.T) {
	m := newManager(ids.GenerateTestNodeID(), ids.GenerateTestID(), nil, nil, nil)
	m.chainsQueue = buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize)
	parked := ChainParameters{ID: ids.GenerateTestID(), VMID: ids.GenerateTestID()}
	require.True(t, m.deferRestrictedChain(parked))

	m.retryPendingRestrictedChains()

	m.restrictedChainsLock.Lock()
	require.Empty(t, m.pendingRestrictedChains, "the parked list must be drained")
	m.restrictedChainsLock.Unlock()

	got, ok := m.chainsQueue.PopLeft()
	require.True(t, ok)
	require.Equal(t, parked.ID, got.ID)
}
