// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bridgevmroot

import (
	"encoding/hex"
	"testing"

	"github.com/luxfi/crypto/merkle"
)

// Canonical KAT fixtures — byte-for-byte ports of the deterministic inputs built
// by lux-private/gpu-kernels/backends/vulkan/tests/test_bridgevm_transition_kat.cpp
// (the inputs every GPU backend + the CPU oracle hash), with the kernel's
// Phase-1a (signer promote / auto-unjail / exit-completion) and Phase-1b
// (daily-limit reset) mutations applied so the leaf FIELD VALUES are the
// POST-transition values the GPU hashes.
//
// Round descriptor for all cases: epoch = 10, closing_flag = 1, so
// target_epoch = 11 and the composed epoch (current_epoch on a closing round) is
// 11. parent_state_root[k] = (uint8)(0xAA + k).
const (
	katEpoch       = 10
	katTargetEpoch = katEpoch + 1 // closing_flag = 1
	katParentByte0 = 0xAA         // parent_state_root[k] = 0xAA + k (wraps mod 256)
)

func katParent() [Size]byte {
	var p [Size]byte
	for k := 0; k < Size; k++ {
		p[k] = byte(katParentByte0 + k)
	}
	return p
}

// applySignerTransition reproduces the CPU oracle's Phase-1a on one signer slot
// at target_epoch, mutating Status / JailUntilEpoch in place. This is the exact
// promote-tombstone / exit-completion / auto-unjail sequence from
// test_bridgevm_transition_kat.cpp:cpu_transition (and bridgevm_transition.cu K1
// Phase 1a). Pending-add is cleared; pending-drop and completed-exit tombstone;
// a jail whose until-epoch has elapsed auto-unjails to active.
func applySignerTransition(s *SignerLeaf, targetEpoch uint64) {
	if s.Occupied == 0 {
		return
	}
	if s.Status&SignerStatusPendingAdd != 0 {
		s.Status &^= SignerStatusPendingAdd
	}
	if s.Status&SignerStatusPendingDrop != 0 {
		s.Status &^= SignerStatusPendingDrop
		s.Status |= SignerStatusTombstoned
	}
	if s.Status&SignerStatusExiting != 0 &&
		s.ExitEpoch != 0 &&
		targetEpoch >= s.ExitEpoch &&
		s.Status&SignerStatusTombstoned == 0 {
		s.Status &^= SignerStatusExiting
		s.Status |= SignerStatusTombstoned
	}
	if s.Status&SignerStatusJailed != 0 &&
		s.JailUntilEpoch != 0 &&
		uint32(targetEpoch) >= s.JailUntilEpoch &&
		s.Status&SignerStatusTombstoned == 0 {
		s.Status &^= SignerStatusJailed
		s.Status |= SignerStatusActive
		s.JailUntilEpoch = 0
	}
}

// dailyFixture carries the pre-transition reset_epoch alongside the leaf so the
// test can apply Phase-1b; reset_epoch is NOT part of the daily leaf preimage, so
// it lives only in the fixture, not in DailyLeaf.
type dailyFixture struct {
	DailyLeaf
	resetEpoch uint64
}

// applyDailyReset reproduces the CPU oracle's Phase-1b on one daily-limit slot:
// when target_epoch >= reset_epoch, used_today is zeroed and reset_epoch bumps to
// target_epoch + 1.
func (df *dailyFixture) applyDailyReset(targetEpoch uint64) {
	if df.Status == 0 {
		return
	}
	if targetEpoch >= df.resetEpoch {
		df.UsedTodayLo = 0
		df.UsedTodayHi = 0
		df.resetEpoch = targetEpoch + 1
	}
}

// ---- "mixed" fixture: 24 signer slots, 20 occupied → 12 active after
// transitions (i%5 cycles promote-drop / exit / jail / pending-add / plain). 16
// liquidity slots (12 active), 4 daily limits, 8 inbox (5 live), 8 outbox (6
// live). bond_amount_hi == 0 everywhere → bond_hi == 0. ----

func katMixedSigners() []SignerLeaf {
	const sc = 24
	sg := make([]SignerLeaf, sc)
	for i := 0; i < sc; i++ {
		if i >= 20 {
			continue // slots 20..23 left unoccupied (zero)
		}
		s := &sg[i]
		s.Occupied = 1
		s.SignerID = uint64(i + 1)
		for k := 0; k < 20; k++ {
			s.UTXOAddr[k] = byte(i + k)
		}
		for k := 0; k < 48; k++ {
			s.BLSPubkey[k] = byte(0x10 + k)
		}
		for k := 0; k < 32; k++ {
			s.CoronaPubkey[k] = byte(0x20 + k)
			s.MLDSAPubkey[k] = byte(0x30 + k)
		}
		s.BondLo = 1000000 * uint64(i+1)
		s.OptInHeight = uint64(100 + i)
		s.Status = SignerStatusActive
		switch i % 5 {
		case 0:
			s.Status |= SignerStatusPendingDrop
		case 1:
			s.Status = SignerStatusExiting
			s.ExitEpoch = 5
		case 2:
			s.Status = SignerStatusJailed
			s.JailUntilEpoch = 11
		case 3:
			s.Status |= SignerStatusPendingAdd
		}
		applySignerTransition(s, katTargetEpoch)
	}
	return sg
}

func katMixedLiquidity() []LiquidityLeaf {
	const lc = 16
	lq := make([]LiquidityLeaf, lc)
	for i := 0; i < lc; i++ {
		if i >= 12 {
			continue
		}
		lq[i].Status = 1
		lq[i].AssetID = uint32(i%4) + 1
		for k := 0; k < 20; k++ {
			lq[i].ProviderAddr[k] = byte(i*7 + k)
		}
		lq[i].AmountLo = uint64(1000000 + i)
		lq[i].FeeAccrualLo = uint64(i)
	}
	return lq
}

func katMixedDaily() []DailyLeaf {
	const dc = 4
	out := make([]DailyLeaf, dc)
	for i := 0; i < dc; i++ {
		df := dailyFixture{}
		df.Status = 1
		df.AssetID = uint32(i + 1)
		df.DailyCapLo = 1000000
		df.UsedTodayLo = uint64(500 + i)
		if i%2 != 0 {
			df.resetEpoch = 5
		} else {
			df.resetEpoch = 99
		}
		df.applyDailyReset(katTargetEpoch)
		out[i] = df.DailyLeaf
	}
	return out
}

func katMixedInbox() []MessageLeaf {
	const ic = 8
	ib := make([]MessageLeaf, ic)
	for i := 0; i < ic; i++ {
		if i >= 5 {
			continue
		}
		ib[i].Status = 1
		for k := 0; k < 32; k++ {
			ib[i].MsgID[k] = byte(i + k)
			ib[i].PayloadRoot[k] = byte(0x40 + i + k)
		}
		ib[i].Nonce = uint64(i)
		ib[i].SrcChain = 1
		ib[i].DstChain = 2
		ib[i].Kind = 0
		ib[i].AmountLo = uint64(100 + i)
	}
	return ib
}

func katMixedOutbox() []MessageLeaf {
	const oc = 8
	ob := make([]MessageLeaf, oc)
	for i := 0; i < oc; i++ {
		if i >= 6 {
			continue
		}
		ob[i].Status = 1
		for k := 0; k < 32; k++ {
			ob[i].MsgID[k] = byte(0x80 + i + k)
			ob[i].PayloadRoot[k] = byte(0x90 + i + k)
		}
		ob[i].Nonce = uint64(i)
		ob[i].SrcChain = 2
		ob[i].DstChain = 3
		ob[i].Kind = 0
		ob[i].AmountLo = uint64(200 + i)
	}
	return ob
}

// TestMixedKAT is the cross-language byte-parity proof for the "mixed" fixture:
// native Go == GPU. The C++ KAT (run_and_compare "mixed") prints
// signer_root=c9812fee9d0704ed… and the composed state_root=6c973a559afb5fd0…
// with bond_hi == 0. We assert those documented prefixes exactly and log the
// full 32-byte sub-roots + state_root.
func TestMixedKAT(t *testing.T) {
	const (
		wantSignerPrefix = "c9812fee9d0704ed" // GPU signer_set_root[0..8]
		wantStatePrefix  = "6c973a559afb5fd0" // GPU bridgevm_state_root[0..8]
		wantBondHi       = uint64(0)
	)

	signers := katMixedSigners()
	state, signer, liq, inbox, outbox, daily, bondLo, bondHi, active := StateRoot(
		katParent(),
		signers,
		katMixedLiquidity(),
		katMixedInbox(),
		katMixedOutbox(),
		katMixedDaily(),
		katTargetEpoch, // closing round: composed epoch = current_epoch = 11
	)

	t.Logf("signer_set_root:    %x", signer)
	t.Logf("liquidity_root:     %x", liq)
	t.Logf("inbox_root:         %x", inbox)
	t.Logf("outbox_root:        %x", outbox)
	t.Logf("daily_limit_root:   %x", daily)
	t.Logf("bridgevm_state_root:%x", state)
	t.Logf("active=%d bond_lo=%d bond_hi=%d", active, bondLo, bondHi)

	if active != 12 {
		t.Errorf("active count: got %d want 12", active)
	}
	if bondHi != wantBondHi {
		t.Errorf("bond_hi: got %d want %d", bondHi, wantBondHi)
	}
	if got := hex.EncodeToString(signer[:]); got[:16] != wantSignerPrefix {
		t.Errorf("signer_set_root prefix:\n got  %s\n want %s…", got, wantSignerPrefix)
	}
	if got := hex.EncodeToString(state[:]); got[:16] != wantStatePrefix {
		t.Errorf("bridgevm_state_root prefix:\n got  %s\n want %s…", got, wantStatePrefix)
	}
}

// ---- "dense-signer N" fixtures for N ∈ {5,9,17}: every signer is plain
// kActive (no promote/exit/jail), so all N are live AND active. bond_amount_lo =
// MAX-i (near-max, forces carry into hi), bond_amount_hi = 3+i (distinct per
// signer) — exercising the FULL 128-bit bond aggregate (FIX 4 / MED-1): bond_hi
// must surface in the compose, not be dropped. The C++ KAT prints, per N,
// bond_hi and signer_set_root[0..8]:
//
//	N=5  → bond_hi=29,  signer_root=ec4075ee…
//	N=9  → bond_hi=71,  signer_root=4441fdc1…
//	N=17 → bond_hi=203, signer_root=6dc3f64f…
//
// The dense-signer cases set liquidity/inbox/outbox/daily to 2 live slots each
// (the signer family is the strict max), all plain-live (no skip).

func katDenseSigners(n uint32) []SignerLeaf {
	sg := make([]SignerLeaf, n)
	for i := uint32(0); i < n; i++ {
		s := &sg[i]
		s.Occupied = 1
		s.SignerID = uint64(i + 1)
		for k := 0; k < 20; k++ {
			s.UTXOAddr[k] = byte(0x40 + int(i) + k)
		}
		for k := 0; k < 48; k++ {
			s.BLSPubkey[k] = byte(0x11 + int(i) + k)
		}
		for k := 0; k < 32; k++ {
			s.CoronaPubkey[k] = byte(0x22 + int(i) + k)
			s.MLDSAPubkey[k] = byte(0x33 + int(i) + k)
		}
		// Near-max lo forces carry into hi; distinct per-signer hi word.
		s.BondLo = 0xFFFFFFFFFFFFFFFF - uint64(i)
		s.BondHi = 3 + uint64(i)
		s.OptInHeight = uint64(100 + i)
		s.Status = SignerStatusActive // all live & active, no transitions
		applySignerTransition(s, katTargetEpoch)
	}
	return sg
}

func katDenseLiquidity() []LiquidityLeaf {
	const lc = 2
	lq := make([]LiquidityLeaf, lc)
	for i := 0; i < lc; i++ {
		lq[i].Status = 1
		lq[i].AssetID = uint32(i%4) + 1
		for k := 0; k < 20; k++ {
			lq[i].ProviderAddr[k] = byte(i*7 + k)
		}
		lq[i].AmountLo = uint64(1000000 + i)
	}
	return lq
}

func katDenseDaily() []DailyLeaf {
	const dc = 2
	out := make([]DailyLeaf, dc)
	for i := 0; i < dc; i++ {
		df := dailyFixture{}
		df.Status = 1
		df.AssetID = uint32(i + 1)
		df.DailyCapLo = 1000000
		df.resetEpoch = 99
		df.applyDailyReset(katTargetEpoch)
		out[i] = df.DailyLeaf
	}
	return out
}

func katDenseInbox() []MessageLeaf {
	const ic = 2
	ib := make([]MessageLeaf, ic)
	for i := 0; i < ic; i++ {
		ib[i].Status = 1
		for k := 0; k < 32; k++ {
			ib[i].MsgID[k] = byte(i + k + 1)
			ib[i].PayloadRoot[k] = byte(0x40 + i + k)
		}
		ib[i].Nonce = uint64(i)
		ib[i].SrcChain = 1
		ib[i].DstChain = 2
	}
	return ib
}

func katDenseOutbox() []MessageLeaf {
	const oc = 2
	ob := make([]MessageLeaf, oc)
	for i := 0; i < oc; i++ {
		ob[i].Status = 1
		for k := 0; k < 32; k++ {
			ob[i].MsgID[k] = byte(0x80 + i + k + 1)
			ob[i].PayloadRoot[k] = byte(0x90 + i + k)
		}
		ob[i].Nonce = uint64(i)
		ob[i].SrcChain = 2
		ob[i].DstChain = 3
	}
	return ob
}

func TestDenseSignerKAT(t *testing.T) {
	cases := []struct {
		n            uint32
		wantBondHi   uint64
		wantSignerPx string // signer_set_root[0..8]
	}{
		{5, 29, "ec4075ee"},
		{9, 71, "4441fdc1"},
		{17, 203, "6dc3f64f"},
	}
	for _, tc := range cases {
		t.Run(hexN(tc.n), func(t *testing.T) {
			signers := katDenseSigners(tc.n)
			state, signer, liq, inbox, outbox, daily, bondLo, bondHi, active := StateRoot(
				katParent(),
				signers,
				katDenseLiquidity(),
				katDenseInbox(),
				katDenseOutbox(),
				katDenseDaily(),
				katTargetEpoch,
			)
			t.Logf("N=%d signer_set_root:    %x", tc.n, signer)
			t.Logf("N=%d liquidity_root:     %x", tc.n, liq)
			t.Logf("N=%d inbox_root:         %x", tc.n, inbox)
			t.Logf("N=%d outbox_root:        %x", tc.n, outbox)
			t.Logf("N=%d daily_limit_root:   %x", tc.n, daily)
			t.Logf("N=%d bridgevm_state_root:%x", tc.n, state)
			t.Logf("N=%d active=%d bond_lo=%d bond_hi=%d", tc.n, active, bondLo, bondHi)

			if active != tc.n {
				t.Errorf("N=%d active: got %d want %d", tc.n, active, tc.n)
			}
			if bondHi != tc.wantBondHi {
				t.Errorf("N=%d bond_hi: got %d want %d", tc.n, bondHi, tc.wantBondHi)
			}
			if got := hex.EncodeToString(signer[:]); got[:8] != tc.wantSignerPx {
				t.Errorf("N=%d signer_set_root prefix:\n got  %s\n want %s…", tc.n, got, tc.wantSignerPx)
			}
		})
	}
}

// hexN renders the sub-test name "N=<n>".
func hexN(n uint32) string {
	return "N=" + itoa(n)
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestActiveBond128Bit guards the FULL 128-bit aggregate: a single active signer
// with the near-max lo and a nonzero hi must carry into bond_hi (FIX 4 / MED-1).
// Two signers at BondLo = MAX, MAX-1 with BondHi = 3, 4 sum to bond_lo =
// 0xFFFFFFFFFFFFFFFE and bond_hi = 3 + 4 + 1(carry) = 8.
func TestActiveBond128Bit(t *testing.T) {
	sg := []SignerLeaf{
		{Occupied: 1, Status: SignerStatusActive, BondLo: 0xFFFFFFFFFFFFFFFF, BondHi: 3},
		{Occupied: 1, Status: SignerStatusActive, BondLo: 0xFFFFFFFFFFFFFFFE, BondHi: 4},
	}
	bondLo, bondHi, active := ActiveBond(sg)
	if active != 2 {
		t.Fatalf("active: got %d want 2", active)
	}
	if bondLo != 0xFFFFFFFFFFFFFFFD { // MAX + (MAX-1) mod 2^64 = 2^64-3
		t.Errorf("bond_lo: got %#x want 0xfffffffffffffffd", bondLo)
	}
	if bondHi != 8 { // 3 + 4 + 1 carry
		t.Errorf("bond_hi: got %d want 8", bondHi)
	}
}

// TestActiveBondSkipsInactive confirms only active signers (Active set, Jailed
// clear, Tombstoned clear, Occupied != 0) contribute to the bond aggregate —
// matching the GPU counter predicate.
func TestActiveBondSkipsInactive(t *testing.T) {
	sg := []SignerLeaf{
		{Occupied: 1, Status: SignerStatusActive, BondLo: 100},                          // counts
		{Occupied: 0, Status: SignerStatusActive, BondLo: 1000},                         // skipped: unoccupied
		{Occupied: 1, Status: SignerStatusActive | SignerStatusJailed, BondLo: 1000},    // skipped: jailed
		{Occupied: 1, Status: SignerStatusActive | SignerStatusTombstoned, BondLo: 100}, // skipped: tombstoned
		{Occupied: 1, Status: SignerStatusActive, BondLo: 50},                           // counts
	}
	bondLo, bondHi, active := ActiveBond(sg)
	if active != 2 || bondLo != 150 || bondHi != 0 {
		t.Errorf("got active=%d bond_lo=%d bond_hi=%d; want active=2 bond_lo=150 bond_hi=0", active, bondLo, bondHi)
	}
}

// TestEmptyFamilyRoots confirms each empty sub-root is keccak256("") (the
// RFC-6962 / merkle.EmptyRoot convention) — an all-empty family must not fold to
// 0x00…00, matching the wave-3/wave-4 spec across all GPU backends.
func TestEmptyFamilyRoots(t *testing.T) {
	const emptyKeccak = "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
	for _, tc := range []struct {
		name string
		got  [Size]byte
	}{
		{"signer", SignerSetRoot(nil)},
		{"liquidity", LiquidityRoot(nil)},
		{"inbox", InboxRoot(nil)},
		{"outbox", OutboxRoot(nil)},
		{"daily", DailyLimitRoot(nil)},
		{"signer-all-unoccupied", SignerSetRoot(make([]SignerLeaf, 4))},
		{"liquidity-all-empty", LiquidityRoot(make([]LiquidityLeaf, 4))},
		{"inbox-all-free", InboxRoot(make([]MessageLeaf, 4))},
		{"daily-all-empty", DailyLimitRoot(make([]DailyLeaf, 4))},
	} {
		if h := hex.EncodeToString(tc.got[:]); h != emptyKeccak {
			t.Errorf("%s empty root:\n got  %s\n want %s", tc.name, h, emptyKeccak)
		}
	}
}

// TestMessageSkipPredicate verifies the inbox/outbox free-slot predicate: a slot
// is folded iff NOT (Status == 0 && MsgID all-zero). A nonzero MsgID with zero
// Status is live; a nonzero Status with zero MsgID is live; both-zero is skipped.
func TestMessageSkipPredicate(t *testing.T) {
	var nonzeroID [32]byte
	nonzeroID[0] = 1
	all := []MessageLeaf{
		{},                        // skipped (free)
		{Status: 1},               // live (status)
		{MsgID: nonzeroID},        // live (msg_id)
		{},                        // skipped (free)
		{Status: 2, MsgID: nonzeroID}, // live
	}
	// Only the live slots (indices 1,2,4) are folded; compare against a manual
	// fold of those three leaf digests in ascending-i order.
	want := merkleRootOf(
		messageLeafDigest(all[1], 1),
		messageLeafDigest(all[2], 2),
		messageLeafDigest(all[4], 4),
	)
	if got := messageRoot(all); got != want {
		t.Errorf("messageRoot skip predicate:\n got  %x\n want %x", got, want)
	}
}

// merkleRootOf folds the given leaf digests through the same combiner the package
// uses (github.com/luxfi/crypto/merkle), for skip-predicate verification.
func merkleRootOf(leaves ...[Size]byte) [Size]byte {
	return merkle.Root(leaves)
}
