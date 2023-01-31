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

package vertex

import (
	"testing"

<<<<<<< HEAD
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/lux"
=======
<<<<<<< HEAD:snow/engine/avalanche/vertex/heap_test.go
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/avalanche"
=======
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/lux"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/vertex/heap_test.go
>>>>>>> 53a8245a8 (Update consensus)
)

// This example inserts several ints into an IntHeap, checks the minimum,
// and removes them in order of priority.
func TestUniqueVertexHeapReturnsOrdered(t *testing.T) {
	h := NewHeap()

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}
	vtx2 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}
	vtx3 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 3,
	}
	vtx4 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Unknown,
		},
		HeightV: 0,
	}

	vts := []lux.Vertex{vtx0, vtx1, vtx2, vtx3, vtx4}

	for _, vtx := range vts {
		h.Push(vtx)
	}

	vtxZ := h.Pop()
	if vtxZ.ID() != vtx4.ID() {
		t.Fatalf("Heap did not pop unknown element first")
	}

	vtxA := h.Pop()
	if height, err := vtxA.Height(); err != nil || height != 3 {
		t.Fatalf("First height from heap was incorrect")
	} else if vtxA.ID() != vtx3.ID() {
		t.Fatalf("Incorrect ID on vertex popped from heap")
	}

	vtxB := h.Pop()
	if height, err := vtxB.Height(); err != nil || height != 1 {
		t.Fatalf("First height from heap was incorrect")
	} else if vtxB.ID() != vtx1.ID() && vtxB.ID() != vtx2.ID() {
		t.Fatalf("Incorrect ID on vertex popped from heap")
	}

	vtxC := h.Pop()
	if height, err := vtxC.Height(); err != nil || height != 1 {
		t.Fatalf("First height from heap was incorrect")
	} else if vtxC.ID() != vtx1.ID() && vtxC.ID() != vtx2.ID() {
		t.Fatalf("Incorrect ID on vertex popped from heap")
	}

	if vtxB.ID() == vtxC.ID() {
		t.Fatalf("Heap returned same element more than once")
	}

	vtxD := h.Pop()
	if height, err := vtxD.Height(); err != nil || height != 0 {
		t.Fatalf("Last height returned was incorrect")
	} else if vtxD.ID() != vtx0.ID() {
		t.Fatalf("Last item from heap had incorrect ID")
	}

	if h.Len() != 0 {
		t.Fatalf("Heap was not empty after popping all of its elements")
	}
}

func TestUniqueVertexHeapRemainsUnique(t *testing.T) {
	h := NewHeap()

	vtx0 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 0,
	}
	vtx1 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}

	sharedID := ids.GenerateTestID()
	vtx2 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     sharedID,
			StatusV: choices.Processing,
		},
		HeightV: 1,
	}
	vtx3 := &lux.TestVertex{
		TestDecidable: choices.TestDecidable{
			IDV:     sharedID,
			StatusV: choices.Processing,
		},
		HeightV: 2,
	}

	pushed1 := h.Push(vtx0)
	pushed2 := h.Push(vtx1)
	pushed3 := h.Push(vtx2)
	pushed4 := h.Push(vtx3)
	switch {
	case h.Len() != 3:
		t.Fatalf("Unique Vertex Heap has incorrect length: %d", h.Len())
	case !(pushed1 && pushed2 && pushed3):
		t.Fatalf("Failed to push a new unique element")
	case pushed4:
		t.Fatalf("Pushed non-unique element to the unique vertex heap")
	}
}
