// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"testing"
)

func TestAdvanceTimeTxRoundTrip(t *testing.T) {
	const want uint64 = 1782604800

	tx := NewAdvanceTimeTx(want)
	if tx.IsZero() {
		t.Fatal("NewAdvanceTimeTx returned zero accessor")
	}
	if got := tx.Time(); got != want {
		t.Fatalf("Time() = %d, want %d", got, want)
	}

	// Re-wrap from bytes; round-trip equality.
	bytes := tx.Bytes()
	if len(bytes) == 0 {
		t.Fatal("Bytes() returned empty buffer")
	}
	if !IsZAPBytes(bytes) {
		t.Fatalf("IsZAPBytes(buf) = false, want true (first 4 bytes = %q)", string(bytes[:4]))
	}

	tx2, err := WrapAdvanceTimeTx(bytes)
	if err != nil {
		t.Fatalf("WrapAdvanceTimeTx: %v", err)
	}
	if got := tx2.Time(); got != want {
		t.Fatalf("WrapAdvanceTimeTx.Time() = %d, want %d", got, want)
	}
}

func TestAdvanceTimeTxZeroAlloc(t *testing.T) {
	tx := NewAdvanceTimeTx(1782604800)
	avg := testing.AllocsPerRun(100, func() {
		_ = tx.Time()
	})
	if avg != 0 {
		t.Fatalf("Time() accessor allocates %.2f allocs per run; want 0 (zero-copy)", avg)
	}
}

func TestAdvanceTimeTxBytesReusable(t *testing.T) {
	tx := NewAdvanceTimeTx(42)
	a := tx.Bytes()
	b := tx.Bytes()
	if &a[0] != &b[0] {
		t.Fatal("Bytes() does not return the underlying buffer (made a copy?)")
	}
}

func TestIsZAPBytes(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty", nil, false},
		{"too short", []byte{'Z', 'A', 'P'}, false},
		{"magic ok", []byte{'Z', 'A', 'P', 0, 0xff}, true},
		{"magic bad", []byte{'Z', 'A', 'P', 0xff, 0}, false},
		{"linearcodec v0", []byte{0, 0, 0, 0, 0}, false},
		{"linearcodec v1", []byte{0, 1, 0, 0, 0}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsZAPBytes(c.in); got != c.want {
				t.Fatalf("IsZAPBytes(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestShouldUseZAPForWrite(t *testing.T) {
	// Default: native ZAP for every timestamp. Legacy is opt-in via
	// LUXD_ENABLE_LEGACY_CODEC. The package-level LegacyEnabled is read
	// once at init from the env var; tests assert the default-false path
	// and the explicitly-toggled path.
	defer func(prev bool) { LegacyEnabled = prev }(LegacyEnabled)

	t.Run("default (legacy disabled): ZAP always", func(t *testing.T) {
		LegacyEnabled = false
		for _, ts := range []uint64{0, 1, 1782604800 - 1, 1782604800, 1782604800 + 86400} {
			if !ShouldUseZAPForWrite(ts) {
				t.Fatalf("ShouldUseZAPForWrite(%d) = false, want true (ZAP default)", ts)
			}
		}
	})

	t.Run("legacy enabled: timestamp-gated", func(t *testing.T) {
		LegacyEnabled = true
		cases := []struct {
			name string
			ts   uint64
			want bool
		}{
			{"pre-activation", 1782604800 - 1, false},
			{"at activation", 1782604800, true},
			{"post-activation", 1782604800 + 86400, true},
			{"epoch", 0, false},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := ShouldUseZAPForWrite(c.ts); got != c.want {
					t.Fatalf("ShouldUseZAPForWrite(%d) = %v, want %v", c.ts, got, c.want)
				}
			})
		}
	})
}
