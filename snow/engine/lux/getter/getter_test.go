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

package getter

import (
	"context"
	"errors"
	"testing"

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/getter/getter_test.go
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/snow/engine/common"
	"github.com/luxdefi/node/snow/validators"
	"github.com/luxdefi/node/utils/set"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow"
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/snow/validators"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/getter/getter_test.go
>>>>>>> 53a8245a8 (Update consensus)
)

var errUnknownVertex = errors.New("unknown vertex")

func testSetup(t *testing.T) (*vertex.TestManager, *common.SenderTest, common.Config) {
	peers := validators.NewSet()
	peer := ids.GenerateTestNodeID()
<<<<<<< HEAD
	if err := peers.AddWeight(peer, 1); err != nil {
=======
<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
	if err := peers.Add(peer, nil, ids.Empty, 1); err != nil {
=======
	if err := peers.Add(peer, 1); err != nil {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
	if err := peers.Add(peer, nil, 1); err != nil {
>>>>>>> 4d169e12a (Add BLS keys to validator set (#2073))
=======
	if err := peers.Add(peer, nil, ids.Empty, 1); err != nil {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
>>>>>>> 53a8245a8 (Update consensus)
		t.Fatal(err)
	}

	sender := &common.SenderTest{T: t}
	sender.Default(true)
	sender.CantSendGetAcceptedFrontier = false

	isBootstrapped := false
	subnet := &common.SubnetTest{
<<<<<<< HEAD
		T:               t,
		IsBootstrappedF: func() bool { return isBootstrapped },
		BootstrappedF:   func(ids.ID) { isBootstrapped = true },
=======
		T: t,
		IsBootstrappedF: func() bool {
			return isBootstrapped
		},
		BootstrappedF: func(ids.ID) {
			isBootstrapped = true
		},
>>>>>>> 53a8245a8 (Update consensus)
	}

	commonConfig := common.Config{
		Ctx:                            snow.DefaultConsensusContextTest(),
		Validators:                     peers,
		Beacons:                        peers,
		SampleK:                        peers.Len(),
		Alpha:                          peers.Weight()/2 + 1,
		Sender:                         sender,
		Subnet:                         subnet,
		Timer:                          &common.TimerTest{},
		AncestorsMaxContainersSent:     2000,
		AncestorsMaxContainersReceived: 2000,
		SharedCfg:                      &common.SharedConfig{},
	}

	manager := vertex.NewTestManager(t)
	manager.Default(true)

	return manager, sender, commonConfig
}

func TestAcceptedFrontier(t *testing.T) {
	manager, sender, config := testSetup(t)

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()
	vtxID2 := ids.GenerateTestID()

	bsIntf, err := New(manager, config)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := bsIntf.(*getter)
	if !ok {
		t.Fatal("Unexpected get handler")
	}

<<<<<<< HEAD
	manager.EdgeF = func() []ids.ID {
=======
	manager.EdgeF = func(context.Context) []ids.ID {
>>>>>>> 53a8245a8 (Update consensus)
		return []ids.ID{
			vtxID0,
			vtxID1,
		}
	}

	var accepted []ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	if err := bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	acceptedSet := ids.Set{}
=======
	acceptedSet := set.Set[ids.ID]{}
>>>>>>> 53a8245a8 (Update consensus)
	acceptedSet.Add(accepted...)

	manager.EdgeF = nil

	if !acceptedSet.Contains(vtxID0) {
		t.Fatalf("Vtx should be accepted")
	}
	if !acceptedSet.Contains(vtxID1) {
		t.Fatalf("Vtx should be accepted")
	}
	if acceptedSet.Contains(vtxID2) {
		t.Fatalf("Vtx shouldn't be accepted")
	}
}

func TestFilterAccepted(t *testing.T) {
	manager, sender, config := testSetup(t)

	vtxID0 := ids.GenerateTestID()
	vtxID1 := ids.GenerateTestID()
	vtxID2 := ids.GenerateTestID()

	vtx0 := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     vtxID0,
		StatusV: choices.Accepted,
	}}
	vtx1 := &lux.TestVertex{TestDecidable: choices.TestDecidable{
		IDV:     vtxID1,
		StatusV: choices.Accepted,
	}}

	bsIntf, err := New(manager, config)
	if err != nil {
		t.Fatal(err)
	}
	bs, ok := bsIntf.(*getter)
	if !ok {
		t.Fatal("Unexpected get handler")
	}

	vtxIDs := []ids.ID{vtxID0, vtxID1, vtxID2}

<<<<<<< HEAD
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
=======
<<<<<<< HEAD:snow/engine/avalanche/getter/getter_test.go
	manager.GetVtxF = func(_ context.Context, vtxID ids.ID) (avalanche.Vertex, error) {
=======
	manager.GetVtxF = func(vtxID ids.ID) (lux.Vertex, error) {
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/getter/getter_test.go
>>>>>>> 53a8245a8 (Update consensus)
		switch vtxID {
		case vtxID0:
			return vtx0, nil
		case vtxID1:
			return vtx1, nil
		case vtxID2:
			return nil, errUnknownVertex
		}
		t.Fatal(errUnknownVertex)
		return nil, errUnknownVertex
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	if err := bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, vtxIDs); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	acceptedSet := ids.Set{}
=======
	acceptedSet := set.Set[ids.ID]{}
>>>>>>> 53a8245a8 (Update consensus)
	acceptedSet.Add(accepted...)

	manager.GetVtxF = nil

	if !acceptedSet.Contains(vtxID0) {
		t.Fatalf("Vtx should be accepted")
	}
	if !acceptedSet.Contains(vtxID1) {
		t.Fatalf("Vtx should be accepted")
	}
	if acceptedSet.Contains(vtxID2) {
		t.Fatalf("Vtx shouldn't be accepted")
	}
}
