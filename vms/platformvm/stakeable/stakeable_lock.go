// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package stakeable

import (
	"errors"

	"github.com/luxfi/node/vms/components/lux"
)

var (
	errInvalidLocktime      = errors.New("invalid locktime")
	errNestedStakeableLocks = errors.New("shouldn't nest stakeable locks")
)

type LockOut struct {
	Locktime             uint64 `serialize:"true" json:"locktime"`
	lux.TransferableOut `serialize:"true" json:"output"`
}

func (s *LockOut) Addresses() [][]byte {
	if addressable, ok := s.TransferableOut.(lux.Addressable); ok {
		return addressable.Addresses()
	}
	return nil
}

func (s *LockOut) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableOut.(*LockOut); nested {
		return errNestedStakeableLocks
	}
	return s.TransferableOut.Verify()
}

type LockIn struct {
	Locktime            uint64 `serialize:"true" json:"locktime"`
	lux.TransferableIn `serialize:"true" json:"input"`
}

func (s *LockIn) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableIn.(*LockIn); nested {
		return errNestedStakeableLocks
	}
	return s.TransferableIn.Verify()
}
