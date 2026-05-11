// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package security

import (
	"strconv"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/metric"
)

// Metrics owns the Prometheus gauges and counters that expose the
// active ChainSecurityProfile to the o11y stack. Constructed once at
// node bootstrap by NewMetrics; the resulting struct is stamped onto
// the security service and consumed by mempool / bridge call sites
// via accessor methods.
//
// All gauges carry the (profile_id, profile_name) label pair so a
// chain that hot-swaps its profile (forbidden today, but possible in
// future migrations) keeps a stable time series; consumers can group
// by the label set to count the active profile.
//
// Counters carry the call-site label (chain="P|X|C" for mempool drops,
// primitive=... for bridge classical-compat traversals) so a single
// dashboard query slices by the relevant axis.
type Metrics struct {
	// postQuantumE2E is the headline boolean: 1 iff every E2E axis
	// is post-quantum AND every classical primitive is forbidden.
	// Wallets / dApps wire alerts on a transition 1 → 0.
	postQuantumE2E metric.GaugeVec

	// nistFriendly is 1 iff the chain pins HashSuiteSHA3NIST. A
	// fork that pins BLAKE3-legacy sets this to 0 so audit tooling
	// can refuse to issue a "NIST-aligned" attestation.
	nistFriendly metric.GaugeVec

	// luxCanonical is 1 iff the profile is one of the three
	// audit-signed-off-on Lux canonical profiles (StrictPQ, FIPS,
	// Permissive). Downstream forks return 0.
	luxCanonical metric.GaugeVec

	// unsafeModeEnabled is 1 iff the chain is on a classical-compat
	// fork profile (ProfileID ≥ 0x80). Inverse of luxCanonical for
	// alert ergonomics: alert on unsafeModeEnabled == 1.
	unsafeModeEnabled metric.GaugeVec

	// mempoolClassicalCredentialsTotal counts the number of inbound
	// txs the chain accepted with a classical secp256k1 credential
	// under a classical-compat profile. Strict-PQ chains never
	// increment this counter; the gate at vms/txs/auth/policy.go
	// drops the tx before it reaches the mempool.
	//
	// label: chain ∈ {"P", "X", "C"}
	mempoolClassicalCredentialsTotal metric.CounterVec

	// zchainProofLagEpochs reports the current proof-lag in Z-Chain
	// epochs. The Z-Chain VM updates this gauge on every accepted
	// block; observers alert on a sustained non-zero value.
	zchainProofLagEpochs metric.Gauge
}

// NewMetrics constructs the security-profile metrics and registers
// them on the supplied registry. The returned struct exposes setters
// (SetActiveProfile, ObserveMempoolClassicalCredential,
// SetZchainProofLagEpochs) so the call sites that own the underlying
// state don't take a direct dependency on the prometheus library.
//
// metricsInstance MUST be non-nil; pass metric.NewWithRegistry with
// the security namespace from the node bootstrap.
func NewMetrics(metricsInstance metric.Metrics) *Metrics {
	return &Metrics{
		postQuantumE2E: metricsInstance.NewGaugeVec(
			"profile_post_quantum_end_to_end",
			"1 iff every E2E axis is post-quantum and every classical primitive is forbidden",
			[]string{"profile_id", "profile_name"},
		),
		nistFriendly: metricsInstance.NewGaugeVec(
			"profile_nist_friendly",
			"1 iff the chain pins HashSuiteSHA3NIST",
			[]string{"profile_id"},
		),
		luxCanonical: metricsInstance.NewGaugeVec(
			"profile_lux_canonical",
			"1 iff the chain pins one of the audit-signed-off-on Lux canonical profiles",
			[]string{"profile_id"},
		),
		unsafeModeEnabled: metricsInstance.NewGaugeVec(
			"profile_unsafe_mode_enabled",
			"1 iff the chain pins a classical-compat fork profile (ProfileID >= 0x80)",
			[]string{"profile_id"},
		),
		mempoolClassicalCredentialsTotal: metricsInstance.NewCounterVec(
			"mempool_classical_credentials_total",
			"Number of inbound txs admitted with a classical secp256k1 credential under classical-compat",
			[]string{"chain"},
		),
		zchainProofLagEpochs: metricsInstance.NewGauge(
			"zchain_proof_lag_epochs",
			"Current Z-Chain proof lag, measured in epochs",
		),
	}
}

// SetActiveProfile stamps the active profile's posture onto every
// gauge. Call once at node bootstrap after the profile is resolved;
// re-call only when the chain undergoes a profile migration (which
// is forbidden today but supported by the API surface).
//
// Passing a nil profile resets every gauge to 0 — the explicit
// "no profile pinned" posture.
func (m *Metrics) SetActiveProfile(profile *consensusconfig.ChainSecurityProfile) {
	if m == nil {
		return
	}
	if profile == nil {
		// Reset on the "no profile" label set so a stale gauge
		// doesn't claim post-quantum after a profile got cleared.
		m.postQuantumE2E.WithLabelValues("0", "NONE").Set(0)
		m.nistFriendly.WithLabelValues("0").Set(0)
		m.luxCanonical.WithLabelValues("0").Set(0)
		m.unsafeModeEnabled.WithLabelValues("0").Set(0)
		return
	}
	idLabel := strconv.FormatUint(uint64(profile.ProfileID), 10)
	m.postQuantumE2E.WithLabelValues(idLabel, profile.ProfileName).
		Set(boolToFloat(isPostQuantumEndToEnd(profile)))
	m.nistFriendly.WithLabelValues(idLabel).
		Set(boolToFloat(profile.HashSuiteID == consensusconfig.HashSuiteSHA3NIST))
	m.luxCanonical.WithLabelValues(idLabel).
		Set(boolToFloat(isLuxCanonical(profile)))
	m.unsafeModeEnabled.WithLabelValues(idLabel).
		Set(boolToFloat(profile.ProfileID >= consensusconfig.ForkClassicalCompatUnsafeProfileID))
}

// ObserveMempoolClassicalCredential records that the chain admitted
// a tx whose credential set carries at least one classical secp256k1
// credential under a classical-compat profile. Callers MUST invoke
// this only AFTER the auth-policy gate admits the tx; a refused tx
// MUST NOT increment the counter.
//
// chain is the chain alias ("P", "X", "C").
func (m *Metrics) ObserveMempoolClassicalCredential(chain string) {
	if m == nil {
		return
	}
	m.mempoolClassicalCredentialsTotal.WithLabelValues(chain).Inc()
}

// SetZchainProofLagEpochs sets the current Z-Chain proof-lag gauge.
// The Z-Chain VM calls this on every accepted block; observers alert
// on a sustained non-zero value.
func (m *Metrics) SetZchainProofLagEpochs(epochs float64) {
	if m == nil {
		return
	}
	m.zchainProofLagEpochs.Set(epochs)
}

// boolToFloat is the single point that maps bool → {0, 1} for the
// Prometheus gauges. Standalone for testability and to avoid the
// idiom drift that "1 if x else 0" inline expressions invite.
func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
