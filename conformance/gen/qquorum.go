// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Q-chain's QUORUM corpus, and the Go answer to it.
//
// # Why these vectors carry no bytes
//
// A Q-chain finality certificate is not a field of a block. The block id is
// sha256 of the block's own bytes, so a signature inside it would make the id
// depend on WHO signed and two honest nodes would compute two ids for one
// block — a fork by construction. The certificate is a quorum's statement
// ABOUT a block, and it lives in the bridge.
//
// So no byte string can carry a quorum question across three implementations,
// and every rule about which signatures count — the whole of quasar.go, which
// is where a Q-chain decides what is FINAL — sat outside the differential
// while twenty-three block vectors agreed about parsing.
//
// # What a quorum vector is instead
//
// An EXPERIMENT, named by the vector id, that each implementation runs on its
// own committee with its own keys, reporting only the verdict. The keys differ
// between languages and that is the point: nothing is compared except the
// answer to "does this bridge admit this statement", which is exactly the
// question a consensus disagreement is made of.
//
// Every experiment is run on a FRESH bridge: committee of four, threshold
// three, members node-0..node-3, and one block hash. A vector cannot be judged
// against a committee some earlier vector moved.
package main

import (
	"context"
	"fmt"

	"github.com/luxfi/chains/quantumvm"
	qconfig "github.com/luxfi/chains/quantumvm/config"
	"github.com/luxfi/consensus/protocol/quasar"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// The verdict vocabulary for a quorum vector. Spelled identically in the Rust
// and C++ evaluators, because the runner compares strings.
const (
	qAdmitted  = "ADMITTED"  // the bridge took the statement
	qRefused   = "REFUSED"   // it did not
	qFinalized = "FINALIZED" // the quorum aggregated AND the aggregate verified
	qPending   = "PENDING"   // not enough signatures yet, and no error
	qVerified  = "VERIFIED"  // a certificate checked out over the message
)

// The one committee every quorum vector is run on, and the one block.
const (
	qCommittee = qconfig.CommitteeMin // 4
	qMembers   = qCommittee           // node-0 .. node-3
)

var (
	qQuorumBlock = ids.ID{'q', 'u', 'o', 'r', 'u', 'm'}
	qQuorumHash  = []byte("the block hash")
	qOtherHash   = []byte("another block hash")
)

func qMember(i int) string { return fmt.Sprintf("node-%d", i) }

// qBridge is a finality bridge holding the whole committee: node-0 is this
// node, node-1..node-3 are its peers. Every key is minted by the consensus
// core, which is what keeps a key chosen to cancel against the honest ones out
// of the aggregate.
func qBridge() *quantumvm.Quasar {
	var q *quantumvm.Quasar
	// The consensus core announces a registration on stdout, and this program's
	// stdout is the result stream the runner parses.
	must(qQuiet(func() error {
		b, err := quantumvm.NewQuasarBridge(quantumvm.QuasarBridgeConfig{
			ValidatorID: qMember(0),
			Committee:   qCommittee,
			Logger:      log.NewNoOpLogger(),
		})
		if err != nil {
			return err
		}
		for i := 1; i < qMembers; i++ {
			if err := b.AddValidator(qMember(i), 1); err != nil {
				return err
			}
		}
		q = b
		return nil
	}))
	return q
}

// qSignAs is a peer's signature over a message, made by the consensus core
// that holds that peer's key. The bridge signs only for its own identity, so
// this is how a quorum of one process gets more than one voice — the same door
// the reference exposes (quasar.Quasar.SignMessage).
func qSignAs(q *quantumvm.Quasar, validator string, message []byte) *quasar.QuasarSig {
	sig, err := q.GetQuasar().SignMessage(validator, message)
	must(err)
	return sig
}

// qTracked is a bridge already tracking the block, with this node's signature
// on it — the state every "does it admit a peer's statement" experiment starts
// from.
func qTracked() *quantumvm.Quasar {
	q := qBridge()
	_, err := q.SignBlock(context.Background(), qQuorumBlock, qQuorumHash, 1)
	must(err)
	return q
}

// qQuorumVectors names the experiments. The bytes column is empty for all of
// them: the id IS the vector.
func qQuorumVectors() []Vector {
	ids := []string{
		// The number the whole chain's finality is measured against.
		"Q_QUORUM_THRESHOLD",

		// Admission: what the bridge takes, and what it refuses.
		"Q_QUORUM_HONEST_SIG",
		"Q_QUORUM_SIGN_TWICE",
		"Q_QUORUM_THRESHOLD_FLAG",
		"Q_QUORUM_FOREIGN_MESSAGE",
		"Q_QUORUM_UNKNOWN_VALIDATOR",
		"Q_QUORUM_RESPELLED_NAME",
		"Q_QUORUM_DUPLICATE_SIGNER",
		"Q_QUORUM_PQ_ABSENT",
		"Q_QUORUM_PQ_FORGED",
		"Q_QUORUM_BLS_FORGED",
		"Q_QUORUM_UNKNOWN_BLOCK",

		// Finality: what makes a certificate, and what does not.
		"Q_QUORUM_FINALIZE",
		"Q_QUORUM_BELOW_THRESHOLD",
		"Q_QUORUM_CERT_VERIFIES",
		"Q_QUORUM_CERT_OTHER_MESSAGE",
		"Q_QUORUM_AGG_REPEATED_ID",
		"Q_QUORUM_AGG_UNDERCOUNT",
		"Q_QUORUM_AGG_THRESHOLD_FLAG",
		"Q_QUORUM_AGG_STRANGER",
		"Q_QUORUM_AGG_SHORT_SET",

		// Membership: who may be in the committee at all.
		"Q_QUORUM_COMMITTEE_FULL",
		"Q_QUORUM_DUPLICATE_VALIDATOR",
		"Q_QUORUM_EMPTY_VALIDATOR",
		"Q_QUORUM_SMALL_COMMITTEE",

		// Housekeeping that changes what is admissible.
		"Q_QUORUM_CLEANUP_DROPS_BLOCK",
	}
	v := make([]Vector, 0, len(ids))
	for _, id := range ids {
		v = append(v, Vector{ID: id, Chain: "Q", Op: "quorum", Wire: none})
	}
	return v
}

// evalQQuorum runs one experiment against the Go finality bridge and prints
// what the Go bridge said. Nothing here is copied out of the corpus.
func evalQQuorum(v Vector) Result {
	r := Result{ID: v.ID, Parse: "ok", Kind: "Quasar", Hash: none, Syntactic: none}
	verdict, note := qQuorumCase(v.ID)
	r.Exec = verdict
	r.Note = note
	return r
}

func qQuorumCase(id string) (verdict, note string) {
	ctx := context.Background()

	// verdictOf turns "the bridge took it" into the shared word.
	verdictOf := func(err error) (string, string) {
		if err == nil {
			return qAdmitted, "admitted"
		}
		return qRefused, trim(err.Error())
	}

	switch id {
	// ---- the threshold itself ----

	case "Q_QUORUM_THRESHOLD":
		// ⌊2n/3⌋+1, for the committee of four every quorum vector runs on.
		// Nothing else in the corpus compares this number, and a port computing
		// ⌈2n/3⌉ instead would agree about every signature and still finalize a
		// block one voice earlier than the reference — for the whole life of
		// the chain, on every block.
		q := qBridge()
		return fmt.Sprintf("THRESHOLD=%d", q.GetThreshold()),
			fmt.Sprintf("committee=%d registered=%d", q.Committee(), q.GetActiveValidators())

	// ---- admission ----

	case "Q_QUORUM_SIGN_TWICE":
		// One validator makes ONE statement about one block. The question is
		// asked where it counts: this node signs TWICE and one peer signs once,
		// which is two statements and one short of the threshold of three. A
		// bridge that recorded the second signature has three and finalizes —
		// one validator reaching a quorum by resending.
		q := qBridge()
		_, err := q.SignBlock(ctx, qQuorumBlock, qQuorumHash, 1)
		must(err)
		_, err = q.SignBlock(ctx, qQuorumBlock, qQuorumHash, 1)
		must(err)
		must(q.AddSignature(qQuorumBlock, qSignAs(q, qMember(1), qQuorumHash)))
		_, done, ferr := q.TryFinalize(ctx, qQuorumBlock)
		if ferr != nil {
			return qRefused, trim(ferr.Error())
		}
		if done {
			return "TWO_STATEMENTS", "one validator signing twice reached a quorum of three"
		}
		return "ONE_STATEMENT", "signing twice is one statement, and two of three is not a quorum"

	case "Q_QUORUM_HONEST_SIG":
		// The control. Without it an implementation that refuses everything
		// agrees with the reference on every other admission vector.
		q := qTracked()
		return verdictOf(q.AddSignature(qQuorumBlock, qSignAs(q, qMember(1), qQuorumHash)))

	case "Q_QUORUM_THRESHOLD_FLAG":
		// An honest signature relabelled as a share of a threshold key. The two
		// kinds are checked by two different things and the sender does not get
		// to choose which: a share is checked against the GROUP key by a
		// threshold verifier, and this core holds per-validator keys and nothing
		// else. The reference is in the same position — blsVerifier is nil on
		// this path — so it answers false for a signature that announces itself
		// as a share.
		q := qTracked()
		sig := qSignAs(q, qMember(1), qQuorumHash)
		sig.IsThreshold = true
		return verdictOf(q.AddSignature(qQuorumBlock, sig))

	case "Q_QUORUM_FOREIGN_MESSAGE":
		// A real signature by a real member, over other bytes.
		q := qTracked()
		return verdictOf(q.AddSignature(qQuorumBlock, qSignAs(q, qMember(1), qOtherHash)))

	case "Q_QUORUM_UNKNOWN_VALIDATOR":
		q := qTracked()
		sig := qSignAs(q, qMember(1), qQuorumHash)
		sig.ValidatorID = "node-404"
		return verdictOf(q.AddSignature(qQuorumBlock, sig))

	case "Q_QUORUM_RESPELLED_NAME":
		// A quorum counted over names admitted five spellings of one as five
		// signers. The name is looked up in the committee, so a respelling
		// resolves to no validator.
		q := qTracked()
		sig := qSignAs(q, qMember(0), qQuorumHash)
		sig.ValidatorID = "NODE-0"
		return verdictOf(q.AddSignature(qQuorumBlock, sig))

	case "Q_QUORUM_DUPLICATE_SIGNER":
		// The quorum is counted over signers, so a second signature from one
		// validator would let a single peer reach the threshold by resending.
		q := qTracked()
		return verdictOf(q.AddSignature(qQuorumBlock, qSignAs(q, qMember(0), qQuorumHash)))

	case "Q_QUORUM_PQ_ABSENT":
		// A signature whose post-quantum half is not there at all. The
		// reference verifies the ML-DSA path WHEN PRESENT, so this is the
		// vector that says what "when present" means, in three languages.
		q := qTracked()
		sig := qSignAs(q, qMember(1), qQuorumHash)
		sig.MLDSA = nil
		return verdictOf(q.AddSignature(qQuorumBlock, sig))

	case "Q_QUORUM_PQ_FORGED":
		// Present, and one bit turned over. Half a statement is not a statement.
		q := qTracked()
		sig := qSignAs(q, qMember(1), qQuorumHash)
		flipped := append([]byte(nil), sig.MLDSA...)
		flipped[0] ^= 0xFF
		sig.MLDSA = flipped
		return verdictOf(q.AddSignature(qQuorumBlock, sig))

	case "Q_QUORUM_BLS_FORGED":
		q := qTracked()
		sig := qSignAs(q, qMember(1), qQuorumHash)
		flipped := append([]byte(nil), sig.BLS...)
		flipped[0] ^= 0xFF
		sig.BLS = flipped
		return verdictOf(q.AddSignature(qQuorumBlock, sig))

	case "Q_QUORUM_UNKNOWN_BLOCK":
		// A signature for a block this node is not tracking. Creating the entry
		// on demand would let any peer make this node track anything.
		q := qBridge()
		return verdictOf(q.AddSignature(qQuorumBlock, qSignAs(q, qMember(1), qQuorumHash)))

	// ---- finality ----

	case "Q_QUORUM_FINALIZE":
		q, err := qQuorumOf(3)
		must(err)
		_, done, ferr := q.TryFinalize(ctx, qQuorumBlock)
		if ferr != nil {
			return qRefused, trim(ferr.Error())
		}
		if !done {
			return qPending, "three of three did not finalize"
		}
		return qFinalized, "three distinct signatures aggregated and the aggregate verified"

	case "Q_QUORUM_BELOW_THRESHOLD":
		// Two of three. Reaching the count is necessary; not reaching it is
		// not an error, it is a block still gathering.
		q, err := qQuorumOf(2)
		must(err)
		_, done, ferr := q.TryFinalize(ctx, qQuorumBlock)
		if ferr != nil {
			return qRefused, trim(ferr.Error())
		}
		if done {
			return qFinalized, "two signatures finalized a threshold of three"
		}
		return qPending, "two of three"

	case "Q_QUORUM_CERT_VERIFIES":
		q, agg := qCert()
		if q.VerifyAggregate(ctx, qQuorumHash, agg) {
			return qVerified, "the certificate checks out over the block it names"
		}
		return qRefused, "the certificate did not verify over its own block"

	case "Q_QUORUM_CERT_OTHER_MESSAGE":
		q, agg := qCert()
		if q.VerifyAggregate(ctx, qOtherHash, agg) {
			return qVerified, "a certificate verified over bytes it does not cover"
		}
		return qRefused, "the certificate does not cover these bytes"

	case "Q_QUORUM_AGG_REPEATED_ID":
		// BLS is linear, so a repeated id adds the same key again: t copies of
		// one validator yield t·pk, which that validator's own signature scaled
		// to t·σ satisfies. Counting DISTINCT ids is what makes the threshold
		// mean "t validators agreed".
		q, agg := qCert()
		agg.ValidatorIDs = []string{qMember(0), qMember(0), qMember(0)}
		return qVerdictOfAggregate(q, agg)

	case "Q_QUORUM_AGG_UNDERCOUNT":
		// The count travels inside the message the sender chose. A sender that
		// does not even CLAIM a quorum is not offering one.
		q, agg := qCert()
		agg.SignerCount = q.GetThreshold() - 1
		return qVerdictOfAggregate(q, agg)

	case "Q_QUORUM_AGG_THRESHOLD_FLAG":
		// Nor does the sender get to choose HOW its aggregate is checked.
		q, agg := qCert()
		agg.IsThreshold = true
		return qVerdictOfAggregate(q, agg)

	case "Q_QUORUM_AGG_STRANGER":
		q, agg := qCert()
		agg.ValidatorIDs = []string{qMember(0), qMember(1), "node-404"}
		return qVerdictOfAggregate(q, agg)

	case "Q_QUORUM_AGG_SHORT_SET":
		// Distinct, registered, and fewer than the threshold.
		q, agg := qCert()
		agg.ValidatorIDs = []string{qMember(0), qMember(1)}
		return qVerdictOfAggregate(q, agg)

	// ---- membership ----

	case "Q_QUORUM_COMMITTEE_FULL":
		// The committee is the set the threshold was derived from. Registering
		// more members than it declares makes the threshold a quorum of a
		// committee that no longer exists.
		q := qBridge()
		return verdictOf(q.AddValidator("node-4", 1))

	case "Q_QUORUM_DUPLICATE_VALIDATOR":
		// Registering an id twice hands the core a fresh key for it, which
		// silently invalidates every signature that validator has contributed.
		q := qBridge()
		return verdictOf(q.AddValidator(qMember(1), 1))

	case "Q_QUORUM_EMPTY_VALIDATOR":
		q := qBridge()
		return verdictOf(q.AddValidator("", 1))

	case "Q_QUORUM_SMALL_COMMITTEE":
		// Below four the quorum ⌊2n/3⌋+1 is the entire committee: one absent
		// validator is a halt and one dishonest validator is the decision.
		var err error
		_ = qQuiet(func() error {
			_, err = quantumvm.NewQuasarBridge(quantumvm.QuasarBridgeConfig{
				ValidatorID: qMember(0),
				Committee:   qconfig.CommitteeMin - 1,
				Logger:      log.NewNoOpLogger(),
			})
			return nil
		})
		if err == nil {
			return qAdmitted, "a committee that tolerates no fault was accepted"
		}
		return qRefused, trim(err.Error())

	// ---- housekeeping ----

	case "Q_QUORUM_CLEANUP_DROPS_BLOCK":
		// Cleanup drops every block below the caller's finalized frontier,
		// finalized or not: a block beneath it will never gather another
		// signature. So a signature arriving afterwards has nothing to join.
		q := qTracked()
		q.Cleanup(2)
		return verdictOf(q.AddSignature(qQuorumBlock, qSignAs(q, qMember(1), qQuorumHash)))
	}

	return VInternal, "unknown quorum vector " + id
}

// qQuorumOf is the tracked block carrying `n` distinct verified signatures.
func qQuorumOf(n int) (*quantumvm.Quasar, error) {
	q := qTracked() // node-0's signature
	for i := 1; i < n; i++ {
		if err := q.AddSignature(qQuorumBlock, qSignAs(q, qMember(i), qQuorumHash)); err != nil {
			return nil, err
		}
	}
	return q, nil
}

// qCert is a finalized block's certificate, and the bridge that made it. Every
// aggregate vector below is a tampered copy of this one, so each differs from a
// certificate that verifies in exactly the one way it names.
func qCert() (*quantumvm.Quasar, *quasar.AggregatedSignature) {
	q, err := qQuorumOf(3)
	must(err)
	agg, done, err := q.TryFinalize(context.Background(), qQuorumBlock)
	must(err)
	if !done {
		panic("quantumvm corpus: a quorum of three did not finalize")
	}
	return q, agg
}

func qVerdictOfAggregate(q *quantumvm.Quasar, agg *quasar.AggregatedSignature) (string, string) {
	if q.VerifyAggregate(context.Background(), qQuorumHash, agg) {
		return qVerified, "the tampered certificate verified"
	}
	return qRefused, "the tampered certificate did not verify"
}
