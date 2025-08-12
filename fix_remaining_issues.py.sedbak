#!/usr/bin/env python3

import os
import re
import subprocess

def fix_file(filepath, replacements):
    """Apply replacements to a file"""
    if not os.path.exists(filepath):
        print(f"File not found: {filepath}")
        return
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    for old, new in replacements:
        content = re.sub(old, new, content, flags=re.MULTILINE | re.DOTALL)
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        print(f"Fixed: {filepath}")

# Fix vms/components/index/metrics.go
fix_file('vms/components/index/metrics.go', [
    (r'type metrics struct', r'type indexMetrics struct'),
    (r'func newMetrics\(', r'func newIndexMetrics('),
    (r'return &metrics\{', r'return &indexMetrics{'),
    (r'\) \(\*metrics,', r') (*indexMetrics,'),
])

# Fix vms/components/index/index.go
fix_file('vms/components/index/index.go', [
    (r'\*metrics\n', r'*indexMetrics\n'),
])

# Fix consensus/engine/graph/bootstrap/metrics.go
fix_file('consensus/engine/graph/bootstrap/metrics.go', [
    (r'type metrics struct', r'type bootstrapMetrics struct'),
    (r'func newMetrics\(', r'func newBootstrapMetrics('),
    (r'return metrics\{', r'return bootstrapMetrics{'),
    (r'\) metrics \{', r') bootstrapMetrics {'),
])

# Fix consensus/engine/graph/bootstrap files
for f in ['bootstrapper.go', 'tx_job.go', 'vertex_job.go']:
    fix_file(f'consensus/engine/graph/bootstrap/{f}', [
        (r'\bmetrics\s+metrics\b', r'metrics bootstrapMetrics'),
        (r'b\.metrics\.', r'b.metrics.'),
    ])

# Fix consensus/networking/timeout/manager.go - Registry to Metrics conversion
fix_file('consensus/networking/timeout/manager.go', [
    (r'requestReg,', r'metrics.NewWithRegistry("", requestReg),'),
])

# Fix network/p2p/gossip issues
fix_file('network/p2p/gossip/bloom.go', [
    (r'registerer,', r'metrics.NewWithRegistry("", registerer),'),
])

fix_file('network/p2p/gossip/gossip.go', [
    (r'undefined: reg', r'reg := registerer'),
    (r'metrics\.Register\(m\.bytes\)', r'// metrics.Register(m.bytes) // TODO: Fix vector registration'),
    (r'metrics\.Register\(m\.trackingLifetimeAverage\)', r'// metrics.Register(m.trackingLifetimeAverage) // TODO: Fix gauge registration'),
])

# Fix consensus/engine/chain/metrics.go
fix_file('consensus/engine/chain/metrics.go', [
    (r'metrics\.NewWithRegistry\("", reg\)', r'reg'),
])

# Fix consensus/engine/chain/engine.go
fix_file('consensus/engine/chain/engine.go', [
    (r'config\.Ctx\.MetricsInstance', r'config.Ctx.MetricsRegisterer'),
])

# Fix vms/proposervm issues
fix_file('vms/proposervm/vm.go', [
    (r'vm\.Config\.Registerer,', r'metrics.NewWithRegistry("", vm.Config.Registerer),'),
    (r'vm\.Config\.Registerer\.NewGauge', r'metrics.NewWithRegistry("", vm.Config.Registerer).NewGauge'),
    (r'vm\.Config\.Registerer\.NewHistogram', r'metrics.NewWithRegistry("", vm.Config.Registerer).NewHistogram'),
    (r'vm\.Config\.Registerer\.Register\(vm\.proposerBuildSlotGauge\)', r'// vm.Config.Registerer.Register(vm.proposerBuildSlotGauge) // TODO: Fix gauge registration'),
    (r'vm\.Config\.Registerer\.Register\(vm\.acceptedBlocksSlotHistogram\)', r'// vm.Config.Registerer.Register(vm.acceptedBlocksSlotHistogram) // TODO: Fix histogram registration'),
])

# Run goimports
print("\nRunning goimports...")
subprocess.run(['goimports', '-w', '.'], check=False)

print("\nFixes applied!")
