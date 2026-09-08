// SPDX-License-Identifier: BSD-3-Clause-Eco

// The O-chain half of the corpus, and the Go reference's answers for it.
//
// WHERE THE CHAIN IS. `chains/oraclevm` is 213 lines and none of them are the
// chain: it is a re-export, and the O-Chain is `github.com/luxfi/oracle/vm` —
// 1628 lines. Reading the shim's size as the chain's size understates it by an
// order of magnitude, so this file imports the shim (which is what luxd loads,
// through type aliases) and reaches the one exported function the shim does
// not re-export, ComputeRequestID, from the canonical module.
//
// WHAT THIS CHAIN PUTS ON THE WIRE IS JSON. Not a codec's frame: `ParseBlock`
// is `json.Unmarshal` and `Bytes` is `json.Marshal`, so the O-chain's wire is
// whatever `encoding/json` writes for its structs — CB58 for an id, base64 for
// a byte slice, a list of numbers for a fixed array, RFC 3339 for a time, and
// field order taken from the struct declaration. Every one of those is a
// consensus surface here, and none of them is written down anywhere but in the
// Go standard library's behaviour.
//
// AND THE BLOCK ID IS A HASH OF THAT JSON, RE-MARSHALLED. `computeID` marshals
// the parsed block again and takes sha256 of the result — NOT of the bytes it
// was handed. Two consequences the vectors below pin:
//
//   - a block does not round-trip its own id. `BuildBlock` hashes the block
//     while `ID_` is still empty and then writes that id INTO the struct, so
//     `Bytes()` is not the preimage of `ID()` and re-parsing a block the chain
//     wrote gives it a different id. O_BLOCK_ID_SET is that block.
//   - a member `encoding/json` accepts but omitempty later drops changes the
//     id: `"observations":[]` parses to an empty slice, which re-marshals to
//     nothing at all. O_BLOCK_EMPTY_ARRAY is the same block as O_BLOCK_EMPTY
//     with two extra bytes on the wire and the SAME id.
//
// WHAT VERIFY DECIDES: nothing. `Block.Verify` is `return nil` with no
// condition in it, so on block vectors `syntactic` and `exec` are OK for
// everything that parses and MALFORMED for everything that does not. That is
// not a gap in the corpus, it is the chain: what O actually decides lives in
// the request/record/observation plane, and the other four ops below are
// where this chain's refusals are.
//
// THE GENESIS BLOCK'S ID IS NOT A FUNCTION OF THE GENESIS. `Initialize` builds
// it with `time.Unix(genesis.Timestamp, 0)`, which is LOCAL time, and
// `MarshalJSON` writes the offset. Measured on this box: the same genesis
// bytes give 6cf00752… under TZ unset (-08:00) and f4970a27… under
// Asia/Kolkata. Two validators in two timezones derive different genesis ids
// from one genesis file and are on different chains from block zero. So no
// vector here carries that id — a corpus answer that changes with the machine
// is not an answer — and O_GENESIS pins the genesis by the one thing that IS a
// function of its bytes: the canonical re-marshal of the parsed struct.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/chains/oraclevm"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/artifacts"
	oracle "github.com/luxfi/oracle/vm"
	"github.com/luxfi/runtime"
	luxvm "github.com/luxfi/vm"
)

// The chain every O vector belongs to. Unlike Q, Z and F the O-chain hashes
// nothing but its own JSON into a block id, so this number does not enter any
// derivation — it is here so the identity vector can say which chain the
// evaluator was built for, and so a port configured for another one is named
// rather than agreeing by accident.
const oChain = 79 // 'O'

// The feeds the observation vectors are judged against, and the operators each
// admits. It is a corpus contract exactly as Z's genesis is: an implementation
// seeded with different feeds refuses observations this one accepts, and the
// disagreement would read as a rule when it is a configuration. O_GENESIS
// carries these bytes and asks every implementation what it parsed out of
// them, so "we applied the same configuration" is compared rather than assumed.
//
// Written by hand, not marshalled, because these bytes ARE the vector: a
// re-marshal would make the corpus's genesis a function of a struct that could
// change under it.
const oGenesisCfg = `{"version":1,"message":"conformance","timestamp":1000,` +
	`"initialFeeds":[{"id":"2Kw2XL8QVSQHwJYKpWLktaxtwKuz7iYF5pqcauUHpmcrSjRPp",` +
	`"name":"lux-usd","description":"","sources":null,"updateFreq":0,` +
	`"policyHash":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],` +
	`"operators":["NodeID-6Jswqk47s9PUcyCc88MMVwzgvHQxfLnA"],` +
	`"createdAt":"1970-01-01T00:00:05Z","status":"active","metadata":null}]}`

// oOperator is the one operator lux-usd admits: the node id whose twenty bytes
// are all 0x01, which is what the CB58 above decodes to.
func oOperator() ids.NodeID { return nodeID(1) }

// oFeed is the feed lux-usd's id: the thirty-two bytes that are all 0x03,
// which is what the CB58 above decodes to.
func oFeed() ids.ID { return id(3) }

// oTime is a fixed instant in UTC. Every timestamp in the corpus is built here
// so that no vector's bytes depend on the machine's timezone — which, on this
// chain, is not a stylistic point (see the note on the genesis block above).
func oTime(sec int64, nsec int64) time.Time { return time.Unix(sec, nsec).UTC() }

// oFresh is a timestamp far enough ahead that `time.Since` is negative on it.
// SubmitObservation's staleness rule reads the clock, so an observation with a
// fixed past timestamp is stale forever and one with this timestamp is fresh
// for the next three hundred years. Both are wanted: the pair is what separates
// the staleness rule from the authorisation rule underneath it.
func oFresh() time.Time { return time.Date(2400, 1, 1, 0, 0, 0, 0, time.UTC) }

// ---------------------------------------------------------------------------
// Building the wire.
// ---------------------------------------------------------------------------

func oObservation(feed ids.ID, op ids.NodeID, ts time.Time, seed byte) *oraclevm.Observation {
	obs := &oraclevm.Observation{
		FeedID:     feed,
		Value:      zBytes(seed, 8),
		Timestamp:  ts,
		OperatorID: op,
		Scheme:     1, // ML-DSA-65: the only scheme a strict-PQ O-chain admits
		Signature:  zBytes(seed+1, 16),
	}
	copy(obs.SourceMeta[:], zBytes(seed+2, 32))
	return obs
}

func oAggregated(feed ids.ID, epoch uint64, seed byte) *oraclevm.AggregatedValue {
	return &oraclevm.AggregatedValue{
		FeedID:       feed,
		Epoch:        epoch,
		Value:        zBytes(seed, 8),
		Timestamp:    oTime(2000, 0),
		Observations: 3,
	}
}

func oFeedRecord(feed ids.ID, name string) *oraclevm.Feed {
	return &oraclevm.Feed{
		ID:        feed,
		Name:      name,
		Operators: []ids.NodeID{oOperator()},
		CreatedAt: oTime(5, 0),
		Status:    "active",
	}
}

// oBlockWire writes a block the way the chain writes one. `ID_` is left empty
// because that is the state `BuildBlock` marshals in, and it is what makes the
// preimage the id is taken over.
func oBlockWire(parent ids.ID, height uint64, ts time.Time,
	obs []*oraclevm.Observation, agg []*oraclevm.AggregatedValue,
	feeds []*oraclevm.Feed) []byte {
	return oBlockWithAttestations(parent, height, ts, obs, agg, feeds, nil)
}

func oBlockWithAttestations(parent ids.ID, height uint64, ts time.Time,
	obs []*oraclevm.Observation, agg []*oraclevm.AggregatedValue,
	feeds []*oraclevm.Feed, att []*artifacts.OracleAttestation) []byte {
	b := &oraclevm.Block{
		ID_:          ids.Empty,
		ParentID_:    parent,
		Height_:      height,
		Timestamp_:   ts,
		Observations: obs,
		Aggregations: agg,
		FeedUpdates:  feeds,
		Attestations: att,
	}
	w, err := json.Marshal(b)
	must(err)
	return w
}

func oParent() ids.ID { return id(7) }

// oNativeID is an id whose first thirty-one bytes are zero and whose last is
// the letter of a native chain. `ids.ID.MarshalJSON` writes those as the
// LETTER — "P" — and not as CB58, and `UnmarshalJSON` reads them back. An
// implementation that only knows CB58 writes a different block and derives a
// different id for it.
func oNativeID(letter byte) ids.ID {
	var i ids.ID
	i[31] = letter
	return i
}

func oVectors() []Vector {
	obs1 := oObservation(oFeed(), oOperator(), oTime(1500, 0), 0x20)
	obs2 := oObservation(oFeed(), nodeID(2), oTime(1600, 0), 0x30)
	agg := oAggregated(oFeed(), 1, 0x40)
	feed := oFeedRecord(id(4), "eth-usd")

	empty := oBlockWire(oParent(), 1, oTime(1000, 0), nil, nil, nil)

	// The same block, written the way the chain writes one AFTER it has
	// computed the id: `Bytes()` marshals with `ID_` filled in. Its id is
	// therefore not the id it carries, which is the round-trip this chain does
	// not have.
	withID := func() []byte {
		b := &oraclevm.Block{ParentID_: oParent(), Height_: 1, Timestamp_: oTime(1000, 0)}
		b.ID_ = oBlockID(oBlockWire(oParent(), 1, oTime(1000, 0), nil, nil, nil))
		w, err := json.Marshal(b)
		must(err)
		return w
	}()

	v := []Vector{
		vec("O_GENESIS", "O", "genesis", []byte(oGenesisCfg)),

		vec("O_BLOCK_EMPTY", "O", "block", empty),
		vec("O_BLOCK_ZERO_HEIGHT", "O", "block",
			oBlockWire(ids.Empty, 0, oTime(1000, 0), nil, nil, nil)),
		vec("O_BLOCK_OBSERVATION", "O", "block",
			oBlockWire(oParent(), 1, oTime(1000, 0),
				[]*oraclevm.Observation{obs1}, nil, nil)),
		vec("O_BLOCK_OBSERVATION_TWO", "O", "block",
			oBlockWire(oParent(), 1, oTime(1000, 0),
				[]*oraclevm.Observation{obs1, obs2}, nil, nil)),
		vec("O_BLOCK_AGGREGATION", "O", "block",
			oBlockWire(oParent(), 2, oTime(1000, 0), nil,
				[]*oraclevm.AggregatedValue{agg}, nil)),
		vec("O_BLOCK_FEED_UPDATE", "O", "block",
			oBlockWire(oParent(), 3, oTime(1000, 0), nil, nil,
				[]*oraclevm.Feed{feed})),
		// The artifact this chain hands the X-chain, carried in a block. The
		// pair is the omitempty rule read twice: the byte slices are written
		// on the first and gone on the second, and the fixed array is written
		// on both.
		vec("O_BLOCK_ATTESTATION", "O", "block",
			oBlockWithAttestations(oParent(), 5, oTime(1000, 0), nil, nil, nil,
				[]*artifacts.OracleAttestation{oAttestation(0x90, 8, 16, 24)})),
		vec("O_BLOCK_ATTESTATION_BARE", "O", "block",
			oBlockWithAttestations(oParent(), 5, oTime(1000, 0), nil, nil, nil,
				[]*artifacts.OracleAttestation{oAttestation(0x90, 0, 0, 0)})),
		// The attestation whose fixed array is ALL ZERO. Without it the
		// omitempty-on-an-array rule is a claim and not a vector: every other
		// attestation here carries a non-zero commitment, so a writer that
		// dropped an empty array would still write all of them and the run
		// would stay green. Proven, by breaking the Rust writer that way and
		// watching nothing happen.
		vec("O_BLOCK_ATTESTATION_ZERO_COMMITMENT", "O", "block",
			oBlockWithAttestations(oParent(), 5, oTime(1000, 0), nil, nil, nil,
				[]*artifacts.OracleAttestation{oZeroCommitted()})),

		vec("O_BLOCK_ATTESTATION_TWO", "O", "block",
			oBlockWithAttestations(oParent(), 5, oTime(1000, 0), nil, nil, nil,
				[]*artifacts.OracleAttestation{oAttestation(0x90, 8, 16, 24), oAttestation(0xA0, 4, 0, 0)})),

		vec("O_BLOCK_MIXED", "O", "block",
			oBlockWire(oParent(), 4, oTime(1000, 0),
				[]*oraclevm.Observation{obs1},
				[]*oraclevm.AggregatedValue{agg},
				[]*oraclevm.Feed{feed})),

		// The encoder's own edges, each a place two implementations can write
		// one value differently and derive two ids for one block.
		//
		// omitempty on a []byte: present when it carries bytes, absent when it
		// is empty, and an implementation that wrote `"aggProof":""` for the
		// empty one names a different block. The pair is the check.
		vec("O_BLOCK_AGG_PROOFS", "O", "block",
			oBlockWire(oParent(), 2, oTime(1000, 0), nil,
				[]*oraclevm.AggregatedValue{oProved(0x41, 8, 4)}, nil)),
		vec("O_BLOCK_AGG_EMPTY_PROOF", "O", "block",
			oBlockWire(oParent(), 2, oTime(1000, 0), nil,
				[]*oraclevm.AggregatedValue{oProved(0x41, 0, 0)}, nil)),

		// A feed carrying every member that has a rendering of its own: a
		// non-nil list of strings, a duration in nanoseconds, a map — whose
		// keys come out SORTED and are given here out of order — and a
		// description holding the three runes Marshal escapes.
		vec("O_BLOCK_FEED_RICH", "O", "block",
			oBlockWire(oParent(), 3, oTime(1000, 0), nil, nil,
				[]*oraclevm.Feed{oRichFeed()})),

		// A nil []byte is `null` and an empty one is `""`. Both read back, and
		// they are not the same block.
		vec("O_BLOCK_OBS_NULL_VALUE", "O", "block",
			oBlockWire(oParent(), 1, oTime(1000, 0),
				[]*oraclevm.Observation{oNilValued()}, nil, nil)),
		vec("O_BLOCK_OBS_EMPTY_VALUE", "O", "block",
			oBlockWire(oParent(), 1, oTime(1000, 0),
				[]*oraclevm.Observation{oEmptyValued()}, nil, nil)),

		// A Go [32]byte reads a list of ANY length: the elements past the
		// thirty-second are discarded without being type-checked, and the ones
		// it never reached stay zero. Both are refusals a hand-written reader
		// is likely to invent, and neither is one the chain makes.
		vec("O_BLOCK_SOURCEMETA_SHORT", "O", "block",
			oSourceMeta(obs1, "[1,2,3]")),
		vec("O_BLOCK_SOURCEMETA_LONG", "O", "block",
			oSourceMeta(obs1, oNumbers(40))),

		// The block the chain WROTE, id and all. Its id is a different id.
		vec("O_BLOCK_ID_SET", "O", "block", withID),

		// A parent whose id renders as a chain LETTER rather than as CB58.
		vec("O_BLOCK_NATIVE_PARENT", "O", "block",
			oBlockWire(oNativeID('P'), 1, oTime(1000, 0), nil, nil, nil)),

		// Sub-second time. Go trims trailing zeros from the fraction, so this
		// is "…:40.5Z" and not "…:40.500000000Z", and an implementation that
		// writes the full nine digits derives a different id.
		vec("O_BLOCK_TIME_HALF_SECOND", "O", "block",
			oBlockWire(oParent(), 1, oTime(1000, 500000000), nil, nil, nil)),
		vec("O_BLOCK_TIME_NANO", "O", "block",
			oBlockWire(oParent(), 1, oTime(1000, 123456789), nil, nil, nil)),

		// A timestamp that is not in UTC. Go keeps the offset it was given
		// rather than normalising, so the re-marshal — and the id — is over
		// "+05:30" and not over the same instant written as Z.
		vec("O_BLOCK_TIME_OFFSET", "O", "block",
			[]byte(`{"id":"11111111111111111111111111111111LpoYY",`+
				`"parentID":"`+oParent().String()+`",`+
				`"height":1,"timestamp":"1970-01-01T05:46:40+05:30"}`)),

		// `encoding/json` ignores a member the struct does not name. A reader
		// that refuses unknown members refuses a block this chain accepts.
		vec("O_BLOCK_UNKNOWN_MEMBER", "O", "block",
			oInsert(empty, `"round":4,`)),
		// Two members naming one field: the LAST one wins, silently. The
		// pair below is what says WHICH, and neither alone would: with the
		// intruder FIRST the original wins and the block is unchanged, with
		// it LAST the block is a different block. A reader that took the
		// first member disagrees on both rows and in opposite directions.
		vec("O_BLOCK_DUPLICATE_HEIGHT_FIRST", "O", "block",
			oInsert(empty, `"height":9,`)),
		vec("O_BLOCK_DUPLICATE_HEIGHT_LAST", "O", "block",
			oAppend(empty, `"height":9`)),
		// A member name that differs from the field's tag only by case.
		// `encoding/json` matches it, case-insensitively, when nothing matches
		// exactly.
		vec("O_BLOCK_FOLDED_MEMBER", "O", "block",
			[]byte(strings.Replace(string(empty), `"height":`, `"HEIGHT":`, 1))),
		// The exact name and the folded one, in the same object, with the
		// FOLDED one last. Go's decoder walks the members in document order
		// and decodes each one that resolves to the field, so exactness
		// decides WHICH FIELD a name reaches and POSITION decides which value
		// survives — a reader that preferred the exact member wherever it sat
		// would keep 1 where the chain keeps 9. Nothing anywhere said which
		// of those two rules the chain has until this vector asked.
		vec("O_BLOCK_FOLDED_AFTER_EXACT", "O", "block",
			oAppend(empty, `"HEIGHT":9`)),
		vec("O_BLOCK_EXACT_AFTER_FOLDED", "O", "block",
			oInsert(empty, `"HEIGHT":9,`)),
		// null for a member leaves it at its zero value and is not an error.
		vec("O_BLOCK_NULL_MEMBER", "O", "block",
			oInsert(empty, `"observations":null,`)),
		// An empty list parses, and then omitempty drops it on the way back
		// out — so these bytes and O_BLOCK_EMPTY's are one id.
		vec("O_BLOCK_EMPTY_ARRAY", "O", "block",
			oInsert(empty, `"observations":[],`)),

		// A number reaching an integer must BE one: Go hands the literal to
		// strconv, so a float, an exponent, a negative and a value past the
		// width are all refused, and the largest value that FITS is taken. The
		// five are one rule read at five points, and a reader that ran the
		// literal through a double would take three of them.
		vec("O_BLOCK_HEIGHT_FLOAT", "O", "block", oHeight(empty, "1.0")),
		vec("O_BLOCK_HEIGHT_EXPONENT", "O", "block", oHeight(empty, "1e2")),
		vec("O_BLOCK_HEIGHT_NEGATIVE", "O", "block", oHeight(empty, "-1")),
		vec("O_BLOCK_HEIGHT_MAX", "O", "block", oHeight(empty, "18446744073709551615")),
		vec("O_BLOCK_HEIGHT_OVER_MAX", "O", "block", oHeight(empty, "18446744073709551616")),
		// A scheme past the width of the byte it is read into.
		vec("O_BLOCK_SCHEME_OVER_BYTE", "O", "block",
			[]byte(strings.Replace(
				string(oBlockWire(oParent(), 1, oTime(1000, 0),
					[]*oraclevm.Observation{obs1}, nil, nil)),
				`"scheme":1`, `"scheme":256`, 1))),
		// A CB58 string whose checksum does not check.
		vec("O_BLOCK_PARENT_BAD_CHECKSUM", "O", "block",
			[]byte(strings.Replace(string(empty), oParent().String(),
				oCorrupt(oParent().String()), 1))),
		// A byte slice that is not base64.
		vec("O_BLOCK_VALUE_BAD_BASE64", "O", "block",
			[]byte(strings.Replace(
				string(oBlockWire(oParent(), 1, oTime(1000, 0),
					[]*oraclevm.Observation{obs1}, nil, nil)),
				`"value":"`+oB64(obs1.Value)+`"`, `"value":"!!!!"`, 1))),
		// A timestamp that is not RFC 3339.
		vec("O_BLOCK_TIME_NOT_RFC3339", "O", "block",
			[]byte(strings.Replace(string(empty), `"1970-01-01T00:16:40Z"`,
				`"the tenth of never"`, 1))),

		vec("O_BLOCK_NOT_JSON", "O", "block", []byte("this is not a block")),
		vec("O_BLOCK_EMPTY_WIRE", "O", "block", nil),
		vec("O_BLOCK_ONE_BYTE", "O", "block", []byte{'{'}),
	}

	v = append(v, oArtifactIDVectors()...)
	v = append(v, oRequestIDVectors()...)
	v = append(v, oRequestVectors()...)
	v = append(v, oCommitVectors()...)
	v = append(v, oObservationVectors()...)

	oAssert(v)
	return v
}

// oHeight rewrites a block's height member with a literal, so the number rules
// are read against the same block every time.
func oHeight(wire []byte, literal string) []byte {
	s := string(wire)
	if !strings.Contains(s, `"height":1,`) {
		panic("the height member is not written as these vectors assume")
	}
	return []byte(strings.Replace(s, `"height":1,`, `"height":`+literal+`,`, 1))
}

// oAttestation is the artifact an O-chain hands the X-chain: the aggregated
// value for one feed and epoch, with the window it is good for.
//
// `ValueCommitment` carries `omitempty` and is a [32]byte, and omitempty does
// NOTHING to a Go array — an array has no empty form — so it is written on
// every attestation, all zeros included, while the three byte SLICES beside it
// vanish when they are empty. The two rules sit in adjacent fields and only
// one of them fires.
func oAttestation(seed byte, value, proof, cert int) *artifacts.OracleAttestation {
	a := &artifacts.OracleAttestation{
		Version_:  1,
		SigSuite_: 1,
		DomainID_: id(0x0A),
		FeedID:    oFeed(),
		Epoch:     7,
		Value:     zBytes(seed, value),
		AggProof:  zBytes(seed+1, proof),
		QuorumCert: zBytes(seed+2, cert),
		ValidFrom: oTime(1000, 0),
		ValidTo:   oTime(2000, 0),
	}
	copy(a.ValueCommitment[:], zBytes(seed+3, 32))
	copy(a.PolicyHash[:], zBytes(seed+4, 32))
	return a
}

// oZeroCommitted is an attestation whose value commitment is all zero and
// whose byte slices are all empty: everything omitempty could touch is at its
// zero value at once, so the array is written and the slices are not.
func oZeroCommitted() *artifacts.OracleAttestation {
	a := oAttestation(0x90, 0, 0, 0)
	a.ValueCommitment = [32]byte{}
	return a
}

// oProved is an aggregation carrying the two proofs that are omitempty: with
// non-zero lengths both are written, and with zero lengths neither is.
func oProved(seed byte, proof, cert int) *oraclevm.AggregatedValue {
	a := oAggregated(oFeed(), 2, seed)
	a.AggProof = zBytes(seed+1, proof)
	a.QuorumCert = zBytes(seed+2, cert)
	return a
}

// oRichFeed carries every member of a Feed that has a rendering of its own.
// The metadata keys are given out of order because Marshal sorts them, and the
// description holds the three runes it escapes.
func oRichFeed() *oraclevm.Feed {
	f := oFeedRecord(id(5), "btc-usd")
	f.Description = "spot <b> & \"mid\" > 0"
	f.Sources = []string{"a.invalid", "b.invalid"}
	f.UpdateFreq = 1500 * time.Millisecond
	f.Operators = []ids.NodeID{oOperator(), nodeID(2)}
	f.Metadata = map[string]string{"z": "last", "a": "first", "m": "middle"}
	copy(f.PolicyHash[:], zBytes(0x80, 32))
	return f
}

func oNilValued() *oraclevm.Observation {
	o := oObservation(oFeed(), oOperator(), oTime(1500, 0), 0x20)
	o.Value = nil
	o.Signature = nil
	return o
}

func oEmptyValued() *oraclevm.Observation {
	o := oObservation(oFeed(), oOperator(), oTime(1500, 0), 0x20)
	o.Value = []byte{}
	o.Signature = []byte{}
	return o
}

// oSourceMeta replaces an observation's fixed-array member with a list of a
// different length, which is a thing the Go reader takes rather than refuses.
func oSourceMeta(obs *oraclevm.Observation, list string) []byte {
	wire := oBlockWire(oParent(), 1, oTime(1000, 0),
		[]*oraclevm.Observation{obs}, nil, nil)
	want := `"sourceMetaHash":` + oNumbers(32)
	if !strings.Contains(string(wire), want) {
		panic("the source-meta member is not written as this vector assumes")
	}
	return []byte(strings.Replace(string(wire), want, `"sourceMetaHash":`+list, 1))
}

// oNumbers is the list Marshal writes for n bytes of the value obs1 carries.
func oNumbers(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "34" // the 0x22 every byte of that member holds
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// oInsert puts a member at the front of a JSON object's member list, which is
// where a duplicate has to sit for "the last one wins" to be the thing under
// test.
func oInsert(wire []byte, member string) []byte {
	return []byte("{" + member + string(wire)[1:])
}

// oAppend puts a member at the END of a JSON object's member list, which is
// where a duplicate has to sit for it to be the one that wins.
func oAppend(wire []byte, member string) []byte {
	s := string(wire)
	return []byte(s[:len(s)-1] + "," + member + "}")
}

func oB64(b []byte) string {
	w, err := json.Marshal(b)
	must(err)
	var s string
	must(json.Unmarshal(w, &s))
	return s
}

// oCorrupt changes one character of a CB58 string to another base-58 digit, so
// the string is still well formed base 58 and only its checksum is wrong.
func oCorrupt(s string) string {
	b := []byte(s)
	if b[3] == 'A' {
		b[3] = 'B'
	} else {
		b[3] = 'A'
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// The request id, which is the whole of what makes an O request deterministic.
//
// A request is minted on the P-chain and executed on O, and the two sides
// agree on WHICH request only because both derive the same 32 bytes from the
// same five arguments. An implementation that folded step and retry in the
// other order, or wrote them little-endian, would execute one chain's requests
// under another chain's names. There is no wire here: the arguments are the
// vector, pipe-separated, the way D's derivations are.
//
//	requestid  serviceHex | sessionHex | txHex | step | retry
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// The identity an attestation takes when it leaves this chain.
//
// An OracleAttestation is what the O-chain hands the X-chain, and X names it
// by `sha256("LUX:OracleAttestation:v1" ‖ feedID ‖ be64(epoch))` — not by
// anything in the attestation's own bytes. So two implementations can agree on
// every byte of a block and still file its attestations under different names,
// which is a fork of the artifact plane that no block vector can see.
//
//	artifactid  feedHex | epoch
// ---------------------------------------------------------------------------

func oArtifactIDVectors() []Vector {
	feed := hexID32(3)
	return []Vector{
		vec("O_ARTIFACTID_ZERO", "O", "artifactid", dArgs(hexID32(0), "0")),
		vec("O_ARTIFACTID_BASE", "O", "artifactid", dArgs(feed, "7")),
		// The epoch and the feed each move it on their own. One that ignored
		// either would let two attestations share a name, and a name is what a
		// light client checks an oracle answer under.
		vec("O_ARTIFACTID_OTHER_EPOCH", "O", "artifactid", dArgs(feed, "8")),
		vec("O_ARTIFACTID_OTHER_FEED", "O", "artifactid", dArgs(hexID32(4), "7")),
		// The largest epoch, which is where a counter written in the wrong
		// width stops agreeing.
		vec("O_ARTIFACTID_EPOCH_MAX", "O", "artifactid",
			dArgs(feed, "18446744073709551615")),
		vec("O_ARTIFACTID_BAD_HEX", "O", "artifactid", dArgs("zz", "7")),
	}
}

func oRequestIDVectors() []Vector {
	svc, ses, tx := hexID32(0x11), hexID32(0x12), hexID32(0x13)
	zero := hexID32(0)
	return []Vector{
		vec("O_REQUESTID_ZERO", "O", "requestid", dArgs(zero, zero, zero, "0", "0")),
		vec("O_REQUESTID_BASE", "O", "requestid", dArgs(svc, ses, tx, "0", "0")),
		vec("O_REQUESTID_STEP", "O", "requestid", dArgs(svc, ses, tx, "1", "0")),
		vec("O_REQUESTID_RETRY", "O", "requestid", dArgs(svc, ses, tx, "0", "1")),
		vec("O_REQUESTID_STEP_MAX", "O", "requestid", dArgs(svc, ses, tx, "4294967295", "0")),
		vec("O_REQUESTID_RETRY_MAX", "O", "requestid", dArgs(svc, ses, tx, "0", "4294967295")),
		// The three ids each moved on their own. Each must derive a different
		// request id: one that ignored any of them would let two requests
		// share a name, and a name is what the executors sign against.
		vec("O_REQUESTID_OTHER_SERVICE", "O", "requestid", dArgs(hexID32(0x14), ses, tx, "0", "0")),
		vec("O_REQUESTID_OTHER_SESSION", "O", "requestid", dArgs(svc, hexID32(0x14), tx, "0", "0")),
		vec("O_REQUESTID_OTHER_TX", "O", "requestid", dArgs(svc, ses, hexID32(0x14), "0", "0")),
		// The service and session ids swapped. The preimage concatenates them
		// in one order and nothing separates them, so an implementation that
		// wrote them the other way round derives THIS id for the base vector.
		vec("O_REQUESTID_SWAPPED_IDS", "O", "requestid", dArgs(ses, svc, tx, "0", "0")),
		vec("O_REQUESTID_BAD_HEX", "O", "requestid", dArgs("zz", ses, tx, "0", "0")),
		vec("O_REQUESTID_STEP_OVERFLOW", "O", "requestid", dArgs(svc, ses, tx, "4294967296", "0")),
	}
}

// ---------------------------------------------------------------------------
// Requests, records and the commitment over them.
//
// A `request` vector is an OracleRequest as JSON, offered to a chain that
// holds nothing. A `commit` vector carries a request AND the records executed
// against it, and its answer is the Merkle root the chain commits — the one
// number a light client checks an oracle answer against, and the one an
// implementation can get wrong without ever disagreeing about a block.
// ---------------------------------------------------------------------------

// oRequest builds a request whose id is the one its own arguments derive, so
// that the deterministic-id rule passes and the rules under it are reachable.
func oRequest(kind oraclevm.RequestKind, execs []ids.NodeID) *oraclevm.OracleRequest {
	req := &oraclevm.OracleRequest{
		ServiceID:      id(0x11),
		SessionID:      id(0x12),
		TxID:           id(0x13),
		Step:           0,
		Retry:          0,
		Kind:           kind,
		Target:         zBytes(0x21, 8),
		DeadlineHeight: 100,
		Executors:      execs,
		CreatedAt:      oTime(1000, 0),
	}
	copy(req.PayloadHash[:], zBytes(0x22, 32))
	copy(req.SchemaHash[:], zBytes(0x23, 32))
	req.RequestID = oracle.ComputeRequestID(req.ServiceID, req.SessionID, req.TxID, req.Step, req.Retry)
	return req
}

func oRecord(req *oraclevm.OracleRequest, exec ids.NodeID, ts uint64, seed byte) *oraclevm.OracleRecord {
	r := &oraclevm.OracleRecord{
		RequestID:   req.RequestID,
		Executor:    exec,
		Timestamp:   ts,
		Endpoint:    "https://feed.invalid/v1/price",
		ResultCode:  200,
		ExternalRef: zBytes(seed, 8),
		Scheme:      1,
		Signature:   zBytes(seed+1, 16),
	}
	copy(r.BodyHash[:], zBytes(seed+2, 32))
	return r
}

func oJSON(v any) []byte {
	w, err := json.Marshal(v)
	must(err)
	return w
}

// oRun is what a `commit` vector carries: a request and the records offered
// against it, in order.
type oRun struct {
	Request *oraclevm.OracleRequest  `json:"request"`
	Records []*oraclevm.OracleRecord `json:"records"`
}

func oRequestVectors() []Vector {
	ok := oRequest(0, []ids.NodeID{oOperator()})
	wrong := oRequest(0, []ids.NodeID{oOperator()})
	wrong.RequestID[0] ^= 0xFF
	read := oRequest(1, []ids.NodeID{oOperator()})
	stepped := oRequest(0, []ids.NodeID{oOperator()})
	stepped.Step = 3
	// The id is left as the step-0 derivation, so the request now names itself
	// with a name its own arguments do not derive.
	return []Vector{
		vec("O_REQUEST_WRITE", "O", "request", oJSON(ok)),
		vec("O_REQUEST_READ", "O", "request", oJSON(read)),
		vec("O_REQUEST_WRONG_ID", "O", "request", oJSON(wrong)),
		vec("O_REQUEST_STALE_STEP", "O", "request", oJSON(stepped)),
		vec("O_REQUEST_NOT_JSON", "O", "request", []byte("{")),
	}
}

func oCommitVectors() []Vector {
	req := oRequest(0, []ids.NodeID{nodeID(1), nodeID(2), nodeID(3)})
	r1 := oRecord(req, nodeID(1), 1000, 0x50)
	r2 := oRecord(req, nodeID(2), 1100, 0x60)
	r3 := oRecord(req, nodeID(3), 1200, 0x70)

	run := func(rs ...*oraclevm.OracleRecord) []byte {
		return oJSON(&oRun{Request: req, Records: rs})
	}

	unauth := oRecord(req, nodeID(9), 1000, 0x50)

	// A record that differs from r1 in ONE field, one per field the leaf hash
	// is taken over. Each must move the root: a leaf that left a field out
	// would let an executor change it after the commitment.
	other := func(f func(*oraclevm.OracleRecord)) *oraclevm.OracleRecord {
		r := *r1
		f(&r)
		return &r
	}

	return []Vector{
		vec("O_COMMIT_ONE", "O", "commit", run(r1)),
		vec("O_COMMIT_TWO", "O", "commit", run(r1, r2)),
		vec("O_COMMIT_THREE", "O", "commit", run(r1, r2, r3)),
		// Three records and four, where the fourth repeats the third. The
		// tree duplicates a lone last leaf to pair it, so these two DIFFERENT
		// record sets commit to the SAME root — a light client shown either
		// one cannot tell which was executed. The pair is the vector; a single
		// root proves nothing.
		vec("O_COMMIT_THREE_PADDED", "O", "commit", run(r1, r2, r3, r3)),
		vec("O_COMMIT_ORDER_SWAPPED", "O", "commit", run(r2, r1)),
		vec("O_COMMIT_OTHER_TIMESTAMP", "O", "commit",
			run(other(func(r *oraclevm.OracleRecord) { r.Timestamp = 1001 }))),
		vec("O_COMMIT_OTHER_ENDPOINT", "O", "commit",
			run(other(func(r *oraclevm.OracleRecord) { r.Endpoint += "?x=1" }))),
		vec("O_COMMIT_OTHER_BODY", "O", "commit",
			run(other(func(r *oraclevm.OracleRecord) { r.BodyHash[0] ^= 0xFF }))),
		vec("O_COMMIT_OTHER_CODE", "O", "commit",
			run(other(func(r *oraclevm.OracleRecord) { r.ResultCode = 404 }))),
		vec("O_COMMIT_OTHER_REF", "O", "commit",
			run(other(func(r *oraclevm.OracleRecord) { r.ExternalRef = zBytes(0x51, 8) }))),
		// The signature is NOT in the leaf. Two records that differ only there
		// commit to one root, which is the chain's answer and has to be every
		// implementation's.
		vec("O_COMMIT_OTHER_SIGNATURE", "O", "commit",
			run(other(func(r *oraclevm.OracleRecord) { r.Signature = zBytes(0x99, 16) }))),
		vec("O_COMMIT_NO_RECORDS", "O", "commit", run()),
		vec("O_COMMIT_UNAUTHORIZED", "O", "commit", run(unauth)),
		vec("O_COMMIT_NOT_JSON", "O", "commit", []byte("[]")),
	}
}

func oObservationVectors() []Vector {
	fresh := oObservation(oFeed(), oOperator(), oFresh(), 0x20)
	stale := oObservation(oFeed(), oOperator(), oTime(1500, 0), 0x20)
	unknownFeed := oObservation(id(0x66), oOperator(), oFresh(), 0x20)
	unauthorized := oObservation(oFeed(), nodeID(8), oFresh(), 0x20)
	return []Vector{
		vec("O_OBS_ACCEPTED", "O", "observation", oJSON(fresh)),
		vec("O_OBS_STALE", "O", "observation", oJSON(stale)),
		vec("O_OBS_UNKNOWN_FEED", "O", "observation", oJSON(unknownFeed)),
		vec("O_OBS_UNAUTHORIZED", "O", "observation", oJSON(unauthorized)),
		vec("O_OBS_NOT_JSON", "O", "observation", []byte("7")),
	}
}

// oAssert holds the emit to the claims this half of the corpus rests on. A
// vector that does not do what its name says is worse than no vector: it is a
// row three implementations will agree about for the wrong reason.
func oAssert(v []Vector) {
	by := map[string]Result{}
	for _, vec := range v {
		by[vec.ID] = evalO(vec)
	}
	ok := func(idStr string) Result {
		r := by[idStr]
		if r.Parse != "ok" {
			panic(fmt.Sprintf("%s is not the wire the O-chain writes: %s", idStr, r.Note))
		}
		return r
	}
	same := func(a, b string) {
		if ok(a).Hash != ok(b).Hash {
			panic(fmt.Sprintf("%s and %s were expected to derive one id", a, b))
		}
	}
	differ := func(a, b string) {
		if ok(a).Hash == ok(b).Hash {
			panic(fmt.Sprintf("%s and %s were expected to derive different ids", a, b))
		}
	}

	// The two id claims in the file header.
	same("O_BLOCK_EMPTY", "O_BLOCK_EMPTY_ARRAY")
	differ("O_BLOCK_EMPTY", "O_BLOCK_ID_SET")
	written := oBlockID(oBlockWire(oParent(), 1, oTime(1000, 0), nil, nil, nil))
	if by["O_BLOCK_ID_SET"].Hash == hex.EncodeToString(written[:]) {
		panic("O_BLOCK_ID_SET was expected NOT to round-trip its own id")
	}

	// The five number vectors: four refusals and the one that fits.
	for _, refused := range []string{
		"O_BLOCK_HEIGHT_FLOAT", "O_BLOCK_HEIGHT_EXPONENT", "O_BLOCK_HEIGHT_NEGATIVE",
		"O_BLOCK_HEIGHT_OVER_MAX", "O_BLOCK_SCHEME_OVER_BYTE",
	} {
		if by[refused].Parse != VMalformed {
			panic(refused + " was expected to be refused, and was not: " + by[refused].Note)
		}
	}
	if by["O_BLOCK_HEIGHT_MAX"].Parse != "ok" {
		panic("the largest uint64 was refused: " + by["O_BLOCK_HEIGHT_MAX"].Note)
	}

	// omitempty writes the proofs that carry bytes and omits the empty ones,
	// so the two aggregation blocks are two different blocks.
	differ("O_BLOCK_AGG_PROOFS", "O_BLOCK_AGG_EMPTY_PROOF")
	// omitempty empties the three byte slices of an attestation and does
	// NOTHING to its fixed array, so the two attestation blocks differ.
	differ("O_BLOCK_ATTESTATION", "O_BLOCK_ATTESTATION_BARE")
	// And the zero commitment must still be WRITTEN, which is the half of that
	// rule no other vector reaches: an attestation with everything omitempty
	// could touch at its zero value is not the same block as one whose array
	// is missing. Held by asserting the array is on the wire.
	if !strings.Contains(string(oBlockWithAttestations(oParent(), 5, oTime(1000, 0),
		nil, nil, nil, []*artifacts.OracleAttestation{oZeroCommitted()})),
		`"valueCommitment":[0,0,0`) {
		panic("omitempty dropped a fixed array, which Go does not do")
	}
	// A nil []byte and an empty one are `null` and `""`, and not one block.
	differ("O_BLOCK_OBS_NULL_VALUE", "O_BLOCK_OBS_EMPTY_VALUE")
	// A short fixed-array list zero-fills what it did not reach, and a long
	// one discards the rest — so the short one is a DIFFERENT block and the
	// long one is the SAME block as the one it was cut from.
	differ("O_BLOCK_OBSERVATION", "O_BLOCK_SOURCEMETA_SHORT")
	same("O_BLOCK_OBSERVATION", "O_BLOCK_SOURCEMETA_LONG")

	// The unknown member is ignored, so it is the same block.
	same("O_BLOCK_EMPTY", "O_BLOCK_UNKNOWN_MEMBER")
	// The last of two members naming one field wins, so the intruder placed
	// first changes nothing and the one placed last changes the block.
	same("O_BLOCK_EMPTY", "O_BLOCK_DUPLICATE_HEIGHT_FIRST")
	differ("O_BLOCK_EMPTY", "O_BLOCK_DUPLICATE_HEIGHT_LAST")
	// A member name that matches only case-insensitively still reaches the
	// field, so the block is unchanged.
	same("O_BLOCK_EMPTY", "O_BLOCK_FOLDED_MEMBER")
	// And position, not exactness, decides which of two names that both reach
	// the field wins.
	differ("O_BLOCK_EMPTY", "O_BLOCK_FOLDED_AFTER_EXACT")
	same("O_BLOCK_EMPTY", "O_BLOCK_EXACT_AFTER_FOLDED")
	// The native-chain parent must render through the alias table and NOT as
	// CB58 — the alias carries no checksum, where every other id does — and it
	// must read back as the id it names. Both halves are the vector: a port
	// that only knows CB58 writes a different parent and derives a different
	// id for this block, and one that writes the alias but cannot read it
	// refuses the block outright.
	native := oNativeID('P')
	rendered := native.String()
	if !strings.Contains(string(oBlockWire(native, 1, oTime(1000, 0), nil, nil, nil)),
		`"parentID":"`+rendered+`"`) {
		panic("O_BLOCK_NATIVE_PARENT does not exercise the native-chain rendering")
	}
	if rendered == ids.Empty.String() || len(rendered) != 33 {
		panic("the native-chain alias is not the unchecksummed form this vector is for: " + rendered)
	}
	if by["O_BLOCK_NATIVE_PARENT"].Parse != "ok" {
		panic("the native-chain alias did not read back: " + by["O_BLOCK_NATIVE_PARENT"].Note)
	}

	// Both arguments of an artifact id move it.
	differ("O_ARTIFACTID_BASE", "O_ARTIFACTID_OTHER_EPOCH")
	differ("O_ARTIFACTID_BASE", "O_ARTIFACTID_OTHER_FEED")

	// The five arguments of a request id each move it.
	for _, pair := range [][2]string{
		{"O_REQUESTID_BASE", "O_REQUESTID_STEP"},
		{"O_REQUESTID_BASE", "O_REQUESTID_RETRY"},
		{"O_REQUESTID_BASE", "O_REQUESTID_OTHER_SERVICE"},
		{"O_REQUESTID_BASE", "O_REQUESTID_OTHER_SESSION"},
		{"O_REQUESTID_BASE", "O_REQUESTID_OTHER_TX"},
		{"O_REQUESTID_BASE", "O_REQUESTID_SWAPPED_IDS"},
		{"O_REQUESTID_STEP", "O_REQUESTID_RETRY"},
	} {
		differ(pair[0], pair[1])
	}

	// The malleability the tree has: an odd last leaf is paired with itself,
	// so three records and those three with the last repeated are one root.
	same("O_COMMIT_THREE", "O_COMMIT_THREE_PADDED")
	// Order is in the root, and so is every field the leaf hashes.
	differ("O_COMMIT_TWO", "O_COMMIT_ORDER_SWAPPED")
	for _, other := range []string{
		"O_COMMIT_OTHER_TIMESTAMP", "O_COMMIT_OTHER_ENDPOINT",
		"O_COMMIT_OTHER_BODY", "O_COMMIT_OTHER_CODE", "O_COMMIT_OTHER_REF",
	} {
		differ("O_COMMIT_ONE", other)
	}
	// And the signature is not.
	same("O_COMMIT_ONE", "O_COMMIT_OTHER_SIGNATURE")

	// The four observation vectors must reach four different rules, or the
	// staleness rule and the authorisation rule are one row.
	for _, want := range [][2]string{
		{"O_OBS_ACCEPTED", VOK},
		{"O_OBS_STALE", VSyntactic},
		{"O_OBS_UNKNOWN_FEED", VLedger},
		{"O_OBS_UNAUTHORIZED", VAuth},
	} {
		if by[want[0]].Exec != want[1] {
			panic(fmt.Sprintf("%s answered %s, not %s: %s",
				want[0], by[want[0]].Exec, want[1], by[want[0]].Note))
		}
	}
}

// ---------------------------------------------------------------------------
// Answering.
// ---------------------------------------------------------------------------

// oBlockID is the id this chain derives for a block on the wire: parse it,
// marshal it again, hash that. Not the hash of the bytes given.
func oBlockID(wire []byte) ids.ID {
	blk := &oraclevm.Block{}
	if err := json.Unmarshal(wire, blk); err != nil {
		return ids.Empty
	}
	again, err := json.Marshal(blk)
	must(err)
	return ids.ID(sha256.Sum256(again))
}

// ovm is an O-chain over an empty database, seeded with the corpus's feeds.
// One per process. SubmitObservation appends to a pending list nothing else
// reads, so the vectors do not see each other.
var (
	ovmOnce  sync.Once
	ovmChain *oraclevm.VM
	ovmErr   error
)

func ovm() (*oraclevm.VM, error) {
	ovmOnce.Do(func() { ovmChain, ovmErr = startOvm(oGenesisCfg) })
	return ovmChain, ovmErr
}

func startOvm(genesis string) (*oraclevm.VM, error) {
	// Initialize announces itself on stdout, and stdout is the result stream
	// the runner reads.
	stdout := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = stdout }()

	vm := &oraclevm.VM{}
	err := vm.Initialize(context.Background(), luxvm.Init{
		Runtime: &runtime.Runtime{
			NodeID:    nodeID(1),
			NetworkID: networkID,
			ChainID:   id(oChain),
			Log:       log.Noop(),
		},
		DB:      memdb.New(),
		Log:     log.Noop(),
		Genesis: []byte(genesis),
	})
	if err != nil {
		return nil, err
	}
	return vm, nil
}

func evalO(v Vector) Result {
	switch v.Op {
	case "block":
		return evalOBlock(v)
	case "genesis":
		return evalOGenesis(v)
	case "artifactid":
		return evalOArtifactID(v)
	case "requestid":
		return evalORequestID(v)
	case "request":
		return evalORequest(v)
	case "commit":
		return evalOCommit(v)
	case "observation":
		return evalOObservation(v)
	}
	return Result{ID: v.ID, Parse: VInternal, Kind: none, Hash: none,
		Syntactic: VInternal, Exec: VInternal, Note: "unknown O op " + v.Op}
}

func oResult(v Vector) (Result, []byte, bool) {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r, nil, false
	}
	return r, b, true
}

func oMalformed(r Result, err error) Result {
	r.Parse = VMalformed
	r.Syntactic = VMalformed
	r.Exec = VMalformed
	r.Note = trim(err.Error())
	return r
}

// evalOBlock reads a block, names what it carries, and derives its id.
//
// `Verify` is the chain's, and it is unconditional — so both verdict fields
// are OK for everything that parses. That is what this chain decides about a
// block, and printing anything else here would be this file's opinion rather
// than the reference's.
func evalOBlock(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	vm, err := ovm()
	if err != nil {
		r.Parse = VInternal
		r.Note = "the reference VM did not start: " + err.Error()
		return r
	}
	blk, err := vm.ParseBlock(context.Background(), b)
	if err != nil {
		return oMalformed(r, err)
	}
	r.Parse = "ok"
	ob, isO := blk.(*oraclevm.Block)
	if !isO {
		r.Parse = VInternal
		r.Note = "ParseBlock returned something that is not an O block"
		return r
	}
	r.Kind = oKindName(ob)
	blockID := blk.ID()
	r.Hash = hex.EncodeToString(blockID[:])
	if err := blk.Verify(context.Background()); err != nil {
		class := classify(err)
		r.Syntactic = class
		r.Exec = class
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = fmt.Sprintf("height=%d parent=%s obs=%d agg=%d feeds=%d att=%d",
		ob.Height_, shortID(ob.ParentID_), len(ob.Observations),
		len(ob.Aggregations), len(ob.FeedUpdates), len(ob.Attestations))
	return r
}

// oKindName says what a block CARRIES. The O-chain has one block type, so
// naming the type would compare a constant against itself; which of the three
// lists a buffer decoded into is the thing the implementations have to agree
// about before any verdict about them means anything.
func oKindName(b *oraclevm.Block) string {
	var parts []string
	add := func(name string, n int) {
		switch {
		case n == 0:
		case n == 1:
			parts = append(parts, name)
		default:
			parts = append(parts, fmt.Sprintf("%sx%d", name, n))
		}
	}
	add("Observation", len(b.Observations))
	add("Aggregation", len(b.Aggregations))
	add("FeedUpdate", len(b.FeedUpdates))
	add("Attestation", len(b.Attestations))
	if len(parts) == 0 {
		return "Empty"
	}
	return strings.Join(parts, "+")
}

// evalOGenesis reads the genesis the observation vectors are judged under and
// answers with what it parsed, so that "we applied the same configuration" is
// a compared field rather than an assumption under every row below it.
//
// The hash is over the canonical re-marshal of the parsed struct, and NOT over
// the id of the genesis block the chain would build from it: that id is a
// function of the machine's timezone (see the note at the top of this file)
// and would put a different answer in every column on every box.
func evalOGenesis(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	g := &oraclevm.Genesis{}
	if err := json.Unmarshal(b, g); err != nil {
		return oMalformed(r, err)
	}
	r.Parse = "ok"
	r.Kind = "Genesis"
	again, err := json.Marshal(g)
	must(err)
	sum := sha256.Sum256(again)
	r.Hash = hex.EncodeToString(sum[:])
	r.Syntactic = fmt.Sprintf("feeds=%d", len(g.InitialFeeds))
	r.Exec = VOK
	r.Note = fmt.Sprintf("timestamp=%d message=%q", g.Timestamp, g.Message)
	return r
}

// evalOArtifactID asks the artifact itself for its id, so the answer comes out
// of the reference's own method rather than out of a preimage written here.
func evalOArtifactID(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	f := strings.Split(string(b), "|")
	if len(f) != 2 {
		return oMalformed(r, fmt.Errorf("an artifact id takes two arguments, got %d", len(f)))
	}
	feed, err := oArgID(f[0])
	if err != nil {
		return oMalformed(r, err)
	}
	epoch, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return oMalformed(r, err)
	}
	r.Parse = "ok"
	r.Kind = "OracleAttestation"
	a := &artifacts.OracleAttestation{FeedID: feed, Epoch: epoch}
	r.Hash = hexID(a.ArtifactID())
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = fmt.Sprintf("epoch=%d", epoch)
	return r
}

func evalORequestID(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	f := strings.Split(string(b), "|")
	if len(f) != 5 {
		return oMalformed(r, fmt.Errorf("a request id takes five arguments, got %d", len(f)))
	}
	svc, e1 := oArgID(f[0])
	ses, e2 := oArgID(f[1])
	tx, e3 := oArgID(f[2])
	step, e4 := strconv.ParseUint(f[3], 10, 32)
	retry, e5 := strconv.ParseUint(f[4], 10, 32)
	for _, err := range []error{e1, e2, e3, e4, e5} {
		if err != nil {
			return oMalformed(r, err)
		}
	}
	r.Parse = "ok"
	r.Kind = "RequestID"
	sum := oracle.ComputeRequestID(svc, ses, tx, uint32(step), uint32(retry))
	r.Hash = hex.EncodeToString(sum[:])
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = fmt.Sprintf("step=%d retry=%d", step, retry)
	return r
}

func oArgID(s string) (ids.ID, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ids.Empty, err
	}
	if len(b) != 32 {
		return ids.Empty, fmt.Errorf("an id is 32 bytes, got %d", len(b))
	}
	return ids.ID(b), nil
}

// evalORequest offers a request to a chain that holds nothing.
//
// RegisterRequest checks the deterministic id BEFORE it looks in the request
// map, and that lookup is its first read of the chain — so a request whose id
// its own arguments do not derive is refused ahead of the store and answers
// both fields, and everything past it answers exec alone. That boundary is the
// corpus's, defined in conformance/README.md, and this is where the reference
// puts it.
func evalORequest(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	req := &oraclevm.OracleRequest{}
	if err := json.Unmarshal(b, req); err != nil {
		return oMalformed(r, err)
	}
	r.Parse = "ok"
	r.Kind = oRequestKindName(req.Kind)
	r.Hash = hex.EncodeToString(req.RequestID[:])

	vm, err := startOvm(oGenesisCfg)
	if err != nil {
		r.Parse = VInternal
		r.Note = "the reference VM did not start: " + err.Error()
		return r
	}
	if err := vm.RegisterRequest(req); err != nil {
		class := classify(err)
		if strings.HasPrefix(err.Error(), "invalid request_id") {
			r.Syntactic = class
		} else {
			r.Syntactic = VOK
		}
		r.Exec = class
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = fmt.Sprintf("executors=%d deadline=%d", len(req.Executors), req.DeadlineHeight)
	return r
}

func oRequestKindName(k oraclevm.RequestKind) string {
	switch k {
	case 0:
		return "Write"
	case 1:
		return "Read"
	}
	return "unknown"
}

// evalOCommit runs a request and its records through the chain and answers
// with the Merkle root the chain commits to.
//
// Every refusal here is past the chain's first read — RegisterRequest's own
// deterministic-id check aside, which the request vectors already cover — so
// syntactic is OK and exec carries the class.
func evalOCommit(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	run := &oRun{}
	if err := json.Unmarshal(b, run); err != nil {
		return oMalformed(r, err)
	}
	if run.Request == nil {
		return oMalformed(r, fmt.Errorf("a commit run carries a request"))
	}
	r.Parse = "ok"
	r.Kind = "OracleCommit"
	r.Syntactic = fmt.Sprintf("records=%d", len(run.Records))

	vm, err := startOvm(oGenesisCfg)
	if err != nil {
		r.Parse = VInternal
		r.Note = "the reference VM did not start: " + err.Error()
		return r
	}
	if err := vm.RegisterRequest(run.Request); err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	for _, rec := range run.Records {
		if err := vm.SubmitRecord(rec); err != nil {
			r.Exec = classify(err)
			r.Note = trim(err.Error())
			return r
		}
	}
	commit, err := vm.CommitRecords(run.Request.RequestID)
	if err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	r.Hash = hex.EncodeToString(commit.Root[:])
	r.Exec = VOK
	r.Note = fmt.Sprintf("count=%d window=[%d,%d]",
		commit.RecordCount, commit.Window.Start, commit.Window.End)
	return r
}

// evalOObservation offers an observation to the seeded chain. Its first act is
// a feed lookup, which is a read of the chain, so every refusal it can reach
// is exec and syntactic is OK.
func evalOObservation(v Vector) Result {
	r, b, ok := oResult(v)
	if !ok {
		return r
	}
	obs := &oraclevm.Observation{}
	if err := json.Unmarshal(b, obs); err != nil {
		return oMalformed(r, err)
	}
	r.Parse = "ok"
	r.Kind = "Observation"
	r.Hash = hex.EncodeToString(obs.FeedID[:])
	r.Syntactic = VOK

	vm, err := ovm()
	if err != nil {
		r.Parse = VInternal
		r.Note = "the reference VM did not start: " + err.Error()
		return r
	}
	if err := vm.SubmitObservation(obs); err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	r.Exec = VOK
	r.Note = "accepted into the pending set"
	return r
}
