<<<<<<< HEAD
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
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 8fb2bec88 (Must keep bloodline pure)
// See the file LICENSE for licensing terms.

package lux

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
=======
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/set"
>>>>>>> 53a8245a8 (Update consensus)
)

var _ Vertex = (*TestVertex)(nil)

// TestVertex is a useful test vertex
type TestVertex struct {
	choices.TestDecidable

	VerifyErrV    error
	ParentsV      []Vertex
	ParentsErrV   error
	HasWhitelistV bool
<<<<<<< HEAD
	WhitelistV    ids.Set
=======
	WhitelistV    set.Set[ids.ID]
>>>>>>> 53a8245a8 (Update consensus)
	WhitelistErrV error
	HeightV       uint64
	HeightErrV    error
	TxsV          []snowstorm.Tx
	TxsErrV       error
	BytesV        []byte
}

<<<<<<< HEAD
func (v *TestVertex) Verify() error                { return v.VerifyErrV }
func (v *TestVertex) Parents() ([]Vertex, error)   { return v.ParentsV, v.ParentsErrV }
func (v *TestVertex) HasWhitelist() bool           { return v.HasWhitelistV }
func (v *TestVertex) Whitelist() (ids.Set, error)  { return v.WhitelistV, v.WhitelistErrV }
func (v *TestVertex) Height() (uint64, error)      { return v.HeightV, v.HeightErrV }
func (v *TestVertex) Txs() ([]snowstorm.Tx, error) { return v.TxsV, v.TxsErrV }
func (v *TestVertex) Bytes() []byte                { return v.BytesV }
=======
<<<<<<< HEAD
<<<<<<< HEAD
func (v *TestVertex) Verify(context.Context) error {
=======
func (v *TestVertex) Verify() error {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (v *TestVertex) Verify(context.Context) error {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
	return v.VerifyErrV
}

func (v *TestVertex) Parents() ([]Vertex, error) {
	return v.ParentsV, v.ParentsErrV
}

func (v *TestVertex) HasWhitelist() bool {
	return v.HasWhitelistV
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
func (v *TestVertex) Whitelist(context.Context) (set.Set[ids.ID], error) {
=======
func (v *TestVertex) Whitelist() (ids.Set, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (v *TestVertex) Whitelist(context.Context) (ids.Set, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
func (v *TestVertex) Whitelist(context.Context) (set.Set[ids.ID], error) {
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
	return v.WhitelistV, v.WhitelistErrV
}

func (v *TestVertex) Height() (uint64, error) {
	return v.HeightV, v.HeightErrV
}

<<<<<<< HEAD
<<<<<<< HEAD
func (v *TestVertex) Txs(context.Context) ([]snowstorm.Tx, error) {
=======
func (v *TestVertex) Txs() ([]snowstorm.Tx, error) {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (v *TestVertex) Txs(context.Context) ([]snowstorm.Tx, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
	return v.TxsV, v.TxsErrV
}

func (v *TestVertex) Bytes() []byte {
	return v.BytesV
}
>>>>>>> 53a8245a8 (Update consensus)
