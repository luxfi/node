// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleportvm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

var (
	ErrMessageNotSigned       = errors.New("bridge message not signed")
	ErrInvalidBridgeSignature = errors.New("invalid bridge signature")
	ErrDeliveryNotConfirmed   = errors.New("delivery not confirmed")
)

// BridgeMessage represents a signed cross-chain message
type BridgeMessage struct {
	ID        ids.ID    `json:"id"`
	Nonce     uint64    `json:"nonce"`
	Timestamp time.Time `json:"timestamp"`

	SourceChain string `json:"sourceChain"`
	DestChain   string `json:"destChain"`

	Asset     ids.ID `json:"asset"`
	Amount    uint64 `json:"amount"`
	Recipient []byte `json:"recipient"`
	Sender    []byte `json:"sender"`

	SourceTxID    ids.ID `json:"sourceTxId"`
	Confirmations uint32 `json:"confirmations"`

	Signature []byte `json:"signature"`
	SignedBy  []int  `json:"signedBy"`

	DeliveryConfirmation *DeliveryConfirmation `json:"deliveryConfirmation,omitempty"`
}

// DeliveryConfirmation proves message was delivered on destination chain
type DeliveryConfirmation struct {
	DestTxID         ids.ID    `json:"destTxId"`
	DestBlockHeight  uint64    `json:"destBlockHeight"`
	DestConfirms     uint32    `json:"destConfirms"`
	ConfirmedAt      time.Time `json:"confirmedAt"`
	ConfirmSignature []byte    `json:"confirmSignature"`
}

// SigningMessage returns the canonical bytes to sign
func (m *BridgeMessage) SigningMessage() []byte {
	h := sha256.New()
	h.Write(m.ID[:])
	h.Write([]byte(fmt.Sprintf("%d", m.Nonce)))
	h.Write([]byte(m.SourceChain))
	h.Write([]byte(m.DestChain))
	h.Write(m.Asset[:])
	h.Write([]byte(fmt.Sprintf("%d", m.Amount)))
	h.Write(m.Recipient)
	h.Write(m.Sender)
	h.Write(m.SourceTxID[:])
	return h.Sum(nil)
}

// Verify verifies the threshold signature
func (m *BridgeMessage) Verify(groupPublicKey []byte, verifier func([]byte, []byte) bool) error {
	if len(m.Signature) == 0 {
		return ErrMessageNotSigned
	}
	if !verifier(m.SigningMessage(), m.Signature) {
		return ErrInvalidBridgeSignature
	}
	return nil
}

// BridgeSigner handles signing of bridge messages using MPC
type BridgeSigner struct {
	mpcKeyManager  *MPCKeyManager
	mpcCoordinator *MPCCoordinator
	log            log.Logger
}

// NewBridgeSigner creates a new bridge signer
func NewBridgeSigner(keyManager *MPCKeyManager, coordinator *MPCCoordinator, logger log.Logger) *BridgeSigner {
	return &BridgeSigner{
		mpcKeyManager:  keyManager,
		mpcCoordinator: coordinator,
		log:            logger,
	}
}

// RequestSignature initiates threshold signing
func (s *BridgeSigner) RequestSignature(ctx context.Context, message *BridgeMessage, signerIndices []int) (string, error) {
	signingMsg := message.SigningMessage()
	sessionID := fmt.Sprintf("bridge-%s-%d", message.ID.String(), message.Nonce)

	_, err := s.mpcCoordinator.StartSigning(ctx, sessionID, signingMsg, signerIndices, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to start signing: %w", err)
	}
	return sessionID, nil
}

// CreateSignatureShare creates this node's signature share
func (s *BridgeSigner) CreateSignatureShare(ctx context.Context, message *BridgeMessage) ([]byte, []byte, error) {
	signingMsg := message.SigningMessage()

	share, err := s.mpcKeyManager.SignShare(ctx, signingMsg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create share: %w", err)
	}

	publicShare := s.mpcKeyManager.signer.PublicShare()
	return share.Bytes(), publicShare, nil
}

// GetSignature retrieves the completed signature
func (s *BridgeSigner) GetSignature(ctx context.Context, sessionID string) ([]byte, error) {
	session, exists := s.mpcCoordinator.GetSession(sessionID)
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	signature, err := session.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}
	return signature, nil
}

// SignBridgeMessage coordinates complete signing
func (s *BridgeSigner) SignBridgeMessage(ctx context.Context, message *BridgeMessage, activeSigners []int) error {
	sessionID, err := s.RequestSignature(ctx, message, activeSigners)
	if err != nil {
		return err
	}

	signature, err := s.GetSignature(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get signature: %w", err)
	}

	message.Signature = signature
	message.SignedBy = activeSigners

	s.log.Info("bridge message signed",
		log.String("messageID", message.ID.String()),
		log.String("signature", hex.EncodeToString(signature[:16])),
	)
	return nil
}

// VerifyBridgeMessage verifies a bridge message signature
func (s *BridgeSigner) VerifyBridgeMessage(message *BridgeMessage) error {
	groupKey := s.mpcKeyManager.GetGroupPublicKey()
	if len(groupKey) == 0 {
		return errors.New("group key not available")
	}

	verifier := func(msg, sig []byte) bool {
		return s.mpcKeyManager.VerifySignature(msg, sig)
	}
	return message.Verify(groupKey, verifier)
}

// DeliveryConfirmationSigner handles signing of delivery confirmations
type DeliveryConfirmationSigner struct {
	mpcKeyManager  *MPCKeyManager
	mpcCoordinator *MPCCoordinator
	log            log.Logger
}

// NewDeliveryConfirmationSigner creates a new delivery confirmation signer
func NewDeliveryConfirmationSigner(keyManager *MPCKeyManager, coordinator *MPCCoordinator, logger log.Logger) *DeliveryConfirmationSigner {
	return &DeliveryConfirmationSigner{
		mpcKeyManager:  keyManager,
		mpcCoordinator: coordinator,
		log:            logger,
	}
}

// SigningMessage returns the canonical bytes to sign for a delivery confirmation
func (dc *DeliveryConfirmation) SigningMessage(messageID ids.ID) []byte {
	h := sha256.New()
	h.Write(messageID[:])
	h.Write(dc.DestTxID[:])
	h.Write([]byte(fmt.Sprintf("%d", dc.DestBlockHeight)))
	h.Write([]byte(fmt.Sprintf("%d", dc.DestConfirms)))
	return h.Sum(nil)
}

// SignDeliveryConfirmation creates a threshold signature for delivery confirmation
func (s *DeliveryConfirmationSigner) SignDeliveryConfirmation(ctx context.Context, messageID ids.ID, confirmation *DeliveryConfirmation, activeSigners []int) error {
	signingMsg := confirmation.SigningMessage(messageID)
	sessionID := fmt.Sprintf("delivery-%s-%s", messageID.String(), confirmation.DestTxID.String())

	session, err := s.mpcCoordinator.StartSigning(ctx, sessionID, signingMsg, activeSigners, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to start signing: %w", err)
	}

	signature, err := session.Wait(ctx)
	if err != nil {
		return fmt.Errorf("signing failed: %w", err)
	}

	confirmation.ConfirmSignature = signature
	confirmation.ConfirmedAt = time.Now()
	return nil
}

// VerifyDeliveryConfirmation verifies a delivery confirmation signature
func (s *DeliveryConfirmationSigner) VerifyDeliveryConfirmation(messageID ids.ID, confirmation *DeliveryConfirmation) error {
	if len(confirmation.ConfirmSignature) == 0 {
		return ErrDeliveryNotConfirmed
	}

	signingMsg := confirmation.SigningMessage(messageID)
	if !s.mpcKeyManager.VerifySignature(signingMsg, confirmation.ConfirmSignature) {
		return ErrInvalidBridgeSignature
	}
	return nil
}

// BridgeMessageValidator validates bridge messages
type BridgeMessageValidator struct {
	bridgeSigner           *BridgeSigner
	deliverySigner         *DeliveryConfirmationSigner
	minConfirmations       uint32
	requireDeliveryConfirm bool
	log                    log.Logger
}

// NewBridgeMessageValidator creates a new validator
func NewBridgeMessageValidator(
	bridgeSigner *BridgeSigner,
	deliverySigner *DeliveryConfirmationSigner,
	minConfirmations uint32,
	requireDeliveryConfirm bool,
	logger log.Logger,
) *BridgeMessageValidator {
	return &BridgeMessageValidator{
		bridgeSigner:           bridgeSigner,
		deliverySigner:         deliverySigner,
		minConfirmations:       minConfirmations,
		requireDeliveryConfirm: requireDeliveryConfirm,
		log:                    logger,
	}
}

// ValidateMessage performs full validation
func (v *BridgeMessageValidator) ValidateMessage(message *BridgeMessage) error {
	if message.Confirmations < v.minConfirmations {
		return fmt.Errorf("insufficient confirmations: %d < %d", message.Confirmations, v.minConfirmations)
	}

	if err := v.bridgeSigner.VerifyBridgeMessage(message); err != nil {
		return fmt.Errorf("invalid message signature: %w", err)
	}

	if v.requireDeliveryConfirm {
		if message.DeliveryConfirmation == nil {
			return ErrDeliveryNotConfirmed
		}
		if err := v.deliverySigner.VerifyDeliveryConfirmation(message.ID, message.DeliveryConfirmation); err != nil {
			return fmt.Errorf("invalid delivery confirmation: %w", err)
		}
		if message.DeliveryConfirmation.DestConfirms < v.minConfirmations {
			return fmt.Errorf("insufficient delivery confirmations: %d < %d",
				message.DeliveryConfirmation.DestConfirms, v.minConfirmations)
		}
	}
	return nil
}

// ValidateBeforeRelay validates a message before relaying
func (v *BridgeMessageValidator) ValidateBeforeRelay(message *BridgeMessage) error {
	if message.Confirmations < v.minConfirmations {
		return fmt.Errorf("insufficient confirmations: %d < %d", message.Confirmations, v.minConfirmations)
	}
	return v.bridgeSigner.VerifyBridgeMessage(message)
}

// ValidateAfterRelay validates delivery confirmation after relay
func (v *BridgeMessageValidator) ValidateAfterRelay(message *BridgeMessage) error {
	if message.DeliveryConfirmation == nil {
		return ErrDeliveryNotConfirmed
	}
	return v.deliverySigner.VerifyDeliveryConfirmation(message.ID, message.DeliveryConfirmation)
}
