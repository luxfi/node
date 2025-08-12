#!/bin/bash

echo "Fixing all remaining issues for 100% test pass..."

# Fix syntax errors in averager files
echo "Fixing averager syntax errors..."
sed -i 's/metrics\.NewWithRegistry("", //g; s/).clock//g; s/).Now//g' utils/timer/adaptive_timeout_manager.go
sed -i 's/func NewAverager.*, metrics\.NewWithRegistry.*)/func NewAverager(namespace string, reg metrics.Registry, errs *wrappers.Errs) math.Averager/' consensus/engine/dag/getter/averager.go
sed -i 's/func NewAverager.*, metrics\.NewWithRegistry.*)/func NewAverager(namespace string, reg metrics.Registry) math.Averager/' consensus/engine/chain/averager.go

# Fix metrics type mismatches
echo "Fixing metrics type mismatches..."
find . -name "*.go" -type f -exec sed -i 's/newAverager(\([^,]*\), reg,/newAverager(\1, metrics.NewWithRegistry("", reg),/g' {} \;
find . -name "*.go" -type f -exec sed -i 's/NewAverager(\([^,]*\), \([^,]*\), reg)/NewAverager(\1, \2, metrics.NewWithRegistry("", reg))/g' {} \;

# Fix time.Now issues
echo "Fixing time.Now issues..."
sed -i 's/metrics\.NewWithRegistry("", time)\.Now/time.Now/g' network/p2p/peer_tracker.go
sed -i 's/metrics\.NewWithRegistry("", now)/now/g' network/p2p/peer_tracker.go

# Fix vms/components/index/metrics.go
echo "Fixing vms/components/index/metrics.go..."
cat > vms/components/index/metrics.go << 'EOF'
// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import (
	"github.com/luxfi/metrics"
)

type metrics struct {
	numAcceptedTxs metrics.Counter
}

func newMetrics(namespace string, registerer metrics.Registry) (*metrics, error) {
	metricsInstance := metrics.NewWithRegistry(namespace, registerer)
	m := &metrics{
		numAcceptedTxs: metricsInstance.NewCounter("accepted_txs", "Number of transactions accepted"),
	}
	return m, nil
}
EOF

echo "Script complete!"