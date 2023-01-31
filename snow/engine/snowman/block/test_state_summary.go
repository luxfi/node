// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ StateSummary = (*TestStateSummary)(nil)

	errAccept = errors.New("unexpectedly called Accept")
)

type TestStateSummary struct {
	IDV     ids.ID
	HeightV uint64
	BytesV  []byte

	T          *testing.T
	CantAccept bool
<<<<<<< HEAD
<<<<<<< HEAD
	AcceptF    func(context.Context) (StateSyncMode, error)
=======
	AcceptF    func(context.Context) (bool, error)
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
	AcceptF    func(context.Context) (StateSyncMode, error)
>>>>>>> f1ee6f5ba (Add dynamic state sync support (#2362))
}

func (s *TestStateSummary) ID() ids.ID {
	return s.IDV
}
<<<<<<< HEAD
=======

func (s *TestStateSummary) Height() uint64 {
	return s.HeightV
}

func (s *TestStateSummary) Bytes() []byte {
	return s.BytesV
}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))

func (s *TestStateSummary) Height() uint64 {
	return s.HeightV
}

func (s *TestStateSummary) Bytes() []byte {
	return s.BytesV
}

<<<<<<< HEAD
<<<<<<< HEAD
func (s *TestStateSummary) Accept(ctx context.Context) (StateSyncMode, error) {
=======
func (s *TestStateSummary) Accept(ctx context.Context) (bool, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
func (s *TestStateSummary) Accept(ctx context.Context) (StateSyncMode, error) {
>>>>>>> f1ee6f5ba (Add dynamic state sync support (#2362))
	if s.AcceptF != nil {
		return s.AcceptF(ctx)
	}
	if s.CantAccept && s.T != nil {
		s.T.Fatal(errAccept)
	}
	return StateSyncSkipped, errAccept
}
