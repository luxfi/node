// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/nftfx"
)

// maxGatedActivationAttempts bounds how many times a gated chain may be
// deferred waiting for the X-Chain to bootstrap before it is opted out. Each
// X-Chain creation re-queues parked chains exactly once, so this also bounds
// total re-queues per chain. It exists only as defense-in-depth against an
// X-Chain that never bootstraps; the normal path defers zero or one time.
const maxGatedActivationAttempts = 8

// NFTAuthorization identifies the X-Chain NFT a validator's staking address
// must hold to activate (track/validate) a gated chain.
//
// A chain is "gated" when ManagerConfig.ChainAuthorizations has an entry for
// its blockchain ID. Chains with no entry are ungated and always authorized —
// so a nil/empty ChainAuthorizations map preserves today's behavior exactly
// for every chain (P/C/X/Q/D/…).
type NFTAuthorization struct {
	AssetID ids.ID // X-Chain nftfx asset (the collection)
	GroupID uint32 // nftfx group within the collection; 0 = any group
}

// xChainUTXOReader is the narrow in-process accessor the gate needs from the
// X-Chain VM: list the UTXOs owned by an address set. *xvm.VM satisfies it via
// its GetUTXOs method (vms/xvm/vm.go), which wraps lux.GetAllUTXOs over the
// chain's committed state. Keeping the interface to this one method avoids
// importing the concrete xvm VM type into the chain manager.
type xChainUTXOReader interface {
	GetUTXOs(addrs set.Set[ids.ShortID]) ([]*lux.UTXO, error)
}

// holdsAuthorizationNFT is the pure authorization policy: it reports whether
// [utxos] contains an nftfx transfer output that satisfies [auth]. A UTXO
// matches when its output is an *nftfx.TransferOutput for auth.AssetID and,
// when auth.GroupID is non-zero, its GroupID equals auth.GroupID (GroupID 0
// means "any group in the collection").
//
// This is the one place the NFT-ownership rule lives. It takes plain values
// (the fetched UTXOs and the requirement) and returns a decision, so it is
// unit-testable without standing up a manager or an X-Chain.
func holdsAuthorizationNFT(utxos []*lux.UTXO, auth NFTAuthorization) bool {
	for _, utxo := range utxos {
		out, ok := utxo.Out.(*nftfx.TransferOutput)
		if !ok {
			continue
		}
		if utxo.AssetID() != auth.AssetID {
			continue
		}
		if auth.GroupID != 0 && out.GroupID != auth.GroupID {
			continue
		}
		return true
	}
	return false
}

// authorizeChainActivation reports whether this node may activate
// (track/validate) [chainID].
//
// Returns (authorized, ready):
//   - ready=false means the gate cannot decide yet because the X-Chain is not
//     bootstrapped and therefore not queryable in-process. The caller MUST
//     defer (re-attempt later), not skip — see createChain.
//   - ready=true, authorized=true: proceed normally.
//   - ready=true, authorized=false: clean opt-out — this validator's staking
//     X-address does not hold the required NFT.
//
// Decision order:
//  1. Critical chains (P/C/X/…) are never gated — always (true, true).
//  2. Ungated chains (no ChainAuthorizations entry) are always (true, true),
//     so the zero/nil map is a no-op for every chain.
//  3. Gated chain, X-Chain not bootstrapped → (false, false): defer.
//  4. Gated chain, X-Chain bootstrapped, but no usable in-process UTXO reader
//     or no resolvable staking X-address → (false, true): clean opt-out. The
//     node refuses to activate a gated chain it cannot prove authorization
//     for, rather than fail open.
//  5. Otherwise query the X-Chain for the staking X-address's UTXOs and apply
//     holdsAuthorizationNFT.
func (m *manager) authorizeChainActivation(chainID ids.ID) (authorized bool, ready bool) {
	// Critical chains are foundational; the gate must never skip or defer them.
	if m.CriticalChains.Contains(chainID) {
		return true, true
	}

	// D-Chain participation is an operator opt-in (dex-validator), and it is a
	// NECESSARY condition that composes AND-wise with the NFT gate below: the
	// D-Chain activates only if the operator opted in AND (when configured) the
	// staking X-address holds the dex-operator NFT. Checked here — before the
	// ungated short-circuit — so the opt-in is required even when no operator
	// collection is configured for this network (the otherwise-ungated case).
	// A node without dex-validator therefore cleanly opts out of the D-Chain
	// even when --track-all-chains queued it: this is the activation authority,
	// not the platformvm tracking set. The opt-out is decided (ready=true), so
	// the caller takes the same clean skip as a non-held NFT / absent plugin.
	if m.DChainID != ids.Empty && chainID == m.DChainID && !m.DexValidator {
		m.Log.Info("D-Chain activation declined: dex-validator opt-in is disabled",
			log.Stringer("chainID", chainID),
		)
		return false, true
	}

	auth, gated := m.ChainAuthorizations[chainID]
	if !gated {
		return true, true
	}

	// TODO(phase-2): proof-of-possession. holdsAuthorizationNFT below checks
	// ownership-OF-RECORD (the staking X-address appears in an nftfx output's
	// owners) — it does NOT prove the node controls the key for that address.
	// Phase-2 must derive StakingXAddress from the node's staking keys and bind
	// the NFT check to key-control (a signed challenge), so a node cannot claim
	// authorization from an NFT held at an address it does not control.

	// The gate consults the X-Chain UTXO set, so the X-Chain must already be
	// bootstrapped on this node. IsBootstrapped now keys on REAL convergence
	// (sb.Bootstrapped), not mere presence — safe here because X-Chain is a DAG
	// chain marked bootstrapped SYNCHRONOUSLY inside createChain (Engine == nil
	// path) before the sequential chain-creator dequeues any re-pushed gated chain,
	// so IsBootstrapped(X) is already true when a gated chain re-runs. INVARIANT: if
	// X-Chain is ever linearized into an engine chain (async Bootstrapped via
	// monitorBootstrap), retryPendingGatedChains must be made to re-drain after X
	// converges, else gated chains park forever. If not bootstrapped yet, defer.
	if !m.IsBootstrapped(m.XChainID) {
		return false, false
	}

	// The staking X-address must be wired for any gated chain. If it is empty,
	// the node has no identity to check NFT ownership against; refuse to
	// activate the gated chain (fail closed) rather than guess.
	if m.StakingXAddress == ids.ShortEmpty {
		m.Log.Warn("gated chain activation blocked: staking X-address not configured",
			log.Stringer("chainID", chainID),
			log.Stringer("assetID", auth.AssetID),
		)
		return false, true
	}

	reader := m.xChainUTXOReader()
	if reader == nil {
		m.Log.Warn("gated chain activation blocked: X-Chain UTXO reader unavailable",
			log.Stringer("chainID", chainID),
			log.Stringer("assetID", auth.AssetID),
		)
		return false, true
	}

	owners := set.Of(m.StakingXAddress)
	utxos, err := reader.GetUTXOs(owners)
	if err != nil {
		// A query error is not a "decline" — the X-Chain claims to be
		// bootstrapped but cannot answer. Defer and retry rather than
		// permanently opt out on a transient read failure.
		m.Log.Warn("gated chain activation deferred: X-Chain UTXO query failed",
			log.Stringer("chainID", chainID),
			log.Stringer("assetID", auth.AssetID),
			log.Err(err),
		)
		return false, false
	}

	return holdsAuthorizationNFT(utxos, auth), true
}

// xChainUTXOReader returns the in-process UTXO reader for the X-Chain, or nil
// if the X-Chain VM is not present or does not expose the accessor. The
// X-Chain VM (*xvm.VM) implements xChainUTXOReader; this asserts against the
// narrow interface only.
func (m *manager) xChainUTXOReader() xChainUTXOReader {
	m.chainsLock.Lock()
	info, ok := m.chains[m.XChainID]
	m.chainsLock.Unlock()
	if !ok {
		return nil
	}
	reader, _ := info.VM.(xChainUTXOReader)
	return reader
}

// deferGatedChain parks [chainParams] out of the creation queue because the
// X-Chain is not yet bootstrapped, and reports whether the chain may still be
// retried. It returns false once the per-chain attempt cap is exhausted, in
// which case the caller must opt the chain out (a never-bootstrapping X-Chain
// must not park a chain forever). Parking out of the queue — rather than
// re-pushing — is what keeps the single-threaded dispatch loop from
// busy-spinning; retryPendingGatedChains re-pushes when the X-Chain is created.
func (m *manager) deferGatedChain(chainParams ChainParameters) (retry bool) {
	m.gatedChainsLock.Lock()
	defer m.gatedChainsLock.Unlock()

	m.gatedAttempts[chainParams.ID]++
	if m.gatedAttempts[chainParams.ID] > maxGatedActivationAttempts {
		return false
	}
	m.pendingGatedChains = append(m.pendingGatedChains, chainParams)
	return true
}

// retryPendingGatedChains re-queues every chain parked by deferGatedChain. It
// is called once right after the X-Chain finishes createChain, so the gate can
// now reach the X-Chain UTXO set. Chains that still cannot decide will park
// again (bounded by maxGatedActivationAttempts).
func (m *manager) retryPendingGatedChains() {
	m.gatedChainsLock.Lock()
	parked := m.pendingGatedChains
	m.pendingGatedChains = nil
	m.gatedChainsLock.Unlock()

	for _, chainParams := range parked {
		m.Log.Info("re-queuing gated chain after X-Chain bootstrap",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		m.chainsQueue.PushRight(chainParams)
	}
}
