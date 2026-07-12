// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package security is the network security model for Lux L1/L2 networks
// (LP-018 §"Network security modes"). It is one small, orthogonal definition
// — pure values, no wire or state dependencies — composed by the three
// consumers that need it: the P-Chain tx wire, the executor, and state.
package security

import "errors"

// Admission governs how a network's own validator set admits members.
type Admission uint8

const (
	// NoOwnSet: the network runs no validator set of its own; its security is
	// entirely restaked from its parent.
	NoOwnSet Admission = iota
	// Open: permissionless. Any staker meeting Threshold joins the set.
	Open
	// Gated: membership is admitted by the Manager (PoA / permissioned).
	Gated
)

// Manager is the authority that governs a network's own validator set.
type Manager uint8

const (
	// PChain: the set is managed by P-Chain txs, authorised by the network Owner.
	PChain Manager = iota
	// Contract: the set is managed by an EVM staking contract on some chain.
	Contract
)

// Mode is a network's complete security configuration.
//
// The two axes are orthogonal:
//   - RestakeParent — the network leans on its parent's validator set.
//   - own set — whether it runs its OWN set, described by (Admission, Manager).
//
// Their product spans every operating mode: a restaked L2
// (RestakeParent, NoOwnSet), a sovereign L1 (¬RestakeParent, Open|Gated), and
// a hybrid L2 that restakes its parent AND runs an additive own set
// (RestakeParent, Open|Gated).
type Mode struct {
	RestakeParent bool
	Admission     Admission
	Threshold     uint64 // minimum stake for Open admission; 0 otherwise
	Manager       Manager
}

// Sovereign reports whether the network runs its own validator set.
func (m Mode) Sovereign() bool { return m.Admission != NoOwnSet }

var (
	ErrUnknownAdmission = errors.New("security: unknown admission")
	ErrUnknownManager   = errors.New("security: unknown manager")
	ErrNoSecurity       = errors.New("security: network must restake parent or run its own set")
)

// Valid checks the enum ranges and the cross-axis invariant. It deliberately
// does NOT reach into tx-level data (genesis validators, manager address) —
// those consistency checks belong to the tx that carries the Mode.
func (m Mode) Valid() error {
	switch {
	case m.Admission > Gated:
		return ErrUnknownAdmission
	case m.Manager > Contract:
		return ErrUnknownManager
	case !m.RestakeParent && m.Admission == NoOwnSet:
		return ErrNoSecurity
	}
	return nil
}
