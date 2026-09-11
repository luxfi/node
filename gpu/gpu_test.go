// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

package gpu

import (
	"encoding/hex"
	"strings"
	"testing"
)

// rng is a cheap, seeded, reproducible byte stream. A differential wants many
// shapes, not many dependencies.
type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *rng) bytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.next())
	}
	return b
}

func pluginPresent() bool { return strings.HasPrefix(Backend(), "plugin:") }

func TestTheHashesAreTheOnesTheChainIsDefinedOver(t *testing.T) {
	// Keccak-256 is Ethereum's 0x01 pad. SHA3-256("abc") is
	// 3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532; a
	// backend answering with that would be answering a different question,
	// which is exactly what the plugin's op_sha3_256_hash is for.
	if got := hex.EncodeToString(mustSlice(Keccak256([]byte("abc")))); got !=
		"4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45" {
		t.Errorf("keccak256(abc) = %s", got)
	}
	if got := hex.EncodeToString(mustSlice(EmptyRoot())); got !=
		"c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470" {
		t.Errorf("keccak256(\"\") = %s", got)
	}
	if got := hex.EncodeToString(mustSlice(Sha256([]byte("abc")))); got !=
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256(abc) = %s", got)
	}
	a := PubkeyToAddress([]byte("abc"))
	if got := hex.EncodeToString(a[:]); got != "bb1be98c142444d7a56aa3981c3942a978e4dc33" {
		t.Errorf("ripemd160(sha256(abc)) = %s", got)
	}
}

func mustSlice(h Hash256) []byte { return h[:] }

func TestABatchAnswersExactlyWhatTheSinglesAnswer(t *testing.T) {
	r := &rng{s: 0x5eed}
	sizes := []int{0, 1, 31, 32, 135, 136, 137, 271, 272, 1024, 4096}
	for _, batch := range []int{1, 2, 3, 7, 64, 257} {
		ins := make([][]byte, batch)
		for i := range ins {
			ins[i] = r.bytes(sizes[i%len(sizes)])
		}
		got := Keccak256Batch(ins)
		if len(got) != batch {
			t.Fatalf("batch %d returned %d digests", batch, len(got))
		}
		for i := range ins {
			if got[i] != CPUKeccak256(ins[i]) {
				t.Fatalf("batch %d element %d (%d bytes) disagrees", batch, i, len(ins[i]))
			}
		}
	}
}

func TestABatchOfNothingButEmptyInputsStillAgrees(t *testing.T) {
	for _, batch := range []int{1, 8, 100} {
		ins := make([][]byte, batch)
		for i := range ins {
			ins[i] = nil
		}
		if !Same(Keccak256Batch(ins), CPUKeccak256Batch(ins)) {
			t.Fatalf("empty-input batch of %d disagrees", batch)
		}
	}
	if len(Keccak256Batch(nil)) != 0 {
		t.Error("a batch of nothing is not nothing")
	}
}

// scalarRoot is the fold written the obvious way, with no batching and no
// dispatch. It is the thing the seam's version has to keep agreeing with.
func scalarRoot(leaves []Hash256) Hash256 {
	if len(leaves) == 0 {
		return CPUKeccak256()
	}
	level := make([]Hash256, len(leaves))
	for i, d := range leaves {
		level[i] = CPUKeccak256([]byte{LeafTag}, d[:])
	}
	for len(level) > 1 {
		cnt := len(level)
		parents := (cnt + 1) / 2
		next := make([]Hash256, parents)
		for j := 0; j < cnt/2; j++ {
			next[j] = CPUKeccak256([]byte{NodeTag}, level[2*j][:], level[2*j+1][:])
		}
		if cnt%2 == 1 {
			next[parents-1] = level[cnt-1]
		}
		level = next
	}
	return level[0]
}

func TestTheBatchedFoldEqualsTheScalarFoldAtEverySize(t *testing.T) {
	r := &rng{s: 0xf01d}
	for _, n := range []int{0, 1, 2, 3, 5, 8, 17, 64, 129, 1000} {
		leaves := make([]Hash256, n)
		for i := range leaves {
			copy(leaves[i][:], r.bytes(32))
		}
		if MerkleRoot(leaves) != scalarRoot(leaves) {
			t.Fatalf("merkle root disagrees at n=%d (backend=%s)", n, Backend())
		}
	}
}

func TestASingleLeafRootIsTheTaggedLeafItself(t *testing.T) {
	var d Hash256
	for i := range d {
		d[i] = 1
	}
	if MerkleRoot([]Hash256{d}) != LeafHash(d) {
		t.Error("a single leaf is not its own tagged leaf")
	}
}

func TestAnOddLevelPromotesTheLastNodeUnchanged(t *testing.T) {
	d := make([]Hash256, 3)
	for i := range d {
		for k := range d[i] {
			d[i][k] = byte(i)
		}
	}
	want := NodeHash(NodeHash(LeafHash(d[0]), LeafHash(d[1])), LeafHash(d[2]))
	if MerkleRoot(d) != want {
		t.Error("the lone right node was not promoted unchanged")
	}
}

func TestTheVerifyComparisonCanFail(t *testing.T) {
	want := CPUKeccak256Batch([][]byte{[]byte("a"), []byte("bb")})
	if !Same(want, want) {
		t.Error("verify rejects two identical answers")
	}
	got := append([]Hash256(nil), want...)
	got[1][31] ^= 1
	if Same(got, want) {
		t.Error("verify accepts two different answers")
	}
	if Same(want[:1], want) {
		t.Error("verify accepts an answer of the wrong length")
	}
}

func TestASignatureThatIsNotOneRecoversNothing(t *testing.T) {
	var h Hash256
	for i := range h {
		h[i] = 7
	}
	var sig [SignatureLen]byte
	if _, ok := Recover(h, sig); ok {
		t.Error("the zero signature recovered a key")
	}
	sig[64] = 4
	if _, ok := Recover(h, sig); ok {
		t.Error("recovery id 4 recovered a key")
	}
}

func TestTheBackendNamesItself(t *testing.T) {
	b := Backend()
	if b != "cpu" && !strings.HasPrefix(b, "plugin:") {
		t.Errorf("Backend() = %q", b)
	}
	t.Logf("backend = %s", b)
}

func TestBothBackendsGiveTheSameAnswer(t *testing.T) {
	if !pluginPresent() {
		t.Skipf("no kernel library installed (backend=%s); set LUX_GPU_LIB to run the differential",
			Backend())
	}
	t.Logf("differential against %s", Backend())

	r := &rng{s: 0xd1ff}
	sizes := []int{0, 1, 31, 32, 135, 136, 137, 271, 272, 1024, 4096}
	for _, batch := range []int{1, 2, 3, 7, 64, 257} {
		ins := make([][]byte, batch)
		for i := range ins {
			ins[i] = r.bytes(sizes[i%len(sizes)])
		}
		if !Same(Keccak256Batch(ins), CPUKeccak256Batch(ins)) {
			t.Fatalf("cpu and plugin disagree on a batch of %d", batch)
		}
	}
}
