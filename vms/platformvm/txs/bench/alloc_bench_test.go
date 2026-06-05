// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bench

import (
	"runtime"
	"testing"
	"time"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// allocLoopSeconds: 5 seconds keeps a full bench run under 10 min on
// CI. The honest report quotes both the 5s baseline AND a manual 60s
// reproduction command.
const allocLoopSeconds = 5

// BenchmarkAllocPressureLegacy: 5-second parse-loop over the legacy
// codec, AddPermissionlessValidatorTx fixture. Reports MB/s allocated,
// mallocs, NumGC.
func BenchmarkAllocPressureLegacy(b *testing.B) {
	if testing.Short() {
		b.Skip("alloc pressure bench skipped under -short")
	}
	tx := NewAddPermissionlessValidatorTxFixture()
	signedBytes := MustMarshalSignedTx(tx)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	deadline := time.Now().Add(allocLoopSeconds * time.Second)
	parses := 0
	for time.Now().Before(deadline) {
		_, err := txs.Parse(txs.Codec, signedBytes)
		if err != nil {
			b.Fatal(err)
		}
		parses++
	}

	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(parses)/float64(allocLoopSeconds), "parses/s")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/1024/1024, "MB_alloc/run")
	b.ReportMetric(float64(after.Mallocs-before.Mallocs), "mallocs/run")
	b.ReportMetric(float64(after.NumGC-before.NumGC), "GC/run")
	if parses > 0 {
		b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(parses), "B/parse")
	}
}

// BenchmarkAllocPressureNativeAdvanceTime: 5-second parse-loop over
// the native-ZAP AdvanceTimeTx path. Direct apples-to-apples vs
// BenchmarkAllocPressureLegacy only if you compare the AdvanceTimeTx-
// only fixture; comparing against the AddPermissionlessValidator
// fixture is unfair to legacy. To get the apples-to-apples number,
// use BenchmarkAllocPressureLegacyAdvanceTime below.
func BenchmarkAllocPressureNativeAdvanceTime(b *testing.B) {
	if testing.Short() {
		b.Skip("alloc pressure bench skipped under -short")
	}
	tx := zap_native.NewAdvanceTimeTx(1782604800)
	zapBytes := tx.Bytes()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	deadline := time.Now().Add(allocLoopSeconds * time.Second)
	parses := 0
	for time.Now().Before(deadline) {
		_, err := zap_native.WrapAdvanceTimeTx(zapBytes)
		if err != nil {
			b.Fatal(err)
		}
		parses++
	}

	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(parses)/float64(allocLoopSeconds), "parses/s")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/1024/1024, "MB_alloc/run")
	b.ReportMetric(float64(after.Mallocs-before.Mallocs), "mallocs/run")
	b.ReportMetric(float64(after.NumGC-before.NumGC), "GC/run")
	if parses > 0 {
		b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(parses), "B/parse")
	}
}

// BenchmarkAllocPressureLegacyAdvanceTime is the apples-to-apples
// counterpart of BenchmarkAllocPressureNativeAdvanceTime: a 5-second
// parse loop on the SAME AdvanceTimeTx payload via the legacy codec.
// The ratio of (parses/s, mallocs/run, GC/run) between this and
// BenchmarkAllocPressureNativeAdvanceTime is the direct GC-pressure
// lift number.
func BenchmarkAllocPressureLegacyAdvanceTime(b *testing.B) {
	if testing.Short() {
		b.Skip("alloc pressure bench skipped under -short")
	}
	tx := NewAdvanceTimeTxFixture()
	signedBytes := MustMarshalSignedTx(tx)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	deadline := time.Now().Add(allocLoopSeconds * time.Second)
	parses := 0
	for time.Now().Before(deadline) {
		_, err := txs.Parse(txs.Codec, signedBytes)
		if err != nil {
			b.Fatal(err)
		}
		parses++
	}

	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(parses)/float64(allocLoopSeconds), "parses/s")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/1024/1024, "MB_alloc/run")
	b.ReportMetric(float64(after.Mallocs-before.Mallocs), "mallocs/run")
	b.ReportMetric(float64(after.NumGC-before.NumGC), "GC/run")
	if parses > 0 {
		b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(parses), "B/parse")
	}
}
