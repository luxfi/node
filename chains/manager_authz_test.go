// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/container/buffer"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/nets"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/secp256k1fx"
)

// fakeUTXOReader is a stand-in for the X-Chain VM in unit tests. It returns a
// fixed UTXO set (or an error) regardless of the requested addresses, which is
// all the gate's address-set query needs to be driven deterministically.
type fakeUTXOReader struct {
	utxos []*lux.UTXO
	err   error
}

func (f *fakeUTXOReader) GetUTXOs(set.Set[ids.ShortID]) ([]*lux.UTXO, error) {
	return f.utxos, f.err
}

// ownerScopedUTXOReader is a faithful X-Chain stand-in: it returns ONLY the
// UTXOs whose nftfx/secp owners intersect the queried address set, exactly as
// lux.GetAllUTXOs(vm.state, addrs) does over committed state. This is what the
// fixed-list fakeUTXOReader cannot model — and it is required to test the
// ownership-scoping invariant: a node may activate a gated chain only on an NFT
// held at ITS OWN staking X-address, never one held at someone else's. The gate
// queries set.Of(StakingXAddress), so any UTXO owned by a different address is
// invisible here, just as it would be on a real X-Chain.
type ownerScopedUTXOReader struct {
	utxos []*lux.UTXO
}

func (r *ownerScopedUTXOReader) GetUTXOs(addrs set.Set[ids.ShortID]) ([]*lux.UTXO, error) {
	var out []*lux.UTXO
	for _, utxo := range r.utxos {
		owned, ok := utxo.Out.(lux.Addressable)
		if !ok {
			continue
		}
		for _, raw := range owned.Addresses() {
			addr, err := ids.ToShortID(raw)
			if err != nil {
				continue
			}
			if addrs.Contains(addr) {
				out = append(out, utxo)
				break
			}
		}
	}
	return out, nil
}

// nftUTXO builds a UTXO whose output is an nftfx transfer output for the given
// asset and group. owner is the address recorded as the holder.
func nftUTXO(assetID ids.ID, groupID uint32, owner ids.ShortID) *lux.UTXO {
	return &lux.UTXO{
		UTXOID: lux.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  lux.Asset{ID: assetID},
		Out: &nftfx.TransferOutput{
			GroupID: groupID,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{owner},
			},
		},
	}
}

// secpUTXO builds a non-NFT (secp256k1 transfer) UTXO for the given asset, used
// to prove the gate ignores outputs that are not nftfx transfers.
func secpUTXO(assetID ids.ID, owner ids.ShortID) *lux.UTXO {
	return &lux.UTXO{
		UTXOID: lux.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  lux.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{owner},
			},
		},
	}
}

func TestHoldsAuthorizationNFT(t *testing.T) {
	asset := ids.GenerateTestID()
	other := ids.GenerateTestID()
	owner := ids.GenerateTestShortID()

	tests := []struct {
		name  string
		utxos []*lux.UTXO
		auth  NFTAuthorization
		want  bool
	}{
		{
			name:  "empty set",
			utxos: nil,
			auth:  NFTAuthorization{AssetID: asset},
			want:  false,
		},
		{
			name:  "matching asset, any group",
			utxos: []*lux.UTXO{nftUTXO(asset, 7, owner)},
			auth:  NFTAuthorization{AssetID: asset, GroupID: 0},
			want:  true,
		},
		{
			name:  "matching asset and group",
			utxos: []*lux.UTXO{nftUTXO(asset, 7, owner)},
			auth:  NFTAuthorization{AssetID: asset, GroupID: 7},
			want:  true,
		},
		{
			name:  "matching asset, wrong group",
			utxos: []*lux.UTXO{nftUTXO(asset, 7, owner)},
			auth:  NFTAuthorization{AssetID: asset, GroupID: 8},
			want:  false,
		},
		{
			name:  "wrong asset",
			utxos: []*lux.UTXO{nftUTXO(other, 7, owner)},
			auth:  NFTAuthorization{AssetID: asset},
			want:  false,
		},
		{
			name:  "non-nft output for asset is ignored",
			utxos: []*lux.UTXO{secpUTXO(asset, owner)},
			auth:  NFTAuthorization{AssetID: asset},
			want:  false,
		},
		{
			name: "match found among mixed outputs",
			utxos: []*lux.UTXO{
				secpUTXO(asset, owner),
				nftUTXO(other, 1, owner),
				nftUTXO(asset, 3, owner),
			},
			auth: NFTAuthorization{AssetID: asset, GroupID: 3},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, holdsAuthorizationNFT(tt.utxos, tt.auth))
		})
	}
}

// newGateManager builds the minimal manager needed to exercise the gate:
// only the fields authorizeChainActivation reads. xChainVM, when non-nil, is
// installed as the X-Chain's tracked VM AND marked bootstrapped in its net so
// IsBootstrapped(XChainID) is true (the gate consults the X-Chain UTXO set, which
// is only valid once the X-Chain has finished initial sync — mere presence in
// m.chains is no longer sufficient) and xChainUTXOReader() resolves to it.
func newGateManager(
	xChainID ids.ID,
	stakingAddr ids.ShortID,
	authz map[ids.ID]NFTAuthorization,
	critical set.Set[ids.ID],
	xChainVM xChainUTXOReader,
) *manager {
	netsTracker, err := NewNets(ids.GenerateTestNodeID(), map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	})
	if err != nil {
		panic(err)
	}
	m := &manager{
		chains:        make(map[ids.ID]*chainInfo),
		gatedAttempts: make(map[ids.ID]int),
	}
	m.Log = log.NewNoOpLogger()
	m.Nets = netsTracker
	m.XChainID = xChainID
	m.StakingXAddress = stakingAddr
	m.ChainAuthorizations = authz
	m.CriticalChains = critical
	if xChainVM != nil {
		m.chains[xChainID] = &chainInfo{Name: "X-Chain", VM: xChainVM}
		// The X-Chain has finished initial sync (its UTXO set is valid) — the real
		// precondition the gate depends on, now reflected in the net tracking.
		sb, _ := netsTracker.GetOrCreate(constants.PrimaryNetworkID)
		sb.AddChain(xChainID)
		sb.Bootstrapped(xChainID)
	}
	return m
}

func TestAuthorizeChainActivation(t *testing.T) {
	xChainID := ids.GenerateTestID()
	gatedChain := ids.GenerateTestID()
	asset := ids.GenerateTestID()
	addr := ids.GenerateTestShortID()

	collection := map[ids.ID]NFTAuthorization{
		gatedChain: {AssetID: asset, GroupID: 0},
	}

	t.Run("ungated chain is authorized and ready", func(t *testing.T) {
		// No ChainAuthorizations entry: behaves exactly as today, even with no
		// X-Chain present. Covers P/C/X-like IDs via a table.
		m := newGateManager(xChainID, addr, nil, nil, nil)
		for _, id := range []ids.ID{ids.GenerateTestID(), xChainID, gatedChain} {
			authorized, ready := m.authorizeChainActivation(id)
			require.True(t, authorized, "id %s", id)
			require.True(t, ready, "id %s", id)
		}
	})

	t.Run("gated chain, X-Chain not bootstrapped, defers", func(t *testing.T) {
		// xChainVM nil => XChainID not in m.chains => IsBootstrapped false.
		m := newGateManager(xChainID, addr, collection, nil, nil)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.False(t, ready, "must signal defer")
		require.False(t, authorized)
	})

	t.Run("gated chain, holder, authorized", func(t *testing.T) {
		reader := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(asset, 0, addr)}}
		m := newGateManager(xChainID, addr, collection, nil, reader)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.True(t, ready)
		require.True(t, authorized)
	})

	t.Run("gated chain, non-holder, clean opt-out", func(t *testing.T) {
		reader := &fakeUTXOReader{utxos: nil}
		m := newGateManager(xChainID, addr, collection, nil, reader)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.True(t, ready, "decided, not deferred")
		require.False(t, authorized, "no NFT => opt out")
	})

	t.Run("group match vs mismatch", func(t *testing.T) {
		groupCollection := map[ids.ID]NFTAuthorization{
			gatedChain: {AssetID: asset, GroupID: 5},
		}
		hold5 := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(asset, 5, addr)}}
		hold9 := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(asset, 9, addr)}}

		authorized, ready := newGateManager(xChainID, addr, groupCollection, nil, hold5).
			authorizeChainActivation(gatedChain)
		require.True(t, ready)
		require.True(t, authorized, "group 5 held => authorized")

		authorized, ready = newGateManager(xChainID, addr, groupCollection, nil, hold9).
			authorizeChainActivation(gatedChain)
		require.True(t, ready)
		require.False(t, authorized, "wrong group => opt out")
	})

	t.Run("critical chain never skipped even if gated", func(t *testing.T) {
		// gatedChain is in BOTH ChainAuthorizations and CriticalChains, the
		// holder holds nothing, and the X-Chain is absent. A non-critical chain
		// here would defer (not ready); a critical chain must be authorized.
		critical := set.Of(gatedChain)
		m := newGateManager(xChainID, addr, collection, critical, nil)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.True(t, authorized, "critical chain must never be gated")
		require.True(t, ready, "critical chain must never defer")
	})

	t.Run("gated chain, empty staking address, fails closed", func(t *testing.T) {
		reader := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(asset, 0, addr)}}
		m := newGateManager(xChainID, ids.ShortEmpty, collection, nil, reader)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.True(t, ready, "decided")
		require.False(t, authorized, "no staking identity => opt out")
	})

	t.Run("gated chain, X-Chain query error, defers", func(t *testing.T) {
		reader := &fakeUTXOReader{err: errors.New("state not ready")}
		m := newGateManager(xChainID, addr, collection, nil, reader)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.False(t, ready, "transient query error => retry, not opt out")
		require.False(t, authorized)
	})

	t.Run("gated chain, X-Chain tracked but VM lacks reader, opts out", func(t *testing.T) {
		// X-Chain is bootstrapped (in m.chains AND marked bootstrapped in its net, so
		// IsBootstrapped true) but its VM does not satisfy xChainUTXOReader. The gate must
		// fail closed (ready, !authz), not panic.
		m := newGateManager(xChainID, addr, collection, nil, nil)
		m.chains[xChainID] = &chainInfo{Name: "X-Chain", VM: struct{}{}}
		sb, _ := m.Nets.GetOrCreate(constants.PrimaryNetworkID)
		sb.AddChain(xChainID)
		sb.Bootstrapped(xChainID)
		authorized, ready := m.authorizeChainActivation(gatedChain)
		require.True(t, ready)
		require.False(t, authorized)
	})
}

func TestDeferGatedChainCap(t *testing.T) {
	xChainID := ids.GenerateTestID()
	gatedChain := ids.GenerateTestID()
	m := newGateManager(xChainID, ids.GenerateTestShortID(), nil, nil, nil)

	params := ChainParameters{ID: gatedChain, VMID: ids.GenerateTestID()}

	// The first maxGatedActivationAttempts calls park the chain and allow retry.
	for i := 0; i < maxGatedActivationAttempts; i++ {
		require.True(t, m.deferGatedChain(params), "attempt %d should allow retry", i+1)
	}
	// One past the cap opts out instead of parking again.
	require.False(t, m.deferGatedChain(params), "cap exhausted")

	// Every allowed attempt parked exactly one entry; the over-cap attempt did not.
	m.gatedChainsLock.Lock()
	require.Len(t, m.pendingGatedChains, maxGatedActivationAttempts)
	m.gatedChainsLock.Unlock()
}

func TestRetryPendingGatedChainsRequeues(t *testing.T) {
	xChainID := ids.GenerateTestID()
	m := newGateManager(xChainID, ids.GenerateTestShortID(), nil, nil, nil)
	m.chainsQueue = buffer.NewUnboundedBlockingDeque[ChainParameters](initialQueueSize)

	parked := ChainParameters{ID: ids.GenerateTestID(), VMID: ids.GenerateTestID()}
	require.True(t, m.deferGatedChain(parked))

	m.retryPendingGatedChains()

	// Parked list is drained and the chain is re-pushed onto the creation queue.
	m.gatedChainsLock.Lock()
	require.Empty(t, m.pendingGatedChains)
	m.gatedChainsLock.Unlock()

	got, ok := m.chainsQueue.PopLeft()
	require.True(t, ok)
	require.Equal(t, parked.ID, got.ID)
}

// dexGatedManager builds a manager whose D-Chain (dChainID) is NFT-gated on a
// synthetic dex-operator collection — exactly the ChainAuthorizations shape
// node.chainAuthorizationsFor produces for dexvm: dChainID -> {dexAsset,
// group 0}. reader is installed as the bootstrapped X-Chain so the gate can
// query the staking address's UTXOs. This is the chain-manager-side mirror of
// the OptionalVMs[DexVMID].RequiredNFT policy under a synthetic per-network
// asset.
func dexGatedManager(dexAsset ids.ID, staking ids.ShortID, reader xChainUTXOReader) (*manager, ids.ID) {
	xChainID := ids.GenerateTestID()
	dChainID := ids.GenerateTestID()
	authz := map[ids.ID]NFTAuthorization{
		dChainID: {AssetID: dexAsset, GroupID: 0}, // group 0 = any group in the collection
	}
	m := newGateManager(xChainID, staking, authz, nil, reader)
	// Wire the D-Chain identity and turn the operator opt-in ON, so these NFT
	// tests exercise the REAL dex-validator path: dex-validator=true, then the
	// NFT gate decides. (The dex-validator=false short-circuit is proven
	// separately in TestDexValidatorFlagGatesDChain.)
	m.DChainID = dChainID
	m.DexValidator = true
	return m, dChainID
}

// TestDexVMFailsClosedWithoutXNFT (#4). A node whose staking X-address holds NO
// dex-operator NFT is NOT authorized to activate the D-Chain — a clean
// fail-closed opt-out (ready=true, authorized=false), not a fail-open and not a
// deferral. The X-Chain is bootstrapped and answers; the address simply holds
// nothing. Also covers holding the WRONG asset (some other NFT) → still opt-out.
func TestDexVMFailsClosedWithoutXNFT(t *testing.T) {
	dexAsset := ids.GenerateTestID()
	otherAsset := ids.GenerateTestID()
	staking := ids.GenerateTestShortID()

	t.Run("no NFT held => opt out", func(t *testing.T) {
		reader := &fakeUTXOReader{utxos: nil}
		m, dChainID := dexGatedManager(dexAsset, staking, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready, "X-Chain answered => decided, not deferred")
		require.False(t, authorized, "no dex-operator NFT => D-Chain activation fails closed")
	})

	t.Run("wrong collection held => opt out", func(t *testing.T) {
		reader := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(otherAsset, 0, staking)}}
		m, dChainID := dexGatedManager(dexAsset, staking, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "holding a different collection's NFT must not authorize the D-Chain")
	})

	t.Run("no staking identity => fail closed", func(t *testing.T) {
		// Even with a matching NFT in the set, an empty staking X-address has no
		// identity to check ownership against → fail closed.
		reader := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(dexAsset, 0, staking)}}
		m, dChainID := dexGatedManager(dexAsset, ids.ShortEmpty, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "no staking identity => fail closed")
	})
}

// TestDexVMActivatesWithDexOperatorNFT (#5). The same gated D-Chain, but the
// staking X-address holds the dex-operator NFT → authorized to activate.
// Proves the positive path of the NFT-governed activation.
func TestDexVMActivatesWithDexOperatorNFT(t *testing.T) {
	dexAsset := ids.GenerateTestID()
	staking := ids.GenerateTestShortID()

	t.Run("dex-operator NFT held => authorized", func(t *testing.T) {
		reader := &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(dexAsset, 0, staking)}}
		m, dChainID := dexGatedManager(dexAsset, staking, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized, "holding the dex-operator NFT authorizes D-Chain activation")
	})

	t.Run("dex-operator NFT among mixed UTXOs => authorized", func(t *testing.T) {
		other := ids.GenerateTestID()
		reader := &fakeUTXOReader{utxos: []*lux.UTXO{
			secpUTXO(dexAsset, staking),   // non-NFT output for the asset: ignored
			nftUTXO(other, 1, staking),    // wrong collection: ignored
			nftUTXO(dexAsset, 4, staking), // the dex-operator NFT (group 4, any-group gate)
		}}
		m, dChainID := dexGatedManager(dexAsset, staking, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized, "dex-operator NFT present among other UTXOs => authorized")
	})
}

// TestDexVMOwnershipScoping (H1 gap). The activation NFT must be held at the
// node's OWN staking X-address — not merely exist somewhere on the X-Chain. The
// gate queries set.Of(StakingXAddress), so an ownership-honoring reader returns
// nothing for an NFT minted to a DIFFERENT address, and the gate fails closed.
// The fixed-list fakeUTXOReader could not catch this (it returns its list for
// any query); ownerScopedUTXOReader models real per-address X-Chain state.
//
// NOTE: this proves ownership-OF-RECORD scoping (the NFT is owned by this
// address). It does NOT prove the node controls that address's key — that is
// the // TODO(phase-2): proof-of-possession seam in authorizeChainActivation,
// out of scope this pass.
func TestDexVMOwnershipScoping(t *testing.T) {
	dexAsset := ids.GenerateTestID()
	nodeAddr := ids.GenerateTestShortID()     // this node's staking X-address
	strangerAddr := ids.GenerateTestShortID() // a DIFFERENT operator's address

	t.Run("dex-operator NFT held by a DIFFERENT address => not authorized", func(t *testing.T) {
		// The collection's NFT exists, but it is owned by strangerAddr. An
		// ownership-honoring X-Chain returns no UTXOs for nodeAddr's query.
		reader := &ownerScopedUTXOReader{utxos: []*lux.UTXO{nftUTXO(dexAsset, 0, strangerAddr)}}
		m, dChainID := dexGatedManager(dexAsset, nodeAddr, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready, "X-Chain answered => decided, not deferred")
		require.False(t, authorized, "an NFT held at another address must NOT authorize this node")
	})

	t.Run("dex-operator NFT held at the node's OWN address => authorized", func(t *testing.T) {
		// Same collection, but the NFT (alongside the stranger's) is also held by
		// nodeAddr. Only the node's own holding authorizes it.
		reader := &ownerScopedUTXOReader{utxos: []*lux.UTXO{
			nftUTXO(dexAsset, 0, strangerAddr), // not ours: invisible to our query
			nftUTXO(dexAsset, 0, nodeAddr),     // ours: authorizes
		}}
		m, dChainID := dexGatedManager(dexAsset, nodeAddr, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized, "an NFT held at the node's own staking X-address authorizes it")
	})

	t.Run("only the stranger's NFT exists, queried by stranger => authorized for stranger only", func(t *testing.T) {
		// Sanity: the same reader DOES authorize the address that actually holds
		// the NFT, proving the scoping is by-address and not a blanket deny.
		reader := &ownerScopedUTXOReader{utxos: []*lux.UTXO{nftUTXO(dexAsset, 0, strangerAddr)}}
		m, dChainID := dexGatedManager(dexAsset, strangerAddr, reader)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized, "the holder address is authorized; scoping is per-address, not blanket")
	})
}

// TestDexValidatorFlagGatesDChain (M1). The dex-validator opt-in is a genuine,
// NECESSARY activation condition for the D-Chain — not just a log line — and it
// composes AND-wise with the NFT gate. This proves the full truth table at the
// activation authority (authorizeChainActivation), which is downstream of how a
// chain gets QUEUED: --track-all-chains queues the D-Chain in platformvm
// (CreateChain), but the manager's gate is what ACTIVATES it. So a manager with
// DexValidator=false standing in for a --track-all-chains node MUST decline the
// D-Chain regardless of NFT state.
func TestDexValidatorFlagGatesDChain(t *testing.T) {
	xChainID := ids.GenerateTestID()
	dChainID := ids.GenerateTestID()
	dexAsset := ids.GenerateTestID()
	staking := ids.GenerateTestShortID()

	// withFlag builds a D-Chain manager with the dex-validator flag set to
	// [dexValidator]. When [gated] the D-Chain is NFT-gated on dexAsset and the
	// reader is the bootstrapped X-Chain; when not gated, ChainAuthorizations is
	// empty (the --track-all-chains, no-operator-collection case).
	withFlag := func(dexValidator, gated bool, reader xChainUTXOReader) *manager {
		var authz map[ids.ID]NFTAuthorization
		if gated {
			authz = map[ids.ID]NFTAuthorization{dChainID: {AssetID: dexAsset, GroupID: 0}}
		}
		m := newGateManager(xChainID, staking, authz, nil, reader)
		m.DChainID = dChainID
		m.DexValidator = dexValidator
		return m
	}
	holdsNFT := func() xChainUTXOReader {
		return &fakeUTXOReader{utxos: []*lux.UTXO{nftUTXO(dexAsset, 0, staking)}}
	}

	t.Run("flag=false + ungated (track-all-chains) => D NOT activated", func(t *testing.T) {
		// The headline M1 case: even with no operator collection configured (so
		// the chain would otherwise be ungated and activate under --track-all),
		// dex-validator=false declines the D-Chain. Decided opt-out, not defer.
		m := withFlag(false, false, nil)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready, "decided opt-out, not a deferral")
		require.False(t, authorized, "dex-validator=false must block the D-Chain even under --track-all-chains")
	})

	t.Run("flag=false + NFT held => still NOT activated (flag is necessary)", func(t *testing.T) {
		// Holding the dex-operator NFT does not rescue activation when the flag
		// is off: the flag is an independent necessary condition (the AND).
		m := withFlag(false, true, holdsNFT())
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "flag=false blocks D even with the NFT held")
	})

	t.Run("flag=true + NFT held => activated", func(t *testing.T) {
		// Both conditions met: opted in AND holds the NFT.
		m := withFlag(true, true, holdsNFT())
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized, "dex-validator=true AND NFT held => D-Chain activates")
	})

	t.Run("flag=true + gated but NFT NOT held => NOT activated (NFT is necessary)", func(t *testing.T) {
		// The other half of the AND: opting in does not bypass the NFT gate.
		m := withFlag(true, true, &fakeUTXOReader{utxos: nil})
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.False(t, authorized, "flag=true but no NFT => still blocked")
	})

	t.Run("flag=true + ungated (no operator collection) => activated", func(t *testing.T) {
		// Opted in and no NFT requirement configured for this network: the flag
		// alone governs and the D-Chain activates (preserves today's opt-in
		// behavior for the in-flag operator).
		m := withFlag(true, false, nil)
		authorized, ready := m.authorizeChainActivation(dChainID)
		require.True(t, ready)
		require.True(t, authorized, "dex-validator=true with no NFT configured => activates")
	})

	t.Run("flag=false does not affect a non-D gated chain", func(t *testing.T) {
		// The flag is D-specific: another gated chain (e.g. bridgevm) is governed
		// solely by its NFT gate, untouched by dex-validator.
		otherChain := ids.GenerateTestID()
		authz := map[ids.ID]NFTAuthorization{otherChain: {AssetID: dexAsset, GroupID: 0}}
		m := newGateManager(xChainID, staking, authz, nil, holdsNFT())
		m.DChainID = dChainID
		m.DexValidator = false // off, but irrelevant to otherChain
		authorized, ready := m.authorizeChainActivation(otherChain)
		require.True(t, ready)
		require.True(t, authorized, "dex-validator must not gate a non-D chain")
	})
}
