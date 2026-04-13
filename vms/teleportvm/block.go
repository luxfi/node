// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleportvm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/threshold/pkg/ecdsa"
	"github.com/luxfi/threshold/pkg/math/curve"
	"github.com/luxfi/threshold/pkg/party"
)

var (
	errInvalidBlock = errors.New("invalid block")
	errFutureBlock  = errors.New("block timestamp is in the future")

	maxClockSkew = int64(60)
)

// Block represents a unified block in the Teleport chain.
// It can contain bridge requests, relay messages, and oracle observations.
type Block struct {
	ParentID_      ids.ID           `json:"parentId"`
	BlockHeight    uint64           `json:"height"`
	BlockTimestamp int64            `json:"timestamp"`

	// Bridge data
	BridgeRequests []*BridgeRequest       `json:"bridgeRequests,omitempty"`
	MPCSignatures  map[ids.NodeID][]byte  `json:"mpcSignatures,omitempty"`

	// Relay data
	RelayMessages  []*Message             `json:"relayMessages,omitempty"`
	Receipts       []*MessageReceipt      `json:"receipts,omitempty"`
	StateRoot      []byte                 `json:"stateRoot,omitempty"`

	// Oracle data
	Observations   []*Observation         `json:"observations,omitempty"`
	Aggregations   []*AggregatedValue     `json:"aggregations,omitempty"`
	FeedUpdates    []*Feed                `json:"feedUpdates,omitempty"`

	// Cached values
	ID_    ids.ID
	bytes  []byte
	status choices.Status
	vm     *VM
}

// ID returns the block ID
func (b *Block) ID() ids.ID {
	if b.ID_ == ids.Empty {
		b.ID_ = b.computeID()
	}
	return b.ID_
}

// computeID calculates the block ID
func (b *Block) computeID() ids.ID {
	h := sha256.New()

	h.Write(b.ParentID_[:])

	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, b.BlockHeight)
	h.Write(heightBytes)

	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, uint64(b.BlockTimestamp))
	h.Write(timestampBytes)

	// Bridge requests
	for _, req := range b.BridgeRequests {
		h.Write(req.ID[:])
	}

	// Relay messages
	for _, msg := range b.RelayMessages {
		h.Write(msg.ID[:])
	}

	// Receipts
	for _, receipt := range b.Receipts {
		h.Write(receipt.MessageID[:])
		h.Write(receipt.ResultHash)
	}

	// Observations
	for _, obs := range b.Observations {
		h.Write(obs.FeedID[:])
		h.Write(obs.Value)
	}

	h.Write(b.StateRoot)

	return ids.ID(h.Sum(nil))
}

// ParentID returns the parent block ID
func (b *Block) ParentID() ids.ID {
	return b.ParentID_
}

// Parent returns the parent block ID
func (b *Block) Parent() ids.ID {
	return b.ParentID_
}

// Height returns the block height
func (b *Block) Height() uint64 {
	return b.BlockHeight
}

// Timestamp returns the block timestamp
func (b *Block) Timestamp() time.Time {
	return time.Unix(b.BlockTimestamp, 0)
}

// Status returns the block's status
func (b *Block) Status() uint8 {
	return uint8(b.status)
}

// Verify verifies the block
func (b *Block) Verify(ctx context.Context) error {
	// Basic validation
	if b.BlockHeight == 0 && b.ParentID_ != ids.Empty {
		return errInvalidBlock
	}

	// Verify timestamp
	if b.BlockTimestamp > time.Now().Unix()+maxClockSkew {
		return errFutureBlock
	}

	// Verify bridge requests
	for _, req := range b.BridgeRequests {
		if req.Confirmations < b.vm.config.MinConfirmations {
			return fmt.Errorf("insufficient confirmations for request %s: %d < %d",
				req.ID, req.Confirmations, b.vm.config.MinConfirmations)
		}
		if req.Amount > b.vm.config.MaxBridgeAmount {
			return fmt.Errorf("bridge amount exceeds maximum: %d > %d",
				req.Amount, b.vm.config.MaxBridgeAmount)
		}

		b.vm.bridgeRegistry.mu.RLock()
		dailyVolume := b.vm.bridgeRegistry.DailyVolume[req.DestChain]
		b.vm.bridgeRegistry.mu.RUnlock()

		if dailyVolume+req.Amount > b.vm.config.DailyBridgeLimit {
			return fmt.Errorf("would exceed daily bridge limit for chain %s", req.DestChain)
		}

		if len(req.MPCSignatures) > 0 {
			if err := b.verifyRequestMPCSignatures(req); err != nil {
				return fmt.Errorf("MPC signature verification failed for request %s: %w", req.ID, err)
			}
		}
	}

	// Verify relay messages
	for _, msg := range b.RelayMessages {
		_, err := b.vm.GetChannel(msg.ChannelID)
		if err != nil {
			return err
		}
		if len(msg.Payload) > b.vm.config.MaxMessageSize {
			return errMessageTooLarge
		}
		if msg.Timeout > 0 && time.Now().Unix() > msg.Timeout {
			return errors.New("message timeout exceeded")
		}
	}

	// Verify MPC block signatures
	if len(b.MPCSignatures) > 0 {
		validSignatures := 0
		blockHash := b.ID()

		for nodeID, sigBytes := range b.MPCSignatures {
			if b.vm.mpcConfig == nil {
				continue
			}

			partyID := party.ID(nodeID.String())
			pubInfo, exists := b.vm.mpcConfig.Public[partyID]
			if !exists {
				continue
			}

			sig, err := deserializeSignature(b.vm.mpcConfig.Group, sigBytes)
			if err != nil {
				continue
			}

			if sig.Verify(pubInfo.ECDSA, blockHash[:]) {
				validSignatures++
			}
		}

		if validSignatures < 1 {
			return fmt.Errorf("no valid MPC signature found")
		}
	}

	return nil
}

// Accept marks the block as accepted
func (b *Block) Accept(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()

	// Process bridge requests
	for _, req := range b.BridgeRequests {
		b.vm.bridgeRegistry.mu.Lock()

		completed := &CompletedBridge{
			RequestID:   req.ID,
			SourceTxID:  req.SourceTxID,
			CompletedAt: time.Now(),
		}
		if len(req.MPCSignatures) > 0 {
			completed.MPCSignature = req.MPCSignatures[0]
		}

		b.vm.bridgeRegistry.CompletedBridges[req.ID] = completed
		volume := b.vm.bridgeRegistry.DailyVolume[req.DestChain]
		b.vm.bridgeRegistry.DailyVolume[req.DestChain] = volume + req.Amount
		b.vm.bridgeRegistry.mu.Unlock()

		delete(b.vm.pendingBridges, req.ID)

		b.vm.log.Info("completed bridge request",
			log.Stringer("requestID", req.ID),
			log.String("destChain", req.DestChain),
			log.Uint64("amount", req.Amount),
		)
	}

	// Process relay messages
	for _, msg := range b.RelayMessages {
		msg.State = MessageDelivered
		msg.ConfirmedAt = b.BlockTimestamp

		destMsgs := b.vm.pendingMsgs[msg.DestChain]
		for i, m := range destMsgs {
			if m.ID == msg.ID {
				b.vm.pendingMsgs[msg.DestChain] = append(destMsgs[:i], destMsgs[i+1:]...)
				break
			}
		}

		msgBytes, _ := json.Marshal(msg)
		msgKey := append(messagePrefix, msg.ID[:]...)
		b.vm.db.Put(msgKey, msgBytes)
	}

	// Update state
	b.status = choices.Accepted
	b.vm.lastAcceptedID = b.ID()
	b.vm.lastAccepted = b
	delete(b.vm.pendingBlocks, b.ID())

	// Save last accepted
	id := b.ID()
	if err := b.vm.db.Put(lastAcceptedKey, id[:]); err != nil {
		return err
	}

	// Persist block
	return b.vm.putBlock(b)
}

// Reject marks the block as rejected
func (b *Block) Reject(ctx context.Context) error {
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()

	b.status = choices.Rejected
	delete(b.vm.pendingBlocks, b.ID())
	return nil
}

// Bytes returns the block bytes
func (b *Block) Bytes() []byte {
	if b.bytes != nil {
		return b.bytes
	}

	bytes, err := Codec.Marshal(codecVersion, b)
	if err != nil {
		// Fallback to JSON
		bytes, err = json.Marshal(b)
		if err != nil {
			return nil
		}
	}

	b.bytes = bytes
	return bytes
}

// =========================================================================
// MPC signature verification (from bridgevm)
// =========================================================================

func deserializeSignature(group curve.Curve, data []byte) (*ecdsa.Signature, error) {
	if len(data) < 64 {
		return nil, errors.New("signature too short")
	}

	sig := ecdsa.EmptySignature(group)
	rLen := len(data) / 2
	if err := sig.R.UnmarshalBinary(data[:rLen]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal R: %w", err)
	}
	if err := sig.S.UnmarshalBinary(data[rLen:]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal S: %w", err)
	}
	return &sig, nil
}

func (b *Block) verifyRequestMPCSignatures(req *BridgeRequest) error {
	if b.vm.mpcConfig == nil {
		return errors.New("MPC config not initialized")
	}

	groupPublicKey := b.vm.mpcConfig.PublicPoint()
	if groupPublicKey == nil {
		return errors.New("failed to compute group public key")
	}

	messageHash := computeRequestHash(req)

	if len(req.MPCSignatures) == 0 {
		return errors.New("no MPC signatures present")
	}

	aggregatedSigBytes := req.MPCSignatures[0]
	sig, err := deserializeSignature(b.vm.mpcConfig.Group, aggregatedSigBytes)
	if err != nil {
		return fmt.Errorf("failed to deserialize aggregated signature: %w", err)
	}

	if !sig.Verify(groupPublicKey, messageHash) {
		return errors.New("aggregated MPC signature verification failed")
	}
	return nil
}

func computeRequestHash(req *BridgeRequest) []byte {
	h := sha256.New()
	h.Write(req.ID[:])
	h.Write([]byte(req.SourceChain))
	h.Write([]byte(req.DestChain))
	h.Write(req.Asset[:])

	amountBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(amountBytes, req.Amount)
	h.Write(amountBytes)

	h.Write(req.Recipient)
	h.Write(req.SourceTxID[:])
	return h.Sum(nil)
}
