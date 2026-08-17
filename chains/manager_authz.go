// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/luxfi/chains/ownership"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// maxRestrictedActivationAttempts bounds how many times a restricted chain may be
// deferred waiting for the M-Chain to bootstrap before it is opted out. It exists
// only as defense-in-depth against an M-Chain that never converges; the normal
// path defers zero or one time.
const maxRestrictedActivationAttempts = 8

// entitlement is the narrow accessor the activation check needs from the M-Chain
// VM: the ownership attestation recorded for a node. *mpcvm.VM satisfies it by
// reading its own committed state.
//
// The attestation carries the group key it was signed under, which is safe here
// and ONLY here: it comes out of M-Chain consensus state, which a node operator
// cannot edit. An attestation obtained from anywhere the operator controls — a
// config file, a flag, a file on disk — would prove nothing at all, because the
// operator could sign their own claim with their own key. That is why this is an
// interface over a chain VM and not a parameter.
type entitlement interface {
	Entitlement(node [20]byte) (*ownership.Attestation, error)
}

// authorizeChainActivation reports whether this node may activate (track/validate)
// [chainID].
//
// Returns (authorized, ready):
//   - ready=false means the check cannot decide yet because the M-Chain is not
//     bootstrapped and therefore not queryable. The caller MUST defer, not skip.
//   - ready=true, authorized=true: proceed normally.
//   - ready=true, authorized=false: this node is not entitled to the chain.
//
// Decision order:
//  1. Critical chains (P/C/X/…) are never restricted — always (true, true).
//  2. The D-Chain additionally requires the operator's dex-validator opt-in, so a
//     node that did not ask for the DEX never gets it.
//  3. Unrestricted chains are always (true, true), so the zero/nil set is a no-op.
//  4. Restricted chain, M-Chain absent → (false, true) with a specific reason. An
//     unconfigured entitlement refuses and SAYS SO; it never silently returns
//     false forever.
//  5. Restricted chain, M-Chain present but not bootstrapped → (false, false):
//     defer and retry.
//  6. Otherwise read this node's ownership attestation and let it decide.
func (m *manager) authorizeChainActivation(chainID ids.ID) (authorized bool, ready bool) {
	// Critical chains are foundational; the check must never skip or defer them.
	if m.CriticalChains.Contains(chainID) {
		return true, true
	}

	// D-Chain participation is an operator opt-in, and it is a NECESSARY condition
	// that composes AND-wise with the entitlement below: the D-Chain activates only
	// if the operator asked for it AND this node is entitled. Checked before the
	// unrestricted short-circuit so the opt-in is required either way. A node
	// without dex-validator therefore declines the D-Chain even under
	// --track-all-chains: this is the activation authority, not the platformvm
	// tracking set.
	if m.DChainID != ids.Empty && chainID == m.DChainID && !m.DexValidator {
		m.Log.Info("D-Chain activation declined: dex-validator opt-in is disabled",
			log.Stringer("chainID", chainID),
		)
		return false, true
	}

	if !m.RestrictedChains.Contains(chainID) {
		return true, true
	}

	// The entitlement lives in M-Chain consensus state. No M-Chain means no way to
	// prove entitlement, so refuse — but refuse with the reason, because "this node
	// is not entitled" and "this node cannot find out" are different operational
	// problems and an operator must be able to tell them apart.
	if m.MChainID == ids.Empty {
		m.Log.Error("restricted chain activation refused: no M-Chain in this network's genesis, so no entitlement can be verified",
			log.Stringer("chainID", chainID),
		)
		return false, true
	}
	if !m.IsBootstrapped(m.MChainID) {
		return false, false
	}
	reader := m.entitlements()
	if reader == nil {
		// IsBootstrapped above already required the M-Chain to be registered here,
		// so reaching this means the M-Chain is present and its VM does not
		// implement the accessor: no node can ever be entitled, and every
		// restricted chain opts out on every start until one does. Said plainly,
		// because "not reachable in-process" reads as a transient wiring fault and
		// sends an operator looking for a race that is not there.
		m.Log.Error("restricted chain activation refused: the M-Chain VM implements no entitlement accessor, so no entitlement can be read",
			log.Stringer("chainID", chainID),
			log.Stringer("mChainID", m.MChainID),
		)
		return false, true
	}

	node := [20]byte(m.NodeID)
	att, err := reader.Entitlement(node)
	if err != nil {
		// A read error is not a decline — the M-Chain claims to be bootstrapped but
		// cannot answer. Defer and retry rather than permanently opt out on a
		// transient failure.
		m.Log.Warn("restricted chain activation deferred: entitlement read failed",
			log.Stringer("chainID", chainID),
			log.Err(err),
		)
		return false, false
	}
	// Authorizes settles authenticity and node-binding together, so there is no way
	// to accept an attestation that is valid but issued for a different node.
	if err := att.Authorizes(node); err != nil {
		m.Log.Info("restricted chain activation not authorized",
			log.Stringer("chainID", chainID),
			log.Err(err),
		)
		return false, true
	}
	m.Log.Info("restricted chain activation authorized by ownership attestation",
		log.Stringer("chainID", chainID),
		log.Uint64("token", att.Claim.Token),
	)
	return true, true
}

// entitlements returns the in-process entitlement reader for the M-Chain, or nil
// if the M-Chain VM is not present or does not expose the accessor.
//
// Callers reach this only after IsBootstrapped, which already requires the
// M-Chain to be in this map, so in practice a nil here means the VM does not
// implement the accessor rather than that the chain is missing.
func (m *manager) entitlements() entitlement {
	m.chainsLock.Lock()
	info, ok := m.chains[m.MChainID]
	m.chainsLock.Unlock()
	if !ok {
		return nil
	}
	reader, _ := info.VM.(entitlement)
	return reader
}

// deferRestrictedChain parks [chainParams] out of the creation queue because the
// M-Chain is not yet bootstrapped, and reports whether the chain may still be
// retried. It returns false once the per-chain attempt cap is exhausted, in which
// case the caller must opt the chain out — an M-Chain that never converges must
// not park a chain forever. Parking out of the queue, rather than re-pushing, is
// what keeps the single-threaded dispatch loop from busy-spinning;
// retryPendingRestrictedChains re-pushes when the M-Chain converges.
func (m *manager) deferRestrictedChain(chainParams ChainParameters) (retry bool) {
	m.restrictedChainsLock.Lock()
	defer m.restrictedChainsLock.Unlock()

	m.restrictedAttempts[chainParams.ID]++
	if m.restrictedAttempts[chainParams.ID] > maxRestrictedActivationAttempts {
		return false
	}
	m.pendingRestrictedChains = append(m.pendingRestrictedChains, chainParams)
	return true
}

// retryPendingRestrictedChains re-queues every chain parked by
// deferRestrictedChain.
//
// It is called when the M-Chain finishes BOOTSTRAPPING, not merely when it is
// created. The M-Chain is an engine chain, so it is marked bootstrapped
// asynchronously by monitorBootstrap — draining at creation time would re-run the
// check while the answer was still "not ready", park the chain again, and leave it
// parked with nothing left to drain it.
func (m *manager) retryPendingRestrictedChains() {
	m.restrictedChainsLock.Lock()
	parked := m.pendingRestrictedChains
	m.pendingRestrictedChains = nil
	m.restrictedChainsLock.Unlock()

	for _, chainParams := range parked {
		m.Log.Info("re-queuing restricted chain after M-Chain bootstrap",
			log.Stringer("chainID", chainParams.ID),
			log.Stringer("vmID", chainParams.VMID),
		)
		m.chainsQueue.PushRight(chainParams)
	}
}
