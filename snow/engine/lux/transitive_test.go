<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
// See the file LICENSE for licensing terms.

package lux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	"golang.org/x/exp/slices"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/snow/consensus/snowball"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/avalanche/bootstrap"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/engine/common/tracker"
	"github.com/luxdefi/node/snow/engine/snowman/block"
	"github.com/luxdefi/node/snow/validators"
	"github.com/luxdefi/node/utils"
	"github.com/luxdefi/node/utils/constants"
	"github.com/luxdefi/node/utils/set"
	"github.com/luxdefi/node/utils/wrappers"
	"github.com/luxdefi/node/version"

	avagetter "github.com/luxdefi/node/snow/engine/avalanche/getter"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/consensus/snowball"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/snow/engine/lux/bootstrap"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/engine/common/tracker"
	"github.com/luxdefi/luxd/snow/validators"
	"github.com/luxdefi/luxd/utils"
	"github.com/luxdefi/luxd/utils/constants"
	"github.com/luxdefi/luxd/utils/wrappers"
	"github.com/luxdefi/luxd/version"

<<<<<<< HEAD
<<<<<<< HEAD
	luxgetter "github.com/luxdefi/luxd/snow/engine/lux/getter"
=======
	avagetter "github.com/luxdefi/luxd/snow/engine/lux/getter"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
=======
	avagetter "github.com/luxdefi/luxd/snow/engine/lux/getter"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
=======
	luxgetter "github.com/luxdefi/luxd/snow/engine/lux/getter"
>>>>>>> 6ce5514cf (Update getters, constants)
>>>>>>> 30c258f70 (Update getters, constants)
)

var (
	errUnknownVertex = errors.New("unknown vertex")
	errFailedParsing = errors.New("failed parsing")
	errMissing       = errors.New("missing")
<<<<<<< HEAD
)

type dummyHandler struct {
	startEngineF func(startReqID uint32) error
}

func (dh *dummyHandler) onDoneBootstrapping(lastReqID uint32) error {
	lastReqID++
	return dh.startEngineF(lastReqID)
=======
	errTest          = errors.New("non-nil error")
)

type dummyHandler struct {
	startEngineF func(ctx context.Context, startReqID uint32) error
}

func (dh *dummyHandler) onDoneBootstrapping(ctx context.Context, lastReqID uint32) error {
	lastReqID++
	return dh.startEngineF(ctx, lastReqID)
>>>>>>> 53a8245a8 (Update consensus)
}

func TestEngineShutdown(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vmShutdownCalled := false
	vm := &vertex.TestVM{}
	vm.T = t
<<<<<<< HEAD
	vm.ShutdownF = func() error { vmShutdownCalled = true; return nil }
=======
<<<<<<< HEAD
<<<<<<< HEAD
	vm.ShutdownF = func(context.Context) error {
=======
	vm.ShutdownF = func() error {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.ShutdownF = func(context.Context) error {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
		vmShutdownCalled = true
		return nil
	}
>>>>>>> 53a8245a8 (Update consensus)
	engCfg.VM = vm

	transitive, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}
<<<<<<< HEAD
	if err := transitive.Shutdown(); err != nil {
=======
	if err := transitive.Shutdown(context.Background()); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}
	if !vmShutdownCalled {
		t.Fatal("Shutting down the Transitive did not shutdown the VM")
	}
}

func TestEngineAdd(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	manager.Default(true)

	manager.CantEdge = false

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if te.Ctx.ChainID != ids.Empty {
		t.Fatalf("Wrong chain ID")
	}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{
			&lux.TestVertex{TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Unknown,
			}},
		},
		BytesV: []byte{1},
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx.ParentsV[0].ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}

	if err := te.Put(context.Background(), vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil

	if !*asked {
		t.Fatalf("Didn't ask for a missing vertex")
	}

	if len(te.vtxBlocked) != 1 {
		t.Fatalf("Should have been blocking on request")
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) { return nil, errFailedParsing }
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.ParseVtxF = func(context.Context, []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.ParseVtxF = func(context.Context, []byte) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
		return nil, errFailedParsing
	}
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) { return nil, errFailedParsing }
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)

	if err := te.Put(context.Background(), vdr, *reqID, nil); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking issue")
	}
}

func TestEngineQuery(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	manager.Default(true)

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}

		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vertexed := new(bool)
<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if *vertexed {
			t.Fatalf("Sent multiple requests")
		}
		*vertexed = true
		if vtxID != vtx0.ID() {
			t.Fatalf("Wrong vertex requested")
		}
		return nil, errUnknownVertex
	}

	chitted := new(bool)
<<<<<<< HEAD
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, _ uint32, prefs []ids.ID) {
=======
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, _ uint32, prefs []ids.ID, _ []ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		if *chitted {
			t.Fatalf("Sent multiple chits")
		}
		*chitted = true
		if len(prefs) != 2 {
			t.Fatalf("Wrong chits preferences")
		}
	}

	asked := new(bool)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, _ uint32, vtxID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	// After receiving the pull query for [vtx0] we will first request [vtx0]
	// from the peer, because it is currently unknown to the engine.
	if err := te.PullQuery(context.Background(), vdr, 0, vtx0.ID()); err != nil {
		t.Fatal(err)
	}

	if !*vertexed {
		t.Fatalf("Didn't request vertex")
	}
	if !*asked {
		t.Fatalf("Didn't request vertex from validator")
	}

	queried := new(bool)
	queryRequestID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtx []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
<<<<<<< HEAD
		vdrSet := ids.NodeIDSet{}
=======
		vdrSet := set.Set[ids.NodeID]{}
>>>>>>> 53a8245a8 (Update consensus)
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(vtx0.Bytes(), vtx) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !bytes.Equal(b, vtx0.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx0, nil
	}

	// Once the peer returns [vtx0], we will respond to its query and then issue
	// our own push query for [vtx0].
	if err := te.Put(context.Background(), vdr, 0, vtx0.Bytes()); err != nil {
		t.Fatal(err)
	}
	manager.ParseVtxF = nil

	if !*queried {
		t.Fatalf("Didn't ask for preferences")
	}
	if !*chitted {
		t.Fatalf("Didn't provide preferences")
	}

	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{5, 4, 3, 2, 1, 9},
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtx0.ID() {
			return &lux.TestVertex{
				TestDecidable: choices.TestDecidable{
					StatusV: choices.Unknown,
				},
			}, nil
		}
		if vtxID == vtx1.ID() {
			return nil, errUnknownVertex
		}
		t.Fatalf("Wrong vertex requested")
		panic("Should have failed")
	}

	*asked = false
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, _ uint32, vtxID ids.ID) {
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx1.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	// The peer returned [vtx1] from our query for [vtx0], which means we will
	// need to request the missing [vtx1].
<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{vtx1.ID()}); err != nil {
=======
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{vtx1.ID()}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	*queried = false
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtx []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
<<<<<<< HEAD
		vdrSet := ids.NodeIDSet{}
=======
		vdrSet := set.Set[ids.NodeID]{}
>>>>>>> 53a8245a8 (Update consensus)
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(vtx1.Bytes(), vtx) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !bytes.Equal(b, vtx1.Bytes()) {
			t.Fatalf("Wrong bytes")
		}

<<<<<<< HEAD
		manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
		manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
		manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			if vtxID == vtx0.ID() {
				return &lux.TestVertex{
					TestDecidable: choices.TestDecidable{
						StatusV: choices.Processing,
					},
				}, nil
			}
			if vtxID == vtx1.ID() {
				return vtx1, nil
			}
			t.Fatalf("Wrong vertex requested")
			panic("Should have failed")
		}

		return vtx1, nil
	}

	// Once the peer returns [vtx1], the poll that was issued for [vtx0] will be
	// able to terminate. Additionally the node will issue a push query with
	// [vtx1].
	if err := te.Put(context.Background(), vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}
	manager.ParseVtxF = nil

	// Because [vtx1] does not transitively reference [vtx0], the transaction
	// vertex for [vtx0] was never voted for. This results in [vtx0] still being
	// in processing.
	if vtx0.Status() != choices.Processing {
		t.Fatalf("Shouldn't have executed the vertex yet")
	}
	if vtx1.Status() != choices.Accepted {
		t.Fatalf("Should have executed the vertex")
	}
	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have executed the transaction")
	}

	// Make sure there is no memory leak for missing vertex tracking.
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}

	sender.CantSendPullQuery = false

	// Abandon the query for [vtx1]. This will result in a re-query for [vtx0].
	if err := te.QueryFailed(context.Background(), vdr, *queryRequestID); err != nil {
		t.Fatal(err)
	}
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineMultipleQuery(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	engCfg.Params = lux.Parameters{
		Parameters: snowball.Parameters{
			K:                       3,
			Alpha:                   2,
			BetaVirtuous:            1,
			BetaRogue:               2,
			ConcurrentRepolls:       1,
			OptimalProcessing:       100,
			MaxOutstandingItems:     1,
			MaxItemProcessingTime:   1,
			MixedQueryNumPushNonVdr: 3,
		},
		Parents:   2,
		BatchSize: 1,
	}

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()
	vdr2 := ids.GenerateTestNodeID()

	errs := wrappers.Errs{}
	errs.Add(
<<<<<<< HEAD
		vals.AddWeight(vdr0, 1),
		vals.AddWeight(vdr1, 1),
		vals.AddWeight(vdr2, 1),
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
		vals.Add(vdr0, nil, ids.Empty, 1),
		vals.Add(vdr1, nil, ids.Empty, 1),
		vals.Add(vdr2, nil, ids.Empty, 1),
=======
		vals.Add(vdr0, 1),
		vals.Add(vdr1, 1),
		vals.Add(vdr2, 1),
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
		vals.Add(vdr0, nil, 1),
		vals.Add(vdr1, nil, 1),
		vals.Add(vdr2, nil, 1),
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
		vals.Add(vdr0, nil, ids.Empty, 1),
		vals.Add(vdr1, nil, ids.Empty, 1),
		vals.Add(vdr2, nil, ids.Empty, 1),
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
	)
	if errs.Errored() {
		t.Fatal(errs.Err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	queried := new(bool)
	queryRequestID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtx []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		if *queried {
			t.Fatalf("Asked multiple times")
		}
		*queried = true
		*queryRequestID = requestID
<<<<<<< HEAD
		vdrSet := ids.NodeIDSet{}
=======
		vdrSet := set.Set[ids.NodeID]{}
>>>>>>> 53a8245a8 (Update consensus)
		vdrSet.Add(vdr0, vdr1, vdr2)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(vtx0.Bytes(), vtx) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	if err := te.issue(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}

	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx0.ID():
			return vtx0, nil
		case vtx1.ID():
			return nil, errUnknownVertex
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	asked := new(bool)
	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if *asked {
			t.Fatalf("Asked multiple times")
		}
		*asked = true
		if vdr0 != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx1.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	s0 := []ids.ID{vtx0.ID(), vtx1.ID()}

	s2 := []ids.ID{vtx0.ID()}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr0, *queryRequestID, s0); err != nil {
=======
	if err := te.Chits(context.Background(), vdr0, *queryRequestID, s0, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}
	if err := te.QueryFailed(context.Background(), vdr1, *queryRequestID); err != nil {
		t.Fatal(err)
	}
<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr2, *queryRequestID, s2); err != nil {
=======
	if err := te.Chits(context.Background(), vdr2, *queryRequestID, s2, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	// Should be dropped because the query was marked as failed
<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr1, *queryRequestID, s0); err != nil {
=======
	if err := te.Chits(context.Background(), vdr1, *queryRequestID, s0, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.GetFailed(context.Background(), vdr0, *reqID); err != nil {
		t.Fatal(err)
	}

	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have executed vertex")
	}
	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have finished blocking")
	}
}

func TestEngineBlockedIssue(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{
			&lux.TestVertex{TestDecidable: choices.TestDecidable{
				IDV:     vtx0.IDV,
				StatusV: choices.Unknown,
			}},
		},
		HeightV: 1,
		TxsV:    []snowstorm.Tx{tx0},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.issue(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

	vtx1.ParentsV[0] = vtx0
	if err := te.issue(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}

	if prefs := te.Consensus.Preferences(); prefs.Len() != 1 || !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Should have issued vtx1")
	}
}

func TestEngineAbandonResponse(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) { return nil, errUnknownVertex }
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.GetVtxF = func(context.Context, ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.GetVtxF = func(context.Context, ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
		return nil, errUnknownVertex
	}
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) { return nil, errUnknownVertex }
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, vID ids.NodeID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
	}
	sender.CantSendChits = false

	if err := te.PullQuery(context.Background(), vdr, 0, vtx.ID()); err != nil {
		t.Fatal(err)
	}
	if err := te.GetFailed(context.Background(), vdr, *reqID); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Should have removed blocking event")
	}
}

func TestEngineScheduleRepoll(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	manager.Default(true)
	manager.CantEdge = false

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	requestID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, reqID uint32, _ []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], reqID uint32, _ []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		*requestID = reqID
	}

	if err := te.issue(context.Background(), vtx); err != nil {
		t.Fatal(err)
	}

	sender.SendPushQueryF = nil

	repolled := new(bool)
<<<<<<< HEAD
	sender.SendPullQueryF = func(_ context.Context, _ ids.NodeIDSet, _ uint32, vtxID ids.ID) {
=======
	sender.SendPullQueryF = func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, vtxID ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		*repolled = true
		if vtxID != vtx.ID() {
			t.Fatalf("Wrong vertex queried")
		}
	}

	if err := te.QueryFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if !*repolled {
		t.Fatalf("Should have issued a noop")
	}
}

func TestEngineRejectDoubleSpendTx(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager
	manager.Default(true)

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	engCfg.VM = vm
	vm.Default(true)

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
<<<<<<< HEAD
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		return &lux.TestVertex{
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		return &avalanche.TestVertex{
=======
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		return &lux.TestVertex{
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []lux.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}, nil
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
	sender.CantSendPushQuery = false
<<<<<<< HEAD
	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}
}

func TestEngineRejectDoubleSpendIssuedTx(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager
	manager.Default(true)

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	engCfg.VM = vm
	vm.Default(true)

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[0])

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
<<<<<<< HEAD
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		return &lux.TestVertex{
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		return &avalanche.TestVertex{
=======
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		return &lux.TestVertex{
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []lux.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}, nil
	}

	sender.CantSendPushQuery = false

<<<<<<< HEAD
	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
		return []snowstorm.Tx{tx0}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx1}
	}
<<<<<<< HEAD
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}
}

func TestEngineIssueRepoll(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	sender.SendPullQueryF = func(_ context.Context, vdrs ids.NodeIDSet, _ uint32, vtxID ids.ID) {
		vdrSet := ids.NodeIDSet{}
=======
	if err := te.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

	sender.SendPullQueryF = func(_ context.Context, vdrs set.Set[ids.NodeID], _ uint32, vtxID ids.ID) {
		vdrSet := set.Set[ids.NodeID]{}
>>>>>>> 53a8245a8 (Update consensus)
		vdrSet.Add(vdr)
		if !vdrs.Equals(vdrSet) {
			t.Fatalf("Wrong query recipients")
		}
		if vtxID != gVtx.ID() && vtxID != mVtx.ID() {
			t.Fatalf("Unknown re-query")
		}
	}

	te.repoll(context.Background())
	if err := te.errs.Err; err != nil {
		t.Fatal(err)
	}
}

func TestEngineReissue(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	engCfg.Params.BatchSize = 2
	engCfg.Params.BetaVirtuous = 5
	engCfg.Params.BetaRogue = 5

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	engCfg.VM = vm

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	tx2 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx2.InputIDsV = append(tx2.InputIDsV, utxos[1])

	tx3 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx3.InputIDsV = append(tx3.InputIDsV, utxos[0])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{gVtx, mVtx},
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx2},
		BytesV:   []byte{42},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
<<<<<<< HEAD
	lastVtx := new(lux.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		lastVtx = &lux.TestVertex{
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	lastVtx := new(avalanche.TestVertex)
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		lastVtx = &avalanche.TestVertex{
=======
	lastVtx := new(lux.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		lastVtx = &lux.TestVertex{
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []lux.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}
		return lastVtx, nil
	}

<<<<<<< HEAD
	vm.GetTxF = func(id ids.ID) (snowstorm.Tx, error) {
=======
	vm.GetTxF = func(_ context.Context, id ids.ID) (snowstorm.Tx, error) {
>>>>>>> 53a8245a8 (Update consensus)
		if id != tx0.ID() {
			t.Fatalf("Wrong tx")
		}
		return tx0, nil
	}

	queryRequestID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
		*queryRequestID = requestID
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte) {
		*queryRequestID = requestID
	}

<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
		t.Fatal(err)
	}

<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !bytes.Equal(b, vtx.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return vtx, nil
	}

	// must vote on the first poll for the second one to settle
	// *queryRequestID is 1
<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{vtx.ID()}); err != nil {
=======
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{vtx.ID()}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.Put(context.Background(), vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}
	manager.ParseVtxF = nil

<<<<<<< HEAD
	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx3} }
	if err := te.Notify(common.PendingTxs); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx3}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
		return []snowstorm.Tx{tx3}
	}
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx3}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	// vote on second poll, *queryRequestID is 2
<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{vtx.ID()}); err != nil {
=======
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{vtx.ID()}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	// all polls settled

	if len(lastVtx.TxsV) != 1 || lastVtx.TxsV[0].ID() != tx0.ID() {
		t.Fatalf("Should have re-issued the tx")
	}
}

func TestEngineLargeIssue(t *testing.T) {
	_, _, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 5
	engCfg.Params.BetaRogue = 5

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	engCfg.VM = vm

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
<<<<<<< HEAD
	lastVtx := new(lux.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		lastVtx = &lux.TestVertex{
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	lastVtx := new(avalanche.TestVertex)
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		lastVtx = &avalanche.TestVertex{
=======
	lastVtx := new(lux.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		lastVtx = &lux.TestVertex{
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []lux.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}
		return lastVtx, nil
	}

	sender.CantSendPushQuery = false

<<<<<<< HEAD
	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if len(lastVtx.TxsV) != 1 || lastVtx.TxsV[0].ID() != tx1.ID() {
		t.Fatalf("Should have issued txs differently")
	}
}

func TestEngineGetVertex(t *testing.T) {
	commonCfg, _, engCfg := DefaultConfig()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vdrID := ids.GenerateTestNodeID()

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager
<<<<<<< HEAD
<<<<<<< HEAD
	luxGetHandler, err := luxgetter.New(manager, commonCfg)
	if err != nil {
		t.Fatal(err)
	}
	engCfg.AllGetsServer = luxGetHandler
=======
	avaGetHandler, err := avagetter.New(manager, commonCfg)
	if err != nil {
		t.Fatal(err)
	}
	engCfg.AllGetsServer = avaGetHandler
>>>>>>> 53a8245a8 (Update consensus)
=======
	luxGetHandler, err := luxgetter.New(manager, commonCfg)
	if err != nil {
		t.Fatal(err)
	}
	engCfg.AllGetsServer = luxGetHandler
>>>>>>> 30c258f70 (Update getters, constants)

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender.SendPutF = func(_ context.Context, v ids.NodeID, _ uint32, vtx []byte) {
		if v != vdrID {
			t.Fatalf("Wrong validator")
		}
		if !bytes.Equal(mVtx.Bytes(), vtx) {
			t.Fatalf("Wrong vertex")
		}
	}

	if err := te.Get(context.Background(), vdrID, 0, mVtx.ID()); err != nil {
		t.Fatal(err)
	}
}

func TestEngineInsufficientValidators(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	queried := new(bool)
<<<<<<< HEAD
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) {
=======
	sender.SendPushQueryF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		*queried = true
	}

	if err := te.issue(context.Background(), vtx); err != nil {
		t.Fatal(err)
	}

	if *queried {
		t.Fatalf("Unknown query")
	}
}

func TestEnginePushGossip(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	requested := new(bool)
	sender.SendGetF = func(_ context.Context, vdr ids.NodeID, _ uint32, vtxID ids.ID) {
		*requested = true
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx.BytesV) {
			return vtx, nil
		}
		t.Fatalf("Unknown vertex bytes")
		panic("Should have errored")
	}

	sender.CantSendPushQuery = false
	sender.CantSendChits = false
	if err := te.PushQuery(context.Background(), vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	if *requested {
		t.Fatalf("Shouldn't have requested the vertex")
	}
}

func TestEngineSingleQuery(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender.CantSendPushQuery = false
	sender.CantSendPullQuery = false

	if err := te.issue(context.Background(), vtx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineParentBlockingInsert(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	missingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	parentVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{0, 1, 2, 3},
	}

	blockingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{parentVtx},
		HeightV:  3,
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.issue(context.Background(), parentVtx); err != nil {
		t.Fatal(err)
	}
	if err := te.issue(context.Background(), blockingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("Both inserts should be blocking")
	}

	sender.CantSendPushQuery = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(context.Background(), missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineAbandonChit(t *testing.T) {
	require := require.New(t)

	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	err := vals.AddWeight(vdr, 1)
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	err := vals.Add(vdr, nil, ids.Empty, 1)
=======
	err := vals.Add(vdr, 1)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	err := vals.Add(vdr, nil, 1)
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	err := vals.Add(vdr, nil, ids.Empty, 1)
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
	require.NoError(err)

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	require.NoError(err)

<<<<<<< HEAD
	err = te.Start(0)
	require.NoError(err)

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
=======
	err = te.Start(context.Background(), 0)
	require.NoError(err)

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		reqID = requestID
	}

	err = te.issue(context.Background(), vtx)
	require.NoError(err)

	fakeVtxID := ids.GenerateTestID()
<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		require.Equal(fakeVtxID, id)
		return nil, errMissing
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		reqID = requestID
	}

	// Register a voter dependency on an unknown vertex.
<<<<<<< HEAD
	err = te.Chits(context.Background(), vdr, reqID, []ids.ID{fakeVtxID})
=======
	err = te.Chits(context.Background(), vdr, reqID, []ids.ID{fakeVtxID}, nil)
>>>>>>> 53a8245a8 (Update consensus)
	require.NoError(err)
	require.Len(te.vtxBlocked, 1)

	sender.CantSendPullQuery = false

	err = te.GetFailed(context.Background(), vdr, reqID)
	require.NoError(err)
	require.Empty(te.vtxBlocked)
}

func TestEngineAbandonChitWithUnexpectedPutVertex(t *testing.T) {
	require := require.New(t)

	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	err := vals.AddWeight(vdr, 1)
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	err := vals.Add(vdr, nil, ids.Empty, 1)
=======
	err := vals.Add(vdr, 1)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	err := vals.Add(vdr, nil, 1)
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	err := vals.Add(vdr, nil, ids.Empty, 1)
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
	require.NoError(err)

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}
	mVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{1},
	}

	vts := []lux.Vertex{gVtx, mVtx}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	require.NoError(err)

<<<<<<< HEAD
	err = te.Start(0)
	require.NoError(err)

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
=======
	err = te.Start(context.Background(), 0)
	require.NoError(err)

	var reqID uint32
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		reqID = requestID
	}

	err = te.issue(context.Background(), vtx)
	require.NoError(err)

	fakeVtxID := ids.GenerateTestID()
<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		require.Equal(fakeVtxID, id)
		return nil, errMissing
	}

	sender.SendGetF = func(_ context.Context, _ ids.NodeID, requestID uint32, _ ids.ID) {
		reqID = requestID
	}

	// Register a voter dependency on an unknown vertex.
<<<<<<< HEAD
	err = te.Chits(context.Background(), vdr, reqID, []ids.ID{fakeVtxID})
=======
	err = te.Chits(context.Background(), vdr, reqID, []ids.ID{fakeVtxID}, nil)
>>>>>>> 53a8245a8 (Update consensus)
	require.NoError(err)
	require.Len(te.vtxBlocked, 1)

	sender.CantSendPullQuery = false

	gVtxBytes := gVtx.Bytes()
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		require.Equal(gVtxBytes, b)
		return gVtx, nil
	}

	// Respond with an unexpected vertex and verify that the request is
	// correctly cleared.
	err = te.Put(context.Background(), vdr, reqID, gVtxBytes)
	require.NoError(err)
	require.Empty(te.vtxBlocked)
}

func TestEngineBlockingChitRequest(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	missingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	parentVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{parentVtx},
		HeightV:  3,
		BytesV:   []byte{2, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.issue(context.Background(), parentVtx); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == blockingVtx.ID() {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, blockingVtx.Bytes()) {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}
	sender.CantSendChits = false

	if err := te.PushQuery(context.Background(), vdr, 0, blockingVtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("Both inserts should be blocking")
	}

	sender.CantSendPushQuery = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(context.Background(), missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineBlockingChitResponse(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	issuedVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	missingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{2, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.issue(context.Background(), blockingVtx); err != nil {
		t.Fatal(err)
	}

	queryRequestID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := set.Set[ids.NodeID]{}
>>>>>>> 53a8245a8 (Update consensus)
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(issuedVtx.Bytes(), vtx) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	if err := te.issue(context.Background(), issuedVtx); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if id == blockingVtx.ID() {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blockingVtx.ID()}); err != nil {
=======
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blockingVtx.ID()}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("The insert should be blocking, as well as the chit response")
	}

	sender.SendPushQueryF = nil
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(context.Background(), missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineMissingTx(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}

	issuedVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{0, 1, 2, 3},
	}

	missingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   []byte{1, 1, 2, 3},
	}

	blockingVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{missingVtx},
		HeightV:  2,
		BytesV:   []byte{2, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.issue(context.Background(), blockingVtx); err != nil {
		t.Fatal(err)
	}

	queryRequestID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := ids.NodeIDSet{}
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtx []byte) {
		*queryRequestID = requestID
		vdrSet := set.Set[ids.NodeID]{}
>>>>>>> 53a8245a8 (Update consensus)
		vdrSet.Add(vdr)
		if !inVdrs.Equals(vdrSet) {
			t.Fatalf("Asking wrong validator for preference")
		}
		if !bytes.Equal(issuedVtx.Bytes(), vtx) {
			t.Fatalf("Asking for wrong vertex")
		}
	}

	if err := te.issue(context.Background(), issuedVtx); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if id == blockingVtx.ID() {
			return blockingVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blockingVtx.ID()}); err != nil {
=======
	if err := te.Chits(context.Background(), vdr, *queryRequestID, []ids.ID{blockingVtx.ID()}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 2 {
		t.Fatalf("The insert should be blocking, as well as the chit response")
	}

	sender.SendPushQueryF = nil
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	missingVtx.StatusV = choices.Processing
	if err := te.issue(context.Background(), missingVtx); err != nil {
		t.Fatal(err)
	}

	if len(te.vtxBlocked) != 0 {
		t.Fatalf("Both inserts should not longer be blocking")
	}
}

func TestEngineIssueBlockingTx(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0, tx1},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := te.issue(context.Background(), vtx); err != nil {
		t.Fatal(err)
	}

	if prefs := te.Consensus.Preferences(); !prefs.Contains(vtx.ID()) {
		t.Fatalf("Vertex should be preferred")
	}
}

func TestEngineReissueAbortedVertex(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
<<<<<<< HEAD
=======
	manager.TestStorage.CantEdge = false
>>>>>>> 53a8245a8 (Update consensus)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx}

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{0}
	vtxBytes1 := []byte{1}

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		BytesV:   vtxBytes0,
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx0},
		HeightV:  2,
		BytesV:   vtxBytes1,
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID()}
	}

	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID()}
	}

<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == gVtx.ID() {
			return gVtx, nil
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager.EdgeF = nil
	manager.GetVtxF = nil

	requestID := new(uint32)
	sender.SendGetF = func(_ context.Context, vID ids.NodeID, reqID uint32, vtxID ids.ID) {
		*requestID = reqID
	}
	sender.CantSendChits = false
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtxBytes1) {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := te.PushQuery(context.Background(), vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}

	sender.SendGetF = nil
	manager.ParseVtxF = nil

	if err := te.GetFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	requested := new(bool)
	sender.SendGetF = func(_ context.Context, _ ids.NodeID, _ uint32, vtxID ids.ID) {
		if vtxID == vtxID0 {
			*requested = true
		}
	}
<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := te.PullQuery(context.Background(), vdr, 0, vtxID1); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested the missing vertex")
	}
}

func TestEngineBootstrappingIntoConsensus(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()

	vals := validators.NewSet()
	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, 0)
	vals.RegisterCallbackListener(startup)

	bootCfg.Beacons = vals
	bootCfg.Validators = vals
	bootCfg.StartupTracker = startup
	engCfg.Validators = vals

	bootCfg.SampleK = vals.Len()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
<<<<<<< HEAD
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	manager.TestStorage.CantEdge = false
	bootCfg.Manager = manager
	engCfg.Manager = manager

	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

	vm.CantSetState = false
	vm.CantConnected = false

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	txID0 := ids.GenerateTestID()
	txID1 := ids.GenerateTestID()

	txBytes0 := []byte{0}
	txBytes1 := []byte{1}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID0,
			StatusV: choices.Processing,
		},
		BytesV: txBytes0,
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Processing,
		},
		HeightV: 1,
		TxsV:    []snowstorm.Tx{tx0},
		BytesV:  vtxBytes0,
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   vtxBytes1,
	}

	requested := new(bool)
	requestID := new(uint32)
<<<<<<< HEAD
	sender.SendGetAcceptedFrontierF = func(_ context.Context, vdrs ids.NodeIDSet, reqID uint32) {
=======
	sender.SendGetAcceptedFrontierF = func(_ context.Context, vdrs set.Set[ids.NodeID], reqID uint32) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	dh := &dummyHandler{}
	bootstrapper, err := bootstrap.New(
<<<<<<< HEAD
=======
		context.Background(),
>>>>>>> 53a8245a8 (Update consensus)
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}
	dh.startEngineF = te.Start

<<<<<<< HEAD
	if err := bootstrapper.Start(0); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper.Connected(vdr, version.CurrentApp); err != nil {
=======
	if err := bootstrapper.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper.Connected(context.Background(), vdr, version.CurrentApp); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender.SendGetAcceptedFrontierF = nil

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	acceptedFrontier := []ids.ID{vtxID0}

	*requested = false
<<<<<<< HEAD
	sender.SendGetAcceptedF = func(_ context.Context, vdrs ids.NodeIDSet, reqID uint32, proposedAccepted []ids.ID) {
=======
	sender.SendGetAcceptedF = func(_ context.Context, vdrs set.Set[ids.NodeID], reqID uint32, proposedAccepted []ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
<<<<<<< HEAD
		if !ids.Equals(acceptedFrontier, proposedAccepted) {
=======
		if !slices.Equal(acceptedFrontier, proposedAccepted) {
>>>>>>> 53a8245a8 (Update consensus)
			t.Fatalf("Wrong proposedAccepted vertices.\nExpected: %s\nGot: %s", acceptedFrontier, proposedAccepted)
		}
		*requested = true
		*requestID = reqID
	}

	if err := bootstrapper.AcceptedFrontier(context.Background(), vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID0 {
			return nil, errMissing
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	sender.SendGetAncestorsF = func(_ context.Context, inVdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
		*requestID = reqID
	}

	if err := bootstrapper.Accepted(context.Background(), vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = nil
	sender.SendGetF = nil

<<<<<<< HEAD
	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
=======
	vm.ParseTxF = func(_ context.Context, b []byte) (snowstorm.Tx, error) {
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, txBytes0) {
			return tx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtxBytes0) {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vtxID0}
	}
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vtxID0}
	}
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID0 {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := bootstrapper.Ancestors(context.Background(), vdr, *requestID, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	vm.ParseTxF = nil
	manager.ParseVtxF = nil
	manager.EdgeF = nil
	manager.GetVtxF = nil

	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", txID0)
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", vtxID0)
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtxBytes1) {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, _ uint32, chits []ids.ID) {
=======
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, _ uint32, chits []ids.ID, _ []ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		if inVdr != vdr {
			t.Fatalf("Sent to the wrong validator")
		}

		expected := []ids.ID{vtxID0}

<<<<<<< HEAD
		if !ids.Equals(expected, chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.SendPushQueryF = func(_ context.Context, vdrs ids.NodeIDSet, _ uint32, vtx []byte) {
=======
		if !slices.Equal(expected, chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.SendPushQueryF = func(_ context.Context, vdrs set.Set[ids.NodeID], _ uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}

		if !bytes.Equal(vtxBytes1, vtx) {
			t.Fatalf("Sent wrong query bytes")
		}
	}
<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := te.PushQuery(context.Background(), vdr, 0, vtxBytes1); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil
	sender.SendChitsF = nil
	sender.SendPushQueryF = nil
	manager.GetVtxF = nil
}

func TestEngineReBootstrapFails(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	bootCfg.Alpha = 1
	bootCfg.RetryBootstrap = true
	bootCfg.RetryBootstrapWarnFrequency = 4

	vals := validators.NewSet()
	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, 0)
	vals.RegisterCallbackListener(startup)

	bootCfg.Beacons = vals
	bootCfg.Validators = vals
	bootCfg.StartupTracker = startup
	engCfg.Validators = vals

	bootCfg.SampleK = vals.Len()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

	vm.CantSetState = false

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	txID0 := ids.GenerateTestID()
	txID1 := ids.GenerateTestID()

	txBytes0 := []byte{0}
	txBytes1 := []byte{1}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID0,
			StatusV: choices.Processing,
		},
		BytesV: txBytes0,
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	requested := new(bool)
	requestID := new(uint32)
<<<<<<< HEAD
	sender.SendGetAcceptedFrontierF = func(_ context.Context, vdrs ids.NodeIDSet, reqID uint32) {
=======
	sender.SendGetAcceptedFrontierF = func(_ context.Context, vdrs set.Set[ids.NodeID], reqID uint32) {
>>>>>>> 53a8245a8 (Update consensus)
		// instead of triggering the timeout here, we'll just invoke the GetAcceptedFrontierFailed func
		//
		// s.router.GetAcceptedFrontierFailed(context.Background(), vID, s.ctx.ChainID, requestID)
		// -> chain.GetAcceptedFrontierFailed(context.Background(), validatorID, requestID)
		// ---> h.sendReliableMsg(message{
		//			messageType: constants.GetAcceptedFrontierFailedMsg,
		//			validatorID: validatorID,
		//			requestID:   requestID,
		//		})
		// -----> h.engine.GetAcceptedFrontierFailed(context.Background(), msg.validatorID, msg.requestID)
		// -------> return b.AcceptedFrontier(context.Background(), validatorID, requestID, nil)

		// ensure the request is made to the correct validators
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	dh := &dummyHandler{}
	bootstrapper, err := bootstrap.New(
<<<<<<< HEAD
=======
		context.Background(),
>>>>>>> 53a8245a8 (Update consensus)
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := bootstrapper.Start(0); err != nil {
=======
	if err := bootstrapper.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	// reset requested
	*requested = false
<<<<<<< HEAD
	sender.SendGetAcceptedF = func(_ context.Context, vdrs ids.NodeIDSet, reqID uint32, proposedAccepted []ids.ID) {
=======
	sender.SendGetAcceptedF = func(_ context.Context, vdrs set.Set[ids.NodeID], reqID uint32, proposedAccepted []ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	// mimic a GetAcceptedFrontierFailedMsg
	// only validator that was requested timed out on the request
	if err := bootstrapper.GetAcceptedFrontierFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	// mimic a GetAcceptedFrontierFailedMsg
	// only validator that was requested timed out on the request
	if err := bootstrapper.GetAcceptedFrontierFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	bootCfg.Ctx.Registerer = prometheus.NewRegistry()

	// re-register the Transitive
	bootstrapper2, err := bootstrap.New(
<<<<<<< HEAD
=======
		context.Background(),
>>>>>>> 53a8245a8 (Update consensus)
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := bootstrapper2.Start(0); err != nil {
=======
	if err := bootstrapper2.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if err := bootstrapper2.GetAcceptedFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper2.GetAcceptedFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}
}

func TestEngineReBootstrappingIntoConsensus(t *testing.T) {
	_, bootCfg, engCfg := DefaultConfig()
	bootCfg.Alpha = 1
	bootCfg.RetryBootstrap = true
	bootCfg.RetryBootstrapWarnFrequency = 4

	vals := validators.NewSet()
	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	peers := tracker.NewPeers()
	startup := tracker.NewStartup(peers, 0)
	vals.RegisterCallbackListener(startup)

	bootCfg.Beacons = vals
	bootCfg.Validators = vals
	bootCfg.StartupTracker = startup
	engCfg.Validators = vals

	bootCfg.SampleK = vals.Len()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	bootCfg.Sender = sender
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	bootCfg.Manager = manager
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	bootCfg.VM = vm
	engCfg.VM = vm

	vm.CantSetState = false
	vm.CantConnected = false

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	txID0 := ids.GenerateTestID()
	txID1 := ids.GenerateTestID()

	txBytes0 := []byte{0}
	txBytes1 := []byte{1}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID0,
			StatusV: choices.Processing,
		},
		BytesV: txBytes0,
	}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     txID1,
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		BytesV:        txBytes1,
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()

	vtxBytes0 := []byte{2}
	vtxBytes1 := []byte{3}

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID0,
			StatusV: choices.Processing,
		},
		HeightV: 1,
		TxsV:    []snowstorm.Tx{tx0},
		BytesV:  vtxBytes0,
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     vtxID1,
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   vtxBytes1,
	}

	requested := new(bool)
	requestID := new(uint32)
<<<<<<< HEAD
	sender.SendGetAcceptedFrontierF = func(_ context.Context, vdrs ids.NodeIDSet, reqID uint32) {
=======
	sender.SendGetAcceptedFrontierF = func(_ context.Context, vdrs set.Set[ids.NodeID], reqID uint32) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
		*requested = true
		*requestID = reqID
	}

	dh := &dummyHandler{}
	bootstrapper, err := bootstrap.New(
<<<<<<< HEAD
=======
		context.Background(),
>>>>>>> 53a8245a8 (Update consensus)
		bootCfg,
		dh.onDoneBootstrapping,
	)
	if err != nil {
		t.Fatal(err)
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}
	dh.startEngineF = te.Start

<<<<<<< HEAD
	if err := bootstrapper.Start(0); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper.Connected(vdr, version.CurrentApp); err != nil {
=======
	if err := bootstrapper.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapper.Connected(context.Background(), vdr, version.CurrentApp); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	// fail the AcceptedFrontier
	if err := bootstrapper.GetAcceptedFrontierFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	// fail the GetAcceptedFailed
	if err := bootstrapper.GetAcceptedFailed(context.Background(), vdr, *requestID); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during Initialize")
	}

	acceptedFrontier := []ids.ID{vtxID0}

	*requested = false
<<<<<<< HEAD
	sender.SendGetAcceptedF = func(_ context.Context, vdrs ids.NodeIDSet, reqID uint32, proposedAccepted []ids.ID) {
=======
	sender.SendGetAcceptedF = func(_ context.Context, vdrs set.Set[ids.NodeID], reqID uint32, proposedAccepted []ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}
<<<<<<< HEAD
		if !ids.Equals(acceptedFrontier, proposedAccepted) {
=======
		if !slices.Equal(acceptedFrontier, proposedAccepted) {
>>>>>>> 53a8245a8 (Update consensus)
			t.Fatalf("Wrong proposedAccepted vertices.\nExpected: %s\nGot: %s", acceptedFrontier, proposedAccepted)
		}
		*requested = true
		*requestID = reqID
	}

	if err := bootstrapper.AcceptedFrontier(context.Background(), vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	if !*requested {
		t.Fatalf("Should have requested from the validators during AcceptedFrontier")
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID0 {
			return nil, errMissing
		}
		t.Fatalf("Unknown vertex requested")
		panic("Unknown vertex requested")
	}

	sender.SendGetAncestorsF = func(_ context.Context, inVdr ids.NodeID, reqID uint32, vtxID ids.ID) {
		if vdr != inVdr {
			t.Fatalf("Asking wrong validator for vertex")
		}
		if vtx0.ID() != vtxID {
			t.Fatalf("Asking for wrong vertex")
		}
		*requestID = reqID
	}

	if err := bootstrapper.Accepted(context.Background(), vdr, *requestID, acceptedFrontier); err != nil {
		t.Fatal(err)
	}

	manager.GetVtxF = nil

<<<<<<< HEAD
	vm.ParseTxF = func(b []byte) (snowstorm.Tx, error) {
=======
	vm.ParseTxF = func(_ context.Context, b []byte) (snowstorm.Tx, error) {
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, txBytes0) {
			return tx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtxBytes0) {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vtxID0}
	}
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vtxID0}
	}
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID0 {
			return vtx0, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := bootstrapper.Ancestors(context.Background(), vdr, *requestID, [][]byte{vtxBytes0}); err != nil {
		t.Fatal(err)
	}

	sender.SendGetAcceptedFrontierF = nil
	sender.SendGetF = nil
	vm.ParseTxF = nil
	manager.ParseVtxF = nil
	manager.EdgeF = nil
	manager.GetVtxF = nil

	if tx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", txID0)
	}
	if vtx0.Status() != choices.Accepted {
		t.Fatalf("Should have accepted %s", vtxID0)
	}

<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtxBytes1) {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}
<<<<<<< HEAD
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, _ uint32, chits []ids.ID) {
=======
	sender.SendChitsF = func(_ context.Context, inVdr ids.NodeID, _ uint32, chits []ids.ID, _ []ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
		if inVdr != vdr {
			t.Fatalf("Sent to the wrong validator")
		}

		expected := []ids.ID{vtxID1}

<<<<<<< HEAD
		if !ids.Equals(expected, chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.SendPushQueryF = func(_ context.Context, vdrs ids.NodeIDSet, _ uint32, vtx []byte) {
=======
		if !slices.Equal(expected, chits) {
			t.Fatalf("Returned wrong chits")
		}
	}
	sender.SendPushQueryF = func(_ context.Context, vdrs set.Set[ids.NodeID], _ uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		if vdrs.Len() != 1 {
			t.Fatalf("Should have requested from the validators")
		}
		if !vdrs.Contains(vdr) {
			t.Fatalf("Should have requested from %s", vdr)
		}

		if !bytes.Equal(vtxBytes1, vtx) {
			t.Fatalf("Sent wrong query bytes")
		}
	}
<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == vtxID1 {
			return vtx1, nil
		}
		t.Fatalf("Unknown bytes provided")
		panic("Unknown bytes provided")
	}

	if err := bootstrapper.PushQuery(context.Background(), vdr, 0, vtxBytes1); err != nil {
		t.Fatal(err)
	}

	manager.ParseVtxF = nil
	sender.SendChitsF = nil
	sender.SendPushQueryF = nil
	manager.GetVtxF = nil
}

func TestEngineUndeclaredDependencyDeadlock(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
<<<<<<< HEAD
		VerifyV: errors.New(""),
=======
		VerifyV: errTest,
>>>>>>> 53a8245a8 (Update consensus)
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	te.Sender = sender

	reqID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		*reqID = requestID
	}

	if err := te.issue(context.Background(), vtx0); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) {
=======
	sender.SendPushQueryF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatalf("should have failed verification")
	}

	if err := te.issue(context.Background(), vtx1); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch vtxID {
		case vtx0.ID():
			return vtx0, nil
		case vtx1.ID():
			return vtx1, nil
		}
<<<<<<< HEAD
		return nil, errors.New("Unknown vtx")
	}

	if err := te.Chits(context.Background(), vdr, *reqID, []ids.ID{vtx1.ID()}); err != nil {
=======
		return nil, errUnknownVertex
	}

	if err := te.Chits(context.Background(), vdr, *reqID, []ids.ID{vtx1.ID()}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if status := vtx0.Status(); status != choices.Accepted {
		t.Fatalf("should have accepted the vertex due to transitive voting")
	}
}

func TestEnginePartiallyValidVertex(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
<<<<<<< HEAD
		VerifyV: errors.New(""),
=======
		VerifyV: errTest,
>>>>>>> 53a8245a8 (Update consensus)
	}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0, tx1},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	expectedVtxBytes := []byte{1}
<<<<<<< HEAD
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		return &lux.TestVertex{
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		return &avalanche.TestVertex{
=======
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		return &lux.TestVertex{
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: vts,
			HeightV:  1,
			TxsV:     txs,
			BytesV:   expectedVtxBytes,
		}, nil
	}

	sender := &common.SenderTest{T: t}
	te.Sender = sender

<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, _ uint32, vtx []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		if !bytes.Equal(expectedVtxBytes, vtx) {
			t.Fatalf("wrong vertex queried")
		}
	}

	if err := te.issue(context.Background(), vtx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineGossip(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
		t.Fatal(err)
	}

	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID()} }
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
	if err := te.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID()}
	}
	manager.GetVtxF = func(vtxID ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID()} }
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if vtxID == gVtx.ID() {
			return gVtx, nil
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	called := new(bool)
	sender.SendGossipF = func(_ context.Context, vtxBytes []byte) {
		*called = true
		if !bytes.Equal(vtxBytes, gVtx.Bytes()) {
			t.Fatal(errUnknownVertex)
		}
	}

<<<<<<< HEAD
	if err := te.Gossip(); err != nil {
=======
	if err := te.Gossip(context.Background()); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if !*called {
		t.Fatalf("Should have gossiped the vertex")
	}
}

func TestEngineInvalidVertexIgnoredFromUnexpectedPeer(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	secondVdr := ids.GenerateTestNodeID()

<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.AddWeight(secondVdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(secondVdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(secondVdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(secondVdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(secondVdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}

	vts := []lux.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{1},
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   []byte{2},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	parsed := new(bool)
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx1.Bytes()) {
			*parsed = true
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx1.ID() {
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, reqVdr ids.NodeID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if reqVdr != vdr {
			t.Fatalf("Wrong validator requested")
		}
		if vtxID != vtx0.ID() {
			t.Fatalf("Wrong vertex requested")
		}
	}

	if err := te.PushQuery(context.Background(), vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}

	if err := te.Put(context.Background(), secondVdr, *reqID, []byte{3}); err != nil {
		t.Fatal(err)
	}

	*parsed = false
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx0.Bytes()) {
			*parsed = true
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx0.ID() {
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	vtx0.StatusV = choices.Processing

	if err := te.Put(context.Background(), vdr, *reqID, vtx0.Bytes()); err != nil {
		t.Fatal(err)
	}

	prefs := te.Consensus.Preferences()
	if !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending vertex")
	}
}

func TestEnginePushQueryRequestIDConflict(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}

	vts := []lux.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{1},
	}

	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   []byte{2},
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	parsed := new(bool)
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx1.Bytes()) {
			*parsed = true
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx1.ID() {
			return vtx1, nil
		}
		return nil, errUnknownVertex
	}

	reqID := new(uint32)
	sender.SendGetF = func(_ context.Context, reqVdr ids.NodeID, requestID uint32, vtxID ids.ID) {
		*reqID = requestID
		if reqVdr != vdr {
			t.Fatalf("Wrong validator requested")
		}
		if vtxID != vtx0.ID() {
			t.Fatalf("Wrong vertex requested")
		}
	}

	if err := te.PushQuery(context.Background(), vdr, 0, vtx1.Bytes()); err != nil {
		t.Fatal(err)
	}

	sender.SendGetF = nil
	sender.CantSendGet = false

	if err := te.PushQuery(context.Background(), vdr, *reqID, []byte{3}); err != nil {
		t.Fatal(err)
	}

	*parsed = false
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx0.Bytes()) {
			*parsed = true
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx0.ID() {
			return vtx0, nil
		}
		return nil, errUnknownVertex
	}
	sender.CantSendPushQuery = false
	sender.CantSendChits = false

	vtx0.StatusV = choices.Processing

	if err := te.Put(context.Background(), vdr, *reqID, vtx0.Bytes()); err != nil {
		t.Fatal(err)
	}

	prefs := te.Consensus.Preferences()
	if !prefs.Contains(vtx1.ID()) {
		t.Fatalf("Shouldn't have abandoned the pending vertex")
	}
}

func TestEngineAggressivePolling(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	engCfg.Params.ConcurrentRepolls = 3
	engCfg.Params.BetaRogue = 3

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	engCfg.VM = vm

	gVtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		BytesV: []byte{0},
	}

	vts := []lux.Vertex{gVtx}
	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx0.InputIDsV = append(tx0.InputIDsV, utxos[0])

	tx1 := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx1.InputIDsV = append(tx1.InputIDsV, utxos[1])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx0},
		BytesV:   []byte{1},
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
	parsed := new(bool)
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx.Bytes()) {
			*parsed = true
			return vtx, nil
		}
		return nil, errUnknownVertex
	}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if !*parsed {
			return nil, errUnknownVertex
		}

		if vtxID == vtx.ID() {
			return vtx, nil
		}
		return nil, errUnknownVertex
	}

	numPushQueries := new(int)
<<<<<<< HEAD
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) { *numPushQueries++ }

	numPullQueries := new(int)
	sender.SendPullQueryF = func(context.Context, ids.NodeIDSet, uint32, ids.ID) { *numPullQueries++ }
=======
<<<<<<< HEAD
<<<<<<< HEAD
	sender.SendPushQueryF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) {
=======
	sender.SendPushQueryF = func(context.Context, ids.NodeIDSet, uint32, []byte) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	sender.SendPushQueryF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) {
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
		*numPushQueries++
	}

	numPullQueries := new(int)
<<<<<<< HEAD
<<<<<<< HEAD
	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID) {
=======
	sender.SendPullQueryF = func(context.Context, ids.NodeIDSet, uint32, ids.ID) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	sender.SendPullQueryF = func(context.Context, set.Set[ids.NodeID], uint32, ids.ID) {
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
		*numPullQueries++
	}
>>>>>>> 53a8245a8 (Update consensus)

	vm.CantPendingTxs = false

	if err := te.Put(context.Background(), vdr, 0, vtx.Bytes()); err != nil {
		t.Fatal(err)
	}

	if *numPushQueries != 1 {
		t.Fatalf("should have issued one push query")
	}
	if *numPullQueries != 2 {
		t.Fatalf("should have issued two pull queries")
	}
}

func TestEngineDuplicatedIssuance(t *testing.T) {
	_, _, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 5
	engCfg.Params.BetaRogue = 5

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	engCfg.Manager = manager

	manager.Default(true)

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	engCfg.VM = vm

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
	}
	tx.InputIDsV = append(tx.InputIDsV, utxos[0])

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
<<<<<<< HEAD
	lastVtx := new(lux.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		lastVtx = &lux.TestVertex{
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	lastVtx := new(avalanche.TestVertex)
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
		lastVtx = &avalanche.TestVertex{
=======
	lastVtx := new(lux.TestVertex)
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
		lastVtx = &lux.TestVertex{
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentsV: []lux.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}
		return lastVtx, nil
	}

	sender.CantSendPushQuery = false

<<<<<<< HEAD
	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx} }
	if err := te.Notify(common.PendingTxs); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
		return []snowstorm.Tx{tx}
	}
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if len(lastVtx.TxsV) != 1 || lastVtx.TxsV[0].ID() != tx.ID() {
		t.Fatalf("Should have issued txs differently")
	}

<<<<<<< HEAD
	manager.BuildVtxF = func([]ids.ID, []snowstorm.Tx) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.BuildVtxF = func(context.Context, []ids.ID, []snowstorm.Tx) (avalanche.Vertex, error) {
=======
	manager.BuildVtxF = func([]ids.ID, []snowstorm.Tx) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatalf("shouldn't have attempted to issue a duplicated tx")
		return nil, nil
	}

<<<<<<< HEAD
	if err := te.Notify(common.PendingTxs); err != nil {
=======
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}
}

func TestEngineDoubleChit(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	engCfg.Params.Alpha = 2
	engCfg.Params.K = 2
	engCfg.Params.MixedQueryNumPushNonVdr = 2

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr0 := ids.GenerateTestNodeID()
	vdr1 := ids.GenerateTestNodeID()

<<<<<<< HEAD
	if err := vals.AddWeight(vdr0, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.AddWeight(vdr1, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr0, nil, ids.Empty, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(vdr1, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr0, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(vdr1, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr0, nil, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(vdr1, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr0, nil, ids.Empty, 1); err != nil {
		t.Fatal(err)
	}
	if err := vals.Add(vdr1, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	vts := []lux.Vertex{gVtx, mVtx}
	utxos := []ids.ID{ids.GenerateTestID()}

	tx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Processing,
	}}
	tx.InputIDsV = append(tx.InputIDsV, utxos[0])

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: vts,
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx},
		BytesV:   []byte{1, 1, 2, 3},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{vts[0].ID(), vts[1].ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{vts[0].ID(), vts[1].ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	reqID := new(uint32)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtxBytes []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtxBytes []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		*reqID = requestID
		if inVdrs.Len() != 2 {
			t.Fatalf("Wrong number of validators")
		}
		if !bytes.Equal(vtx.Bytes(), vtxBytes) {
			t.Fatalf("Wrong vertex requested")
		}
	}
<<<<<<< HEAD
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if id == vtx.ID() {
			return vtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	if err := te.issue(context.Background(), vtx); err != nil {
		t.Fatal(err)
	}

	votes := []ids.ID{vtx.ID()}

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr0, *reqID, votes); err != nil {
=======
	if err := te.Chits(context.Background(), vdr0, *reqID, votes, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr0, *reqID, votes); err != nil {
=======
	if err := te.Chits(context.Background(), vdr0, *reqID, votes, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if status := tx.Status(); status != choices.Processing {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Processing)
	}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr1, *reqID, votes); err != nil {
=======
	if err := te.Chits(context.Background(), vdr1, *reqID, votes, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if status := tx.Status(); status != choices.Accepted {
		t.Fatalf("Wrong tx status: %s ; expected: %s", status, choices.Accepted)
	}
}

func TestEngineBubbleVotes(t *testing.T) {
	_, _, engCfg := DefaultConfig()

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	err := vals.AddWeight(vdr, 1)
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	err := vals.Add(vdr, nil, ids.Empty, 1)
=======
	err := vals.Add(vdr, 1)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	err := vals.Add(vdr, nil, 1)
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	err := vals.Add(vdr, nil, ids.Empty, 1)
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
	require.NoError(t, err)

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

	utxos := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[:1],
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:2],
	}
	tx2 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		InputIDsV: utxos[1:2],
	}

	vtx := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
		TxsV:    []snowstorm.Tx{tx0},
		BytesV:  []byte{0},
	}

	missingVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Unknown,
	}}

	pendingVtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{vtx, missingVtx},
		HeightV:  1,
		TxsV:     []snowstorm.Tx{tx1},
		BytesV:   []byte{1},
	}

	pendingVtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []lux.Vertex{pendingVtx0},
		HeightV:  2,
		TxsV:     []snowstorm.Tx{tx2},
		BytesV:   []byte{2},
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return nil }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return nil
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return nil
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return nil
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return nil }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case vtx.ID():
			return vtx, nil
		case missingVtx.ID():
			return nil, errMissing
		case pendingVtx0.ID():
			return pendingVtx0, nil
		case pendingVtx1.ID():
			return pendingVtx1, nil
		}
		require.FailNow(t, "unknown vertex", "vtxID: %s", id)
		panic("should have errored")
	}

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	queryReqID := new(uint32)
	queried := new(bool)
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtxBytes []byte) {
=======
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtxBytes []byte) {
>>>>>>> 53a8245a8 (Update consensus)
		require.Len(t, inVdrs, 1, "wrong number of validators")
		*queryReqID = requestID
		require.Equal(t, vtx.Bytes(), vtxBytes, "wrong vertex requested")
		*queried = true
	}

	getReqID := new(uint32)
	fetched := new(bool)
	sender.SendGetF = func(_ context.Context, inVdr ids.NodeID, requestID uint32, vtxID ids.ID) {
		require.Equal(t, vdr, inVdr, "wrong validator")
		*getReqID = requestID
		require.Equal(t, missingVtx.ID(), vtxID, "wrong vertex requested")
		*fetched = true
	}

	issued, err := te.issueFrom(context.Background(), vdr, pendingVtx1)
	require.NoError(t, err)
	require.False(t, issued, "shouldn't have been able to issue %s", pendingVtx1.ID())
	require.True(t, *queried, "should have queried for %s", vtx.ID())
	require.True(t, *fetched, "should have fetched %s", missingVtx.ID())

	// can't apply votes yet because pendingVtx0 isn't issued because missingVtx
	// is missing
<<<<<<< HEAD
	err = te.Chits(context.Background(), vdr, *queryReqID, []ids.ID{pendingVtx1.ID()})
=======
	err = te.Chits(context.Background(), vdr, *queryReqID, []ids.ID{pendingVtx1.ID()}, nil)
>>>>>>> 53a8245a8 (Update consensus)
	require.NoError(t, err)
	require.Equal(t, choices.Processing, tx0.Status(), "wrong tx status")
	require.Equal(t, choices.Processing, tx1.Status(), "wrong tx status")

	// vote for pendingVtx1 should be bubbled up to pendingVtx0 and then to vtx
	err = te.GetFailed(context.Background(), vdr, *getReqID)
	require.NoError(t, err)
	require.Equal(t, choices.Accepted, tx0.Status(), "wrong tx status")
	require.Equal(t, choices.Processing, tx1.Status(), "wrong tx status")
}

func TestEngineIssue(t *testing.T) {
	_, _, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 1
	engCfg.Params.BetaRogue = 1
	engCfg.Params.OptimalProcessing = 1

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := vals.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := vals.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := vals.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := vals.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	engCfg.VM = vm

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx0 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
		InputIDsV:     utxos[:1],
	}
	tx1 := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
		InputIDsV:     utxos[1:],
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
<<<<<<< HEAD
<<<<<<< HEAD
========
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
<<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.EdgeF = func() []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
	manager.EdgeF = func() []ids.ID { return []ids.ID{gVtx.ID(), mVtx.ID()} }
	manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>>> 53a8245a8 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	vm.CantSetState = true
	numBuilt := 0
	vtxID := ids.GenerateTestID()
<<<<<<< HEAD
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.BuildVtxF = func(_ context.Context, _ []ids.ID, txs []snowstorm.Tx) (avalanche.Vertex, error) {
=======
	manager.BuildVtxF = func(_ []ids.ID, txs []snowstorm.Tx) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		numBuilt++
		vtx := &lux.TestVertex{
			TestDecidable: choices.TestDecidable{
				IDV:     vtxID,
				StatusV: choices.Processing,
			},
			ParentsV: []lux.Vertex{gVtx, mVtx},
			HeightV:  1,
			TxsV:     txs,
			BytesV:   []byte{1},
		}

<<<<<<< HEAD
		manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
		manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
=======
		manager.GetVtxF = func(id ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
			switch id {
			case gVtx.ID():
				return gVtx, nil
			case mVtx.ID():
				return mVtx, nil
			case vtx.ID():
				return vtx, nil
			}
			t.Fatalf("Unknown vertex")
			panic("Should have errored")
		}

		return vtx, nil
	}

	var queryRequestID uint32
<<<<<<< HEAD
	sender.SendPushQueryF = func(_ context.Context, _ ids.NodeIDSet, requestID uint32, _ []byte) {
		queryRequestID = requestID
	}

	vm.PendingTxsF = func() []snowstorm.Tx { return []snowstorm.Tx{tx0, tx1} }
	if err := te.Notify(common.PendingTxs); err != nil {
=======
	sender.SendPushQueryF = func(_ context.Context, _ set.Set[ids.NodeID], requestID uint32, _ []byte) {
		queryRequestID = requestID
	}

<<<<<<< HEAD
<<<<<<< HEAD
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
=======
	vm.PendingTxsF = func() []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(common.PendingTxs); err != nil {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
	vm.PendingTxsF = func(context.Context) []snowstorm.Tx {
		return []snowstorm.Tx{tx0, tx1}
	}
	if err := te.Notify(context.Background(), common.PendingTxs); err != nil {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if numBuilt != 1 {
		t.Fatalf("Should have issued txs differently")
	}

<<<<<<< HEAD
	if err := te.Chits(context.Background(), vdr, queryRequestID, []ids.ID{vtxID}); err != nil {
=======
	if err := te.Chits(context.Background(), vdr, queryRequestID, []ids.ID{vtxID}, nil); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	if numBuilt != 2 {
		t.Fatalf("Should have issued txs differently")
	}
}

// Test that a transaction is abandoned if a dependency fails verification,
// even if there are outstanding requests for vertices when the
// dependency fails verification.
func TestAbandonTx(t *testing.T) {
	require := require.New(t)
	_, _, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 1
	engCfg.Params.BetaRogue = 1
	engCfg.Params.OptimalProcessing = 1

	sender := &common.SenderTest{
		T:                           t,
		CantSendGetAcceptedFrontier: false,
	}
	sender.Default(true)
	engCfg.Sender = sender

	engCfg.Validators = validators.NewSet()
	vdr := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := engCfg.Validators.AddWeight(vdr, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := engCfg.Validators.Add(vdr, nil, ids.Empty, 1); err != nil {
=======
	if err := engCfg.Validators.Add(vdr, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := engCfg.Validators.Add(vdr, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := engCfg.Validators.Add(vdr, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	manager.CantEdge = false
	manager.CantGetVtx = false
	engCfg.Manager = manager

<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> 53a8245a8 (Update consensus)
	vm.Default(true)
	vm.CantSetState = false

	engCfg.VM = vm

	te, err := newTransitive(engCfg)
	if err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	if err := te.Start(0); err != nil {
=======
	if err := te.Start(context.Background(), 0); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	gVtx := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	tx0 := &snowstorm.TestTx{ // Fails verification
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
		InputIDsV:     []ids.ID{gTx.ID()},
		BytesV:        utils.RandomBytes(32),
<<<<<<< HEAD
		VerifyV:       errors.New(""),
=======
		VerifyV:       errTest,
>>>>>>> 53a8245a8 (Update consensus)
	}

	tx1 := &snowstorm.TestTx{ // Depends on tx0
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{tx0},
		InputIDsV:     []ids.ID{gTx.ID()},
		BytesV:        utils.RandomBytes(32),
	}

	vtx0 := &lux.TestVertex{ // Contains tx0, which will fail verification
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: []lux.Vertex{gVtx},
		HeightV:  gVtx.HeightV + 1,
		TxsV:     []snowstorm.Tx{tx0},
	}

	// Contains tx1, which depends on tx0.
	// vtx0 and vtx1 are siblings.
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		ParentsV: []lux.Vertex{gVtx},
		HeightV:  gVtx.HeightV + 1,
		TxsV:     []snowstorm.Tx{tx1},
	}

	// Cause the engine to send a Get request for vtx1, vtx0, and some other vtx that doesn't exist
	sender.CantSendGet = false
	sender.CantSendChits = false
	err = te.PullQuery(context.Background(), vdr, 0, vtx1.ID())
	require.NoError(err)
	err = te.PullQuery(context.Background(), vdr, 0, vtx0.ID())
	require.NoError(err)
	err = te.PullQuery(context.Background(), vdr, 0, ids.GenerateTestID())
	require.NoError(err)

	// Give the engine vtx1. It should wait to issue vtx1
	// until tx0 is issued, because tx1 depends on tx0.
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx1.BytesV) {
			vtx1.StatusV = choices.Processing
			return vtx1, nil
		}
		require.FailNow("should have asked to parse vtx1")
<<<<<<< HEAD
		return nil, errors.New("should have asked to parse vtx1")
=======
		return nil, nil
>>>>>>> 53a8245a8 (Update consensus)
	}
	err = te.Put(context.Background(), vdr, 0, vtx1.Bytes())
	require.NoError(err)

	// Verify that vtx1 is waiting to be issued.
	require.True(te.pending.Contains(vtx1.ID()))

	// Give the engine vtx0. It should try to issue vtx0
	// but then abandon it because tx0 fails verification.
<<<<<<< HEAD
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
	manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
	manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
		if bytes.Equal(b, vtx0.BytesV) {
			vtx0.StatusV = choices.Processing
			return vtx0, nil
		}
		require.FailNow("should have asked to parse vtx0")
<<<<<<< HEAD
		return nil, errors.New("should have asked to parse vtx0")
=======
		return nil, nil
>>>>>>> 53a8245a8 (Update consensus)
	}
	err = te.Put(context.Background(), vdr, 0, vtx0.Bytes())
	require.NoError(err)

	// Despite the fact that there is still an outstanding vertex request,
	// vtx1 should have been abandoned because tx0 failed verification
	require.False(te.pending.Contains(vtx1.ID()))
	// sanity check that there is indeed an outstanding vertex request
	require.True(te.outstandingVtxReqs.Len() == 1)
}

func TestSendMixedQuery(t *testing.T) {
	type test struct {
		isVdr bool
	}
	tests := []test{
		{isVdr: true},
		{isVdr: false},
	}
	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("is validator: %v", tt.isVdr),
			func(t *testing.T) {
				_, _, engCfg := DefaultConfig()
				sender := &common.SenderTest{T: t}
				engCfg.Sender = sender
				sender.Default(true)
<<<<<<< HEAD
				vdrSet := engCfg.Validators
=======
>>>>>>> 53a8245a8 (Update consensus)
				manager := vertex.NewTestManager(t)
				engCfg.Manager = manager
				// Override the parameters k, MixedQueryNumPushVdr, MixedQueryNumPushNonVdr,
				// and update the validator set to have k validators.
				engCfg.Params.K = 20
				engCfg.Params.Alpha = 12
				engCfg.Params.MixedQueryNumPushVdr = 12
				engCfg.Params.MixedQueryNumPushNonVdr = 11
				te, err := newTransitive(engCfg)
				if err != nil {
					t.Fatal(err)
				}
				startReqID := uint32(0)
<<<<<<< HEAD
				if err := te.Start(startReqID); err != nil {
					t.Fatal(err)
				}

				vdrsList := []validators.Validator{}
				vdrs := ids.NodeIDSet{}
				for i := 0; i < te.Config.Params.K; i++ {
					vdr := ids.GenerateTestNodeID()
					vdrs.Add(vdr)
					vdrsList = append(vdrsList, validators.NewValidator(vdr, 1))
				}
				if tt.isVdr {
					vdrs.Add(te.Ctx.NodeID)
					vdrsList = append(vdrsList, validators.NewValidator(te.Ctx.NodeID, 1))
				}
				if err := vdrSet.Set(vdrsList); err != nil {
					t.Fatal(err)
=======
				if err := te.Start(context.Background(), startReqID); err != nil {
					t.Fatal(err)
				}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
				vdrs := set.Set[ids.NodeID]{}
				te.Validators = validators.NewSet()
				for i := 0; i < engCfg.Params.K; i++ {
					vdrID := ids.GenerateTestNodeID()
					vdrs.Add(vdrID)
					err := te.Validators.Add(vdrID, nil, ids.Empty, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				if tt.isVdr {
					vdrs.Add(engCfg.Ctx.NodeID)
					err := te.Validators.Add(engCfg.Ctx.NodeID, nil, ids.Empty, 1)
=======
				vdrsList := []validators.Validator{}
=======
>>>>>>> 3e2b5865d (Convert validators.Validator into a struct (#2185))
				vdrs := ids.NodeIDSet{}
				te.Validators = validators.NewSet()
				for i := 0; i < engCfg.Params.K; i++ {
					vdrID := ids.GenerateTestNodeID()
					vdrs.Add(vdrID)
					err := te.Validators.Add(vdrID, nil, ids.Empty, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				if tt.isVdr {
					vdrs.Add(engCfg.Ctx.NodeID)
<<<<<<< HEAD
<<<<<<< HEAD
					vdrsList = append(vdrsList, validators.NewValidator(engCfg.Ctx.NodeID, nil, 1))
				}
				te.Validators = validators.NewSet()
				for _, vdr := range vdrsList {
<<<<<<< HEAD
<<<<<<< HEAD
					err := te.Validators.AddWeight(vdr.ID(), vdr.Weight())
>>>>>>> 1437bfe45 (Remove validators.Set#Set from the interface (#2275))
=======
					err := te.Validators.Add(vdr.ID(), vdr.Weight())
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
					err := te.Validators.Add(vdr.ID(), nil, vdr.Weight())
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
					err := te.Validators.Add(engCfg.Ctx.NodeID, nil, 1)
>>>>>>> 3e2b5865d (Convert validators.Validator into a struct (#2185))
=======
					err := te.Validators.Add(engCfg.Ctx.NodeID, nil, ids.Empty, 1)
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
					if err != nil {
						t.Fatal(err)
					}
>>>>>>> 53a8245a8 (Update consensus)
				}

				// [blk1] is a child of [gBlk] and passes verification
				vtx1 := &lux.TestVertex{
					TestDecidable: choices.TestDecidable{
						IDV:     ids.GenerateTestID(),
						StatusV: choices.Processing,
					},
					ParentsV: []lux.Vertex{
						&lux.TestVertex{TestDecidable: choices.TestDecidable{
							IDV:     ids.GenerateTestID(),
							StatusV: choices.Accepted,
						}},
					},
					BytesV: []byte{1},
				}

<<<<<<< HEAD
				manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/transitive_test.go
				manager.ParseVtxF = func(_ context.Context, b []byte) (avalanche.Vertex, error) {
=======
				manager.ParseVtxF = func(b []byte) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/transitive_test.go
>>>>>>> 53a8245a8 (Update consensus)
					switch {
					case bytes.Equal(b, vtx1.Bytes()):
						return vtx1, nil
					default:
						t.Fatalf("Unknown block bytes")
						return nil, nil
					}
				}

				pullQuerySent := new(bool)
				pullQueryReqID := new(uint32)
<<<<<<< HEAD
				pullQueriedVdrs := ids.NodeIDSet{}
				sender.SendPullQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtxID ids.ID) {
=======
				pullQueriedVdrs := set.Set[ids.NodeID]{}
				sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtxID ids.ID) {
>>>>>>> 53a8245a8 (Update consensus)
					switch {
					case *pullQuerySent:
						t.Fatalf("Asked multiple times")
					case vtxID != vtx1.ID():
						t.Fatalf("Expected engine to request vtx1")
					}
					pullQueriedVdrs.Union(inVdrs)
					*pullQuerySent = true
					*pullQueryReqID = requestID
				}

				pushQuerySent := new(bool)
				pushQueryReqID := new(uint32)
<<<<<<< HEAD
				pushQueriedVdrs := ids.NodeIDSet{}
				sender.SendPushQueryF = func(_ context.Context, inVdrs ids.NodeIDSet, requestID uint32, vtx []byte) {
=======
				pushQueriedVdrs := set.Set[ids.NodeID]{}
				sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtx []byte) {
>>>>>>> 53a8245a8 (Update consensus)
					switch {
					case *pushQuerySent:
						t.Fatal("Asked multiple times")
					case !bytes.Equal(vtx, vtx1.Bytes()):
						t.Fatal("got unexpected block bytes instead of blk1")
					}
					*pushQuerySent = true
					*pushQueryReqID = requestID
					pushQueriedVdrs.Union(inVdrs)
				}

				// Give the engine vtx1. It should insert it into consensus and send a mixed query
				// consisting of 12 pull queries and 8 push queries.
<<<<<<< HEAD
				if err := te.Put(context.Background(), vdrSet.List()[0].ID(), constants.GossipMsgRequestID, vtx1.Bytes()); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
				if err := te.Put(context.Background(), te.Validators.List()[0].NodeID, constants.GossipMsgRequestID, vtx1.Bytes()); err != nil {
=======
				if err := te.Put(context.Background(), te.Validators.List()[0].ID(), constants.GossipMsgRequestID, vtx1.Bytes()); err != nil {
>>>>>>> 1437bfe45 (Remove validators.Set#Set from the interface (#2275))
=======
				if err := te.Put(context.Background(), te.Validators.List()[0].NodeID, constants.GossipMsgRequestID, vtx1.Bytes()); err != nil {
>>>>>>> 3e2b5865d (Convert validators.Validator into a struct (#2185))
>>>>>>> 53a8245a8 (Update consensus)
					t.Fatal(err)
				}

				switch {
				case !*pullQuerySent:
					t.Fatal("expected us to send pull queries")
				case !*pushQuerySent:
					t.Fatal("expected us to send push queries")
				case *pushQueryReqID != *pullQueryReqID:
					t.Fatalf("expected equal push query (%v) and pull query (%v) req IDs", *pushQueryReqID, *pullQueryReqID)
				case pushQueriedVdrs.Len()+pullQueriedVdrs.Len() != te.Config.Params.K:
					t.Fatalf("expected num push queried (%d) + num pull queried (%d) to be %d", pushQueriedVdrs.Len(), pullQueriedVdrs.Len(), te.Config.Params.K)
				case tt.isVdr && pushQueriedVdrs.Len() != te.Params.MixedQueryNumPushVdr:
					t.Fatalf("expected num push queried (%d) to be %d", pullQueriedVdrs.Len(), te.Params.MixedQueryNumPushVdr)
				case !tt.isVdr && pushQueriedVdrs.Len() != te.Params.MixedQueryNumPushNonVdr:
					t.Fatalf("expected num push queried (%d) to be %d", pullQueriedVdrs.Len(), te.Params.MixedQueryNumPushNonVdr)
				}

				pullQueriedVdrs.Union(pushQueriedVdrs) // Now this holds all queried validators (push and pull)
				for vdr := range pullQueriedVdrs {
					if !vdrs.Contains(vdr) {
						t.Fatalf("got unexpected vdr %v", vdr)
					}
				}
			})
	}
}
<<<<<<< HEAD
=======

func TestEngineApplyAcceptedFrontierInQueryFailed(t *testing.T) {
	require := require.New(t)

	_, _, engCfg := DefaultConfig()
	engCfg.Params.BatchSize = 1
	engCfg.Params.BetaVirtuous = 2
	engCfg.Params.BetaRogue = 2
	engCfg.Params.OptimalProcessing = 1

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false
	engCfg.Sender = sender

	vals := validators.NewSet()
	engCfg.Validators = vals

	vdr := ids.GenerateTestNodeID()
	require.NoError(vals.Add(vdr, nil, ids.Empty, 1))

	manager := vertex.NewTestManager(t)
	manager.Default(true)
	engCfg.Manager = manager

<<<<<<< HEAD
<<<<<<< HEAD
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
=======
	vm := &vertex.TestVM{TestVM: common.TestVM{T: t}}
>>>>>>> 007ea3cdf (Apply accepted frontier rather than failing a query (#2135))
=======
	vm := &vertex.TestVM{TestVM: block.TestVM{TestVM: common.TestVM{T: t}}}
>>>>>>> db5704fcd (Update DAGVM interface to support linearization (#2442))
	vm.Default(true)
	engCfg.VM = vm

	gVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}
	mVtx := &avalanche.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	gTx := &snowstorm.TestTx{TestDecidable: choices.TestDecidable{
		IDV:     ids.GenerateTestID(),
		StatusV: choices.Accepted,
	}}

	utxos := []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}

	tx := &snowstorm.TestTx{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		DependenciesV: []snowstorm.Tx{gTx},
		InputIDsV:     utxos[:1],
	}

	manager.EdgeF = func(context.Context) []ids.ID {
		return []ids.ID{gVtx.ID(), mVtx.ID()}
	}
	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		}
		t.Fatalf("Unknown vertex")
		panic("Should have errored")
	}

	vm.CantSetState = false
	te, err := newTransitive(engCfg)
	require.NoError(err)
	require.NoError(te.Start(context.Background(), 0))

	vtx := &avalanche.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentsV: []avalanche.Vertex{gVtx, mVtx},
		TxsV:     []snowstorm.Tx{tx},
		BytesV:   utils.RandomBytes(32),
	}

	queryRequestID := new(uint32)
	sender.SendPushQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtxBytes []byte) {
		require.Contains(inVdrs, vdr)
		require.Equal(vtx.Bytes(), vtxBytes)
		*queryRequestID = requestID
	}

	require.NoError(te.issue(context.Background(), vtx))

	manager.GetVtxF = func(_ context.Context, id ids.ID) (avalanche.Vertex, error) {
		switch id {
		case gVtx.ID():
			return gVtx, nil
		case mVtx.ID():
			return mVtx, nil
		case vtx.ID():
			return vtx, nil
		}
		t.Fatalf("unknown vertex")
		panic("Should have errored")
	}

	require.Equal(choices.Processing, vtx.Status())

	sender.SendPullQueryF = func(_ context.Context, inVdrs set.Set[ids.NodeID], requestID uint32, vtxID ids.ID) {
		require.Contains(inVdrs, vdr)
		require.Equal(vtx.ID(), vtxID)
		*queryRequestID = requestID
	}

	vtxIDs := []ids.ID{vtx.ID()}
	require.NoError(te.Chits(context.Background(), vdr, *queryRequestID, vtxIDs, vtxIDs))

	require.Equal(choices.Processing, vtx.Status())

	require.NoError(te.QueryFailed(context.Background(), vdr, *queryRequestID))

	require.Equal(choices.Accepted, vtx.Status())
}
>>>>>>> 53a8245a8 (Update consensus)
