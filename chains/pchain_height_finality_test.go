// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// pchain_height_finality_test.go — the b2 load-bearing proof the prior round
// lacked: that the node delivers the REAL P-chain epoch height to the chain
// engine, so a K>1 quorum chain finalizes against the LIVE validator set — not
// the frozen genesis set.
//
// The consensus-layer TestPChainEpochFinality_RealWiring proved the engine reads
// the RIGHT height GIVEN a block that already exposes one; it fed a synthetic
// block carrying PChainHeight directly, BYPASSING the boundary. The boundary —
// where a bare plugin block yields pChainHeightOf==0 — was exactly what shipped
// broken (set@0 = genesis). These tests drive the REAL boundary:
//
//	inner VM (bare block, no PChainHeight)
//	  └─ pChainHeightVM (the b2 wrapper, backed by a real validators.State)
//	       └─ consensus engine (real α-of-K cert finality, node BLS sources)
//
// and prove three properties end to end:
//
//	(1) pChainHeightOf(realBlock) returns the wrapper's stamped P-chain height
//	    (NOT 0) — at BuildBlock AND after a ParseBlock round-trip of the gossiped
//	    bytes (the determinism guarantee: every node recovers the same height).
//	(2) K>1 FINALIZES at genesis (set@H0).
//	(3) K>1 FINALIZES AFTER a staking change — validators that JOINED post-genesis
//	    cast the deciding votes+stake. This is the case that STALLS on the set@0
//	    path (the joiners are absent from the genesis set), so finalizing proves
//	    the real height is load-bearing.
//
// CGO-free: the node BLS sources use the pure-Go BLS path under CGO_ENABLED=0, so
// this runs the ACTUAL production quorum sources (blsVoteVerifier / blsVoteSigner
// / validatorStakeSource / validatorSetRootSource) — not an ed25519 stand-in.
package chains

import (
	"context"
	"sync"
	"testing"
	"time"

	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/validatorstest"
)

// --- a bare inner VM whose blocks carry NO P-chain height --------------------

// fakeInnerBlock is a minimal chain block: it satisfies block.Block but does NOT
// expose PChainHeight() — exactly like the plugin VM blocks (C-Chain EVM, dexvm)
// the node runs. Its Bytes() is its own opaque encoding; the wrapper frames a
// P-chain height AROUND these bytes for transport.
type fakeInnerBlock struct {
	id        ids.ID
	parentID  ids.ID
	height    uint64
	bytes     []byte
	timestamp time.Time

	mu           sync.Mutex
	acceptCalled int
}

func (b *fakeInnerBlock) ID() ids.ID                    { return b.id }
func (b *fakeInnerBlock) Parent() ids.ID                { return b.parentID }
func (b *fakeInnerBlock) ParentID() ids.ID             { return b.parentID }
func (b *fakeInnerBlock) Height() uint64               { return b.height }
func (b *fakeInnerBlock) Timestamp() time.Time         { return b.timestamp }
func (b *fakeInnerBlock) Status() uint8                { return 0 }
func (b *fakeInnerBlock) Bytes() []byte                { return b.bytes }
func (b *fakeInnerBlock) Verify(context.Context) error { return nil }
func (b *fakeInnerBlock) Reject(context.Context) error { return nil }
func (b *fakeInnerBlock) Accept(context.Context) error {
	b.mu.Lock()
	b.acceptCalled++
	b.mu.Unlock()
	return nil
}
func (b *fakeInnerBlock) accepted() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.acceptCalled
}

// fakeInnerVM is a BlockBuilder over fakeInnerBlocks, keyed by id and by bytes so
// ParseBlock(bytes) reconstructs the SAME inner block on a follower. It builds one
// block on demand (set via stage) so the test controls the proposed block.
type fakeInnerVM struct {
	mu       sync.Mutex
	byID     map[ids.ID]*fakeInnerBlock
	byBytes  map[string]*fakeInnerBlock
	staged   *fakeInnerBlock // returned by the next BuildBlock
	lastAcc  ids.ID
}

func newFakeInnerVM() *fakeInnerVM {
	return &fakeInnerVM{
		byID:    make(map[ids.ID]*fakeInnerBlock),
		byBytes: make(map[string]*fakeInnerBlock),
	}
}

func (vm *fakeInnerVM) register(b *fakeInnerBlock) {
	vm.mu.Lock()
	vm.byID[b.id] = b
	vm.byBytes[string(b.bytes)] = b
	vm.mu.Unlock()
}

// stage sets the block the next BuildBlock returns (and registers it for Get/Parse).
func (vm *fakeInnerVM) stage(b *fakeInnerBlock) {
	vm.register(b)
	vm.mu.Lock()
	vm.staged = b
	vm.mu.Unlock()
}

func (vm *fakeInnerVM) BuildBlock(context.Context) (block.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.staged, nil
}

func (vm *fakeInnerVM) ParseBlock(_ context.Context, b []byte) (block.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if blk, ok := vm.byBytes[string(b)]; ok {
		return blk, nil
	}
	// Unknown bytes: synthesize a block so a follower can still parse. Real VMs
	// decode deterministically; the test pre-registers every block it gossips, so
	// this path is only a safety net.
	return nil, errUnknownInnerBytes
}

func (vm *fakeInnerVM) GetBlock(_ context.Context, id ids.ID) (block.Block, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if blk, ok := vm.byID[id]; ok {
		return blk, nil
	}
	return nil, errUnknownInnerBytes
}

func (vm *fakeInnerVM) LastAccepted(context.Context) (ids.ID, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.lastAcc, nil
}

func (vm *fakeInnerVM) SetPreference(_ context.Context, id ids.ID) error {
	vm.mu.Lock()
	vm.lastAcc = id
	vm.mu.Unlock()
	return nil
}

var errUnknownInnerBytes = errInnerBytes{}

type errInnerBytes struct{}

func (errInnerBytes) Error() string { return "fakeInnerVM: unknown block bytes" }

// --- BLS validator material (pure-Go BLS under CGO_ENABLED=0) ----------------

type blsValidator struct {
	nodeID ids.NodeID
	sk     *bls.SecretKey
	pkComp []byte
	light  uint64
}

func newBLSValidator(t *testing.T, weight uint64) blsValidator {
	t.Helper()
	sk, err := bls.NewSecretKey()
	if err != nil {
		t.Fatalf("bls.NewSecretKey: %v", err)
	}
	return blsValidator{
		nodeID: ids.GenerateTestNodeID(),
		sk:     sk,
		pkComp: bls.PublicKeyToCompressedBytes(sk.PublicKey()),
		light:  weight,
	}
}

func (v blsValidator) out() *validators.GetValidatorOutput {
	return &validators.GetValidatorOutput{
		NodeID:    v.nodeID,
		PublicKey: v.pkComp,
		Light:     v.light,
		Weight:    v.light,
	}
}

// stateByHeight builds a height-indexed validators.State reporting the given sets
// per height (empty for unknown heights / wrong net), with GetCurrentHeight fixed
// to `current` so the wrapper stamps that height onto built blocks.
func stateByHeight(netID ids.ID, current uint64, byHeight map[uint64][]blsValidator) *validatorstest.TestState {
	s := validatorstest.NewTestState()
	s.GetCurrentHeightF = func(context.Context) (uint64, error) { return current, nil }
	s.GetValidatorSetF = func(_ context.Context, height uint64, gotNet ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		if gotNet != netID {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		}
		out := make(map[ids.NodeID]*validators.GetValidatorOutput)
		for _, v := range byHeight[height] {
			out[v.nodeID] = v.out()
		}
		return out, nil
	}
	return s
}

// --- a recording cert gossiper (engine CertGossiper) -------------------------

type recordingCertGossiper struct {
	mu    sync.Mutex
	certs [][]byte
}

func (g *recordingCertGossiper) GossipCert(_ ids.ID, _ ids.ID, certBytes []byte) error {
	g.mu.Lock()
	g.certs = append(g.certs, append([]byte(nil), certBytes...))
	g.mu.Unlock()
	return nil
}

func (g *recordingCertGossiper) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.certs)
}

var _ consensuschain.CertGossiper = (*recordingCertGossiper)(nil)

// --- helpers -----------------------------------------------------------------

func params5() consensusconfig.Parameters {
	return consensusconfig.Parameters{K: 5, AlphaPreference: 3, AlphaConfidence: 3, Beta: 2}
}

func waitForCond(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// signBLSVote produces validator v's signed accept Vote for a position (the same
// canonical message the node's blsVoteSigner/Verifier use).
func signBLSVote(t *testing.T, v blsValidator, pos consensuschain.VotePosition) consensuschain.Vote {
	t.Helper()
	sig, err := v.sk.Sign(consensuschain.CanonicalVoteMessage(pos))
	if err != nil {
		t.Fatalf("sign vote: %v", err)
	}
	return consensuschain.Vote{
		BlockID:   pos.BlockID,
		NodeID:    v.nodeID,
		Accept:    true,
		SignedAt:  time.Now(),
		Signature: bls.SignatureToBytes(sig),
		ParentID:  pos.ParentID,
		Round:     pos.Round,
	}
}

// quorumEngineFixture wires a real consensus engine with the node's production
// BLS quorum sources, all height-pinned to a height-indexed validators.State,
// driving blocks through the b2 pChainHeightVM wrapper over a bare inner VM.
type quorumEngineFixture struct {
	engine   *consensuschain.Transitive
	wrapper  *pChainHeightVM
	inner    *fakeInnerVM
	chainID  ids.ID
	netID    ids.ID
	proposer blsValidator
	certs    *recordingCertGossiper
}

func newQuorumEngineFixture(t *testing.T, netID ids.ID, state validators.State, proposer blsValidator, byHeight map[uint64][]blsValidator) *quorumEngineFixture {
	t.Helper()
	inner := newFakeInnerVM()
	wrapper := newPChainHeightVM(inner, state, netID)

	chainID := ids.GenerateTestID()
	certs := &recordingCertGossiper{}

	vdrState := state
	engine := consensuschain.NewWithConfig(
		consensuschain.Config{Params: params5(), VM: wrapper},
		consensuschain.WithQuorumCert(chainID, proposer.nodeID, newBLSVoteVerifier(vdrState, netID), certs, newBLSVoteSigner(proposer.sk)),
		consensuschain.WithStakeWeighting(newValidatorStakeSource(vdrState, netID)),
		consensuschain.WithValidatorSetRoot(newValidatorSetRootSource(vdrState, netID)),
	)
	if err := engine.Start(context.Background(), true); err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })

	return &quorumEngineFixture{
		engine:   engine,
		wrapper:  wrapper,
		inner:    inner,
		chainID:  chainID,
		netID:    netID,
		proposer: proposer,
		certs:    certs,
	}
}

// proposeViaWrapper builds the staged inner block THROUGH the wrapper (stamping
// the live P-chain height), tracks it as the engine's own verified proposal with
// the wrapper-delivered epoch, records the proposer's self-vote, and returns the
// wrapped block + the canonical vote position followers must sign.
func (f *quorumEngineFixture) proposeViaWrapper(t *testing.T, inner *fakeInnerBlock) (block.Block, consensuschain.VotePosition) {
	t.Helper()
	f.inner.stage(inner)
	wrapped, err := f.wrapper.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("wrapper.BuildBlock: %v", err)
	}
	pos := f.engine.TrackOwnProposalForTest(context.Background(), wrapped, 0)
	return wrapped, pos
}

// newFakeInner is a small constructor for an inner block at value height h with a
// unique opaque encoding (tag-derived, so byBytes keys never collide).
func newFakeInner(h uint64, parent ids.ID, tag string) *fakeInnerBlock {
	return &fakeInnerBlock{
		id:        ids.GenerateTestID(),
		parentID:  parent,
		height:    h,
		bytes:     []byte("inner:" + tag),
		timestamp: time.Now(),
	}
}

// --- (1) the boundary delivers the REAL height (not 0), build + parse ---------

// TestPChainHeightVM_DeliversRealHeight is the direct b2 boundary proof: the
// wrapper stamps the proposer's live P-chain height onto the block the engine
// sees, so pChainHeightOf(realBlock) returns that height — NOT 0 — at BuildBlock,
// AND a follower recovers the IDENTICAL height by parsing the gossiped bytes (the
// determinism guarantee H rides the bytes, never recomputed from a skewing view).
//
// This is the assertion the whole fix turns on. On the broken path
// pChainHeightOf(any real plugin block)==0 → set@0 (genesis) forever.
func TestPChainHeightVM_DeliversRealHeight(t *testing.T) {
	const epoch = uint64(7) // the live P-chain height the proposer stamps
	netID := ids.GenerateTestID()
	v := newBLSValidator(t, 20)
	// A state whose GetCurrentHeight is 7 (so BuildBlock stamps 7); the set content
	// is irrelevant to the stamping itself, but we register it at 7 for symmetry.
	state := stateByHeight(netID, epoch, map[uint64][]blsValidator{epoch: {v}})
	wrapper := newPChainHeightVM(newFakeInnerVM(), state, netID)

	inner := newFakeInner(10_000_000, ids.Empty, "real-height") // value height races ahead
	wrapper.inner.(*fakeInnerVM).stage(inner)

	wrapped, err := wrapper.BuildBlock(context.Background())
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// (1a) the engine's boundary read on the BUILT block returns the stamped height.
	if got := consensuschain.PChainHeightOfForTest(wrapped); got != epoch {
		t.Fatalf("pChainHeightOf(built block) = %d, want %d — the boundary still delivers the wrong height "+
			"(0 would mean the genesis-set freeze the b2 fix removes)", got, epoch)
	}
	// Sanity: the inner block itself exposes no PChainHeight, so without the wrapper
	// the engine would read 0. This pins WHY the wrapper is load-bearing.
	if got := consensuschain.PChainHeightOfForTest(inner); got != 0 {
		t.Fatalf("bare inner block must expose no P-chain height (pChainHeightOf=0), got %d", got)
	}

	// (1b) determinism: a follower parsing the GOSSIPED bytes recovers the same H.
	parsed, err := wrapper.ParseBlock(context.Background(), wrapped.Bytes())
	if err != nil {
		t.Fatalf("ParseBlock(gossiped bytes): %v", err)
	}
	if got := consensuschain.PChainHeightOfForTest(parsed); got != epoch {
		t.Fatalf("pChainHeightOf(parsed block) = %d, want %d — a follower recomputed/lost the height; "+
			"finality would stall on a dynamic set (worse than the genesis fallback)", got, epoch)
	}
	if parsed.ID() != wrapped.ID() {
		t.Fatalf("parsed block ID %s != built block ID %s — the inner identity must be preserved across the envelope", parsed.ID(), wrapped.ID())
	}
}

// --- (2) K>1 finalizes at genesis (set@H0) -----------------------------------

// TestPChainHeightVM_FinalizesAtGenesis proves the fix UNBRICKS finality in the
// base case: a K>1 quorum chain whose validator set is the genesis set (current
// P-chain height 0) finalizes through the real BLS quorum sources. This is the
// safe floor the genesis fallback guarantees — even here the height is delivered
// honestly (0), and the set@0 is non-empty so a ⅔ quorum finalizes.
func TestPChainHeightVM_FinalizesAtGenesis(t *testing.T) {
	const genesis = uint64(0)
	netID := constantsPrimaryNetworkID()

	g := make([]blsValidator, 5)
	for i := range g {
		g[i] = newBLSValidator(t, 20) // equal stake, total 100
	}
	state := stateByHeight(netID, genesis, map[uint64][]blsValidator{genesis: g})

	f := newQuorumEngineFixture(t, netID, state, g[0], map[uint64][]blsValidator{genesis: g})

	inner := newFakeInner(42, ids.Empty, "genesis-finalize") // value height advanced past genesis
	wrapped, pos := f.proposeViaWrapper(t, inner)

	// The block was stamped at the genesis height (0); the position binds set@0.
	if got := consensuschain.PChainHeightOfForTest(wrapped); got != genesis {
		t.Fatalf("expected genesis stamp %d, got %d", genesis, got)
	}
	if pos.ValidatorSetRoot == ids.Empty {
		t.Fatal("genesis set-root must be non-Empty (the genesis set is non-empty)")
	}

	// proposer g[0] self-voted; drive 3 more signed accepts → 4/5 = 80/100 stake > ⅔.
	f.engine.ReceiveVote(signBLSVote(t, g[1], pos))
	f.engine.ReceiveVote(signBLSVote(t, g[2], pos))
	f.engine.ReceiveVote(signBLSVote(t, g[3], pos))

	if !waitForCond(2*time.Second, func() bool { return inner.accepted() == 1 }) {
		t.Fatalf("UNBRICK: a K>1 block must finalize against the genesis set (VM.Accept=%d)", inner.accepted())
	}
	if f.certs.count() == 0 {
		t.Fatal("a verified quorum cert must be assembled + gossiped at finality")
	}
}

// --- (3) K>1 finalizes AFTER a staking change (THE b2 proof) ------------------

// TestPChainHeightVM_FinalizesAfterStakingChange is the load-bearing b2 proof:
// validators that JOINED after genesis cast the deciding votes + stake, and the
// block finalizes — which is IMPOSSIBLE on the broken set@0 path, where the
// joiners are absent from the genesis set so their votes are dropped and their
// stake is uncounted.
//
// The set@epoch (height 7) holds {g0, j1, j2, j3, j4}: only g0 overlaps genesis;
// j1..j4 JOINED at 7. The genesis set@0 holds five DIFFERENT validators with only
// g0 in common. A 4-voter cert {g0, j1, j2, j3} = 80/100 stake-at-7 > ⅔.
//
//   - With the b2 fix: the block is stamped 7, the verifier resolves j1..j3 at
//     set@7 (present) and the ⅔ tally is measured at 7 → the cert verifies →
//     FINALIZES.
//   - On the broken set@0 path: j1..j3 are unknown at height 0 → their signed
//     votes are dropped → only g0 (20/100) verifies < ⅔ → STALLS FOREVER.
//
// The test asserts BOTH directly: (a) the block finalizes, and (b) the production
// verifier itself rejects a joiner's vote at height 0 but accepts it at height 7 —
// pinning the exact mechanism that would stall the frozen-set path.
func TestPChainHeightVM_FinalizesAfterStakingChange(t *testing.T) {
	const (
		genesis = uint64(0)
		epoch   = uint64(7) // staking change landed here; current P-chain height = 7
	)
	netID := constantsPrimaryNetworkID()

	// g0 is a validator present at BOTH epochs (so the fixture proposer key resolves
	// at the stamped epoch). j1..j4 JOINED at epoch 7. gen1..gen4 are the OTHER four
	// genesis validators (present only at 0) — they make set@0 a genuinely different
	// set so "the joiners decide" is unambiguous.
	g0 := newBLSValidator(t, 20)
	j1 := newBLSValidator(t, 20)
	j2 := newBLSValidator(t, 20)
	j3 := newBLSValidator(t, 20)
	j4 := newBLSValidator(t, 20)
	gen1 := newBLSValidator(t, 20)
	gen2 := newBLSValidator(t, 20)
	gen3 := newBLSValidator(t, 20)
	gen4 := newBLSValidator(t, 20)

	genesisSet := []blsValidator{g0, gen1, gen2, gen3, gen4} // total 100 at height 0
	epochSet := []blsValidator{g0, j1, j2, j3, j4}           // total 100 at height 7

	state := stateByHeight(netID, epoch, map[uint64][]blsValidator{
		genesis: genesisSet,
		epoch:   epochSet,
	})

	// (b) PIN THE MECHANISM that makes the frozen path stall: the production verifier
	// rejects a joiner at height 0 (frozen/genesis) and accepts it at height 7.
	verifier := newBLSVoteVerifier(state, netID)
	// Build a position to sign (any height-7-bound position works for this probe).
	probeBlk := newFakeInner(123, ids.Empty, "probe")
	probeWrapper := newPChainHeightVM(newFakeInnerVM(), state, netID)
	probeWrapper.inner.(*fakeInnerVM).stage(probeBlk)
	probeWrapped, _ := probeWrapper.BuildBlock(context.Background())
	probePos := consensuschain.VotePosition{
		ChainID:          ids.GenerateTestID(),
		Height:           probeWrapped.Height(),
		BlockID:          probeWrapped.ID(),
		ParentID:         ids.Empty,
		ValidatorSetRoot: newValidatorSetRootSource(state, netID).ValidatorSetRoot(epoch),
	}
	probeMsg := consensuschain.CanonicalVoteMessage(probePos)
	j1Sig, err := j1.sk.Sign(probeMsg)
	if err != nil {
		t.Fatalf("sign probe: %v", err)
	}
	j1SigBytes := bls.SignatureToBytes(j1Sig)
	if verifier.VerifyVote(j1.nodeID, probeMsg, j1SigBytes, genesis) {
		t.Fatal("frozen-path mechanism broken: a post-genesis joiner must NOT verify at height 0 " +
			"(if it does, the genesis-set path would not actually stall and the test is vacuous)")
	}
	if !verifier.VerifyVote(j1.nodeID, probeMsg, j1SigBytes, epoch) {
		t.Fatal("a joiner present at the epoch MUST verify at the epoch height — the b2 read is broken")
	}

	// (a) THE END-TO-END FINALIZATION: drive the real engine with the joiners.
	f := newQuorumEngineFixture(t, netID, state, g0, map[uint64][]blsValidator{
		genesis: genesisSet,
		epoch:   epochSet,
	})

	inner := newFakeInner(10_000_000, ids.Empty, "post-staking-change") // value height far ahead
	wrapped, pos := f.proposeViaWrapper(t, inner)

	// The block carries the LIVE epoch height (7), and the position binds set@7.
	if got := consensuschain.PChainHeightOfForTest(wrapped); got != epoch {
		t.Fatalf("block must carry the live epoch height %d, got %d", epoch, got)
	}
	if pos.ValidatorSetRoot != newValidatorSetRootSource(state, netID).ValidatorSetRoot(epoch) {
		t.Fatal("position must bind the set-root at the epoch height (set@7), not another height")
	}
	if pos.ValidatorSetRoot == newValidatorSetRootSource(state, netID).ValidatorSetRoot(genesis) {
		t.Fatal("test vacuous: set@7 root must differ from set@0 root (the sets must genuinely differ)")
	}

	// proposer g0 self-voted; the JOINERS j1,j2,j3 cast the deciding votes.
	// 4 distinct accepts {g0,j1,j2,j3} = 80/100 stake-at-7 > ⅔ → MUST finalize.
	f.engine.ReceiveVote(signBLSVote(t, j1, pos))
	f.engine.ReceiveVote(signBLSVote(t, j2, pos))
	f.engine.ReceiveVote(signBLSVote(t, j3, pos))

	if !waitForCond(3*time.Second, func() bool { return inner.accepted() == 1 }) {
		t.Fatalf("b2: a block whose ⅔ quorum is post-genesis JOINERS must finalize against the LIVE set@%d "+
			"(VM.Accept=%d). On the broken set@0 path the joiners are unknown → votes dropped → permanent stall.",
			epoch, inner.accepted())
	}

	// The gossiped cert must verify stake-weighted AT THE EPOCH, and must FAIL at
	// genesis (where the joiners are absent) — proving the height is load-bearing.
	if f.certs.count() == 0 {
		t.Fatal("a verified quorum cert must be assembled + gossiped at finality")
	}
	f.certs.mu.Lock()
	lastCert := f.certs.certs[len(f.certs.certs)-1]
	f.certs.mu.Unlock()
	cert, err := consensuschain.UnmarshalQuorumCert(lastCert)
	if err != nil {
		t.Fatalf("decode gossiped cert: %v", err)
	}
	stake := newValidatorStakeSource(state, netID)
	if err := cert.VerifyWeighted(verifier, stake, epoch); err != nil {
		t.Fatalf("cert must verify stake-weighted at the epoch height %d: %v", epoch, err)
	}
	if err := cert.VerifyWeighted(verifier, stake, genesis); err == nil {
		t.Fatal("b2: cert must NOT verify at the genesis height (joiners absent / below ⅔ there) — " +
			"if it does, the epoch height is not actually being used")
	}
	// The cert must contain at least one joiner — proving a post-genesis validator's
	// vote was counted (the exact case the frozen-set path drops).
	var hasJoiner bool
	for i := range cert.Votes {
		if cert.Votes[i].NodeID == j1.nodeID || cert.Votes[i].NodeID == j2.nodeID || cert.Votes[i].NodeID == j3.nodeID {
			hasJoiner = true
			break
		}
	}
	if !hasJoiner {
		t.Fatal("the finality cert must include a post-genesis joiner's vote (it was the deciding quorum)")
	}
}

// constantsPrimaryNetworkID returns the primary-network ID the node uses for
// native-chain validator lookups (ids.Empty). Declared here so the test reads the
// SAME net the production wiring resolves native chains against, without importing
// the constants package for one value.
func constantsPrimaryNetworkID() ids.ID { return ids.Empty }
