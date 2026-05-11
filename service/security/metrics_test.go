// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build metrics

package security

import (
	"strconv"
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/metric"
)

// TestMetrics_SecurityProfileGauge_Reflects_ActiveProfile proves the
// chain-wide profile gauges carry the active profile's posture after
// SetActiveProfile is called.
//
// Closes the "operator can't tell what posture the chain is on"
// audit gap: a 1 → 0 transition on profile_post_quantum_end_to_end
// is the alert primitive an operator wires to PagerDuty.
func TestMetrics_SecurityProfileGauge_Reflects_ActiveProfile(t *testing.T) {
	reg := metric.NewRegistry()
	metricsInstance := metric.NewWithRegistry("lux_security", reg)
	m := NewMetrics(metricsInstance)

	strictPQ := consensusconfig.LuxStrictPQ()
	m.SetActiveProfile(strictPQ)

	// Gather and assert the gauge family for each expected metric
	// carries the strict-PQ posture (1 on each headline boolean).
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	values := flattenMetricValues(families)

	expect := map[string]float64{
		"lux_security_profile_post_quantum_end_to_end:profile_id=" +
			strconv.Itoa(int(consensusconfig.ProfileLuxStrictPQ)) +
			",profile_name=LUX_STRICT_PQ": 1,
		"lux_security_profile_nist_friendly:profile_id=" +
			strconv.Itoa(int(consensusconfig.ProfileLuxStrictPQ)): 1,
		"lux_security_profile_lux_canonical:profile_id=" +
			strconv.Itoa(int(consensusconfig.ProfileLuxStrictPQ)): 1,
		"lux_security_profile_unsafe_mode_enabled:profile_id=" +
			strconv.Itoa(int(consensusconfig.ProfileLuxStrictPQ)): 0,
	}
	for key, want := range expect {
		got, ok := values[key]
		if !ok {
			t.Errorf("missing metric %s; got families: %v", key, values)
			continue
		}
		if got != want {
			t.Errorf("metric %s = %v; want %v", key, got, want)
		}
	}
}

// TestMetrics_SecurityProfileGauge_Reflects_UnsafeFork proves a
// classical-compat fork sets unsafe_mode_enabled=1, post-quantum=0,
// nist_friendly=1 (the unsafe fork keeps SHA3 but loses every
// classical-primitive Forbid bit), and lux_canonical=0.
func TestMetrics_SecurityProfileGauge_Reflects_UnsafeFork(t *testing.T) {
	reg := metric.NewRegistry()
	metricsInstance := metric.NewWithRegistry("lux_security", reg)
	m := NewMetrics(metricsInstance)

	pCopy := consensusconfig.ForkClassicalCompatUnsafeProfile
	hash, err := pCopy.ComputeHash()
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	pCopy.ProfileHash = hash
	m.SetActiveProfile(&pCopy)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	values := flattenMetricValues(families)

	idLabel := strconv.Itoa(int(consensusconfig.ForkClassicalCompatUnsafeProfileID))
	cases := map[string]float64{
		"lux_security_profile_post_quantum_end_to_end:profile_id=" + idLabel +
			",profile_name=FORK_CLASSICAL_COMPAT_UNSAFE": 0,
		"lux_security_profile_lux_canonical:profile_id=" + idLabel:        0,
		"lux_security_profile_unsafe_mode_enabled:profile_id=" + idLabel:  1,
		"lux_security_profile_nist_friendly:profile_id=" + idLabel:        1,
	}
	for key, want := range cases {
		got, ok := values[key]
		if !ok {
			t.Errorf("missing metric %s; values=%v", key, values)
			continue
		}
		if got != want {
			t.Errorf("metric %s = %v; want %v", key, got, want)
		}
	}
}

// TestMetrics_ObserveMempoolClassicalCredential_IncrementsCounter
// proves the per-chain classical-credentials counter wires through
// the call-site shim correctly.
func TestMetrics_ObserveMempoolClassicalCredential_IncrementsCounter(t *testing.T) {
	reg := metric.NewRegistry()
	metricsInstance := metric.NewWithRegistry("lux_security", reg)
	m := NewMetrics(metricsInstance)

	m.ObserveMempoolClassicalCredential("P")
	m.ObserveMempoolClassicalCredential("P")
	m.ObserveMempoolClassicalCredential("X")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	values := flattenMetricValues(families)

	if got := values["lux_security_mempool_classical_credentials_total:chain=P"]; got != 2 {
		t.Errorf("P counter = %v; want 2", got)
	}
	if got := values["lux_security_mempool_classical_credentials_total:chain=X"]; got != 1 {
		t.Errorf("X counter = %v; want 1", got)
	}
}

// TestMetrics_SetZchainProofLagEpochs_StoresValue proves the
// zchain_proof_lag_epochs gauge reflects the supplied epoch count.
func TestMetrics_SetZchainProofLagEpochs_StoresValue(t *testing.T) {
	reg := metric.NewRegistry()
	metricsInstance := metric.NewWithRegistry("lux_security", reg)
	m := NewMetrics(metricsInstance)

	m.SetZchainProofLagEpochs(7)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	values := flattenMetricValues(families)
	got, ok := values["lux_security_zchain_proof_lag_epochs:"]
	if !ok {
		t.Fatalf("missing zchain_proof_lag_epochs; values=%v", values)
	}
	if got != 7 {
		t.Errorf("zchain_proof_lag_epochs = %v; want 7", got)
	}
}

// flattenMetricValues converts a Gather() result into a map keyed by
// "<metric_name>:<sorted_label_pairs>" → value. The flattening lets
// tests assert on a single canonical key per (metric, label set)
// without writing nested loops.
func flattenMetricValues(families []*metric.MetricFamily) map[string]float64 {
	out := make(map[string]float64)
	for _, mf := range families {
		for _, m := range mf.Metrics {
			labels := ""
			for i, lp := range m.Labels {
				if i > 0 {
					labels += ","
				}
				labels += lp.Name + "=" + lp.Value
			}
			out[mf.Name+":"+labels] = m.Value.Value
		}
	}
	return out
}
