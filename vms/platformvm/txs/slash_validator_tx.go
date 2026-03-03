// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/runtime"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/lux"
)

var (
	_ UnsignedTx = (*SlashValidatorTx)(nil)

	errNoEvidence           = errors.New("no equivocation evidence provided")
	errInvalidEvidenceType  = errors.New("invalid evidence type")
	errSameContent          = errors.New("evidence messages are identical")
	errHeightMismatch       = errors.New("evidence heights do not match")
	errEmptySignature       = errors.New("evidence contains empty signature")
	errEmptyMessage         = errors.New("evidence contains empty message")
	errSlashPercentTooLarge = errors.New("slash percentage exceeds 100%")
	errNilRuntime           = errors.New("runtime is nil")
)

// EvidenceType classifies the equivocation.
type EvidenceType uint8

const (
	DoubleVoteEvidence EvidenceType = iota + 1
	DoubleSignEvidence
)

// SlashEvidence contains two conflicting signed messages from the same
// validator at the same consensus height. Both signatures must verify
// against the validator's registered BLS public key.
type SlashEvidence struct {
	// Height at which the equivocation occurred
	Height uint64 `serialize:"true" json:"height"`
	// Type of equivocation
	Type EvidenceType `serialize:"true" json:"type"`
	// First signed message (vote or block hash)
	MessageA []byte `serialize:"true" json:"messageA"`
	// BLS signature over MessageA
	SignatureA []byte `serialize:"true" json:"signatureA"`
	// Second conflicting signed message at the same height
	MessageB []byte `serialize:"true" json:"messageB"`
	// BLS signature over MessageB
	SignatureB []byte `serialize:"true" json:"signatureB"`
}

// SlashValidatorTx removes a percentage of a validator's stake as
// punishment for provable equivocation (double-vote or double-sign).
// The slashed amount is burned. Anyone can submit this transaction
// with valid evidence.
type SlashValidatorTx struct {
	BaseTx `serialize:"true"`
	// NodeID of the validator to slash
	NodeID ids.NodeID `serialize:"true" json:"nodeID"`
	// Cryptographic proof of equivocation
	Evidence SlashEvidence `serialize:"true" json:"evidence"`
	// Percentage of stake to slash, in units of reward.PercentDenominator
	// (e.g., 100_000 = 10%). Set by the submitter but capped/verified by
	// the executor against chain parameters.
	SlashPercentage uint32 `serialize:"true" json:"slashPercentage"`
}

func (tx *SlashValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		return nil
	case tx.NodeID == ids.EmptyNodeID:
		return errEmptyNodeID
	case tx.SlashPercentage == 0:
		return errNoEvidence
	case tx.SlashPercentage > 1_000_000: // reward.PercentDenominator
		return errSlashPercentTooLarge
	}

	if err := tx.Evidence.Verify(); err != nil {
		return fmt.Errorf("invalid slash evidence: %w", err)
	}

	if rt == nil {
		return errNilRuntime
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	tx.SyntacticallyVerified = true
	return nil
}

// Verify checks that the evidence is structurally valid. It does NOT
// verify BLS signatures (that requires the validator's public key from
// state and is done in the executor).
func (e *SlashEvidence) Verify() error {
	switch {
	case e.Type != DoubleVoteEvidence && e.Type != DoubleSignEvidence:
		return errInvalidEvidenceType
	case len(e.MessageA) == 0 || len(e.MessageB) == 0:
		return errEmptyMessage
	case len(e.SignatureA) == 0 || len(e.SignatureB) == 0:
		return errEmptySignature
	case len(e.SignatureA) != bls.SignatureLen || len(e.SignatureB) != bls.SignatureLen:
		return errEmptySignature
	}

	// Messages must differ -- same content is not equivocation
	if len(e.MessageA) == len(e.MessageB) {
		same := true
		for i := range e.MessageA {
			if e.MessageA[i] != e.MessageB[i] {
				same = false
				break
			}
		}
		if same {
			return errSameContent
		}
	}
	return nil
}

func (tx *SlashValidatorTx) Visit(visitor Visitor) error {
	return visitor.SlashValidatorTx(tx)
}

func (*SlashValidatorTx) InputIDs() set.Set[ids.ID] {
	return nil
}

func (*SlashValidatorTx) Outputs() []*lux.TransferableOutput {
	return nil
}

func (tx *SlashValidatorTx) InitRuntime(rt *runtime.Runtime) {
	tx.BaseTx.InitRuntime(rt)
}

func (tx *SlashValidatorTx) Initialize(ctx context.Context) error {
	return nil
}
