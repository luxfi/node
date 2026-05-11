// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package security

import (
	"testing"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/metric"
)

// TestMetrics_NilReceiver_Noop proves the metrics struct degrades to
// a no-op when the receiver is nil — every setter must be safe to
// call without crashing. Runs in both metrics-on and metrics-off
// builds because the nil-receiver path does not depend on the
// underlying registry shape.
func TestMetrics_NilReceiver_Noop(t *testing.T) {
	var m *Metrics
	m.SetActiveProfile(consensusconfig.StrictPQ())
	m.SetActiveProfile(nil)
	m.ObserveMempoolClassicalCredential("P")
	m.SetZchainProofLagEpochs(1)
}

// TestMetrics_ConstructionUnderNoopRegistry_DoesNotPanic proves the
// constructor and setters are safe under the no-op registry that the
// default build links in. Production binaries set -tags metrics and
// pick up the real registry; this test fences the development build
// against accidental regressions.
func TestMetrics_ConstructionUnderNoopRegistry_DoesNotPanic(t *testing.T) {
	reg := metric.NewRegistry()
	metricsInstance := metric.NewWithRegistry("lux_security", reg)
	m := NewMetrics(metricsInstance)
	m.SetActiveProfile(consensusconfig.StrictPQ())
	m.ObserveMempoolClassicalCredential("P")
	m.SetZchainProofLagEpochs(3)
	// Gather is expected to be empty under the noop registry; we
	// just verify it returns no error.
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("Gather under noop: %v", err)
	}
}
