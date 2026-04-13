// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"context"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/luxfi/ids"
)

// Service provides RPC endpoints for the ServiceNodeVM
type Service struct {
	vm *VM
}

// ========== Registration Endpoints ==========

// RegisterArgs are the arguments to Register
type RegisterArgs struct {
	NodeID       string `json:"nodeID"`
	PublicKey    string `json:"publicKey"`    // Hex-encoded
	EndpointHash string `json:"endpointHash"` // Hex-encoded
	StakeAmount  uint64 `json:"stakeAmount"`
	StakeLockEnd uint64 `json:"stakeLockEnd"` // Unix timestamp
	Signature    string `json:"signature"`    // Hex-encoded
}

// RegisterReply is the reply from Register
type RegisterReply struct {
	NodeID    string `json:"nodeID"`
	State     string `json:"state"`
	Timestamp int64  `json:"timestamp"`
}

// Register registers a new service node
func (s *Service) Register(r *http.Request, args *RegisterArgs, reply *RegisterReply) error {
	nodeID, err := ids.NodeIDFromString(args.NodeID)
	if err != nil {
		return err
	}

	publicKey, err := hex.DecodeString(args.PublicKey)
	if err != nil {
		return err
	}

	endpointHashBytes, err := hex.DecodeString(args.EndpointHash)
	if err != nil {
		return err
	}
	var endpointHash [32]byte
	copy(endpointHash[:], endpointHashBytes)

	signature, err := hex.DecodeString(args.Signature)
	if err != nil {
		return err
	}

	tx := &RegistrationTx{
		NodeID:       nodeID,
		PublicKey:    publicKey,
		EndpointHash: endpointHash,
		StakeAmount:  args.StakeAmount,
		StakeLockEnd: args.StakeLockEnd,
		Signature:    signature,
	}

	node, err := s.vm.RegisterServiceNode(r.Context(), tx)
	if err != nil {
		return err
	}

	reply.NodeID = node.NodeID.String()
	reply.State = node.State
	reply.Timestamp = node.RegisteredAt.Unix()
	return nil
}

// ActivateArgs are the arguments to Activate
type ActivateArgs struct {
	NodeID string `json:"nodeID"`
}

// ActivateReply is the reply from Activate
type ActivateReply struct {
	Success bool `json:"success"`
}

// Activate activates a registered service node
func (s *Service) Activate(r *http.Request, args *ActivateArgs, reply *ActivateReply) error {
	nodeID, err := ids.NodeIDFromString(args.NodeID)
	if err != nil {
		return err
	}

	if err := s.vm.ActivateServiceNode(r.Context(), nodeID); err != nil {
		return err
	}

	reply.Success = true
	return nil
}

// DeactivateArgs are the arguments to Deactivate
type DeactivateArgs struct {
	NodeID string `json:"nodeID"`
}

// DeactivateReply is the reply from Deactivate
type DeactivateReply struct {
	Success bool `json:"success"`
}

// Deactivate starts the exit process for a service node
func (s *Service) Deactivate(r *http.Request, args *DeactivateArgs, reply *DeactivateReply) error {
	nodeID, err := ids.NodeIDFromString(args.NodeID)
	if err != nil {
		return err
	}

	if err := s.vm.DeactivateServiceNode(r.Context(), nodeID); err != nil {
		return err
	}

	reply.Success = true
	return nil
}

// ========== Query Endpoints ==========

// GetNodeArgs are the arguments to GetNode
type GetNodeArgs struct {
	NodeID string `json:"nodeID"`
}

// GetNodeReply is the reply from GetNode
type GetNodeReply struct {
	NodeID           string `json:"nodeID"`
	PublicKey        string `json:"publicKey"`
	State            string `json:"state"`
	StakeAmount      uint64 `json:"stakeAmount"`
	StakeLockEnd     uint64 `json:"stakeLockEnd"`
	UptimeScore      uint64 `json:"uptimeScore"`
	ChallengesPassed uint64 `json:"challengesPassed"`
	ChallengesFailed uint64 `json:"challengesFailed"`
	PendingRewards   uint64 `json:"pendingRewards"`
	TotalRewards     uint64 `json:"totalRewards"`
	RegisteredAt     int64  `json:"registeredAt"`
	LastActiveAt     int64  `json:"lastActiveAt"`
}

// GetNode retrieves a service node by ID
func (s *Service) GetNode(r *http.Request, args *GetNodeArgs, reply *GetNodeReply) error {
	nodeID, err := ids.NodeIDFromString(args.NodeID)
	if err != nil {
		return err
	}

	node, err := s.vm.GetServiceNode(nodeID)
	if err != nil {
		return err
	}

	reply.NodeID = node.NodeID.String()
	reply.PublicKey = hex.EncodeToString(node.PublicKey)
	reply.State = node.State
	reply.StakeAmount = node.StakeAmount
	reply.StakeLockEnd = node.StakeLockEnd
	reply.UptimeScore = node.UptimeScore
	reply.ChallengesPassed = node.ChallengesPassed
	reply.ChallengesFailed = node.ChallengesFailed
	reply.PendingRewards = node.PendingRewards
	reply.TotalRewards = node.TotalRewards
	reply.RegisteredAt = node.RegisteredAt.Unix()
	reply.LastActiveAt = node.LastActiveAt.Unix()
	return nil
}

// GetActiveNodesArgs are the arguments to GetActiveNodes
type GetActiveNodesArgs struct{}

// GetActiveNodesReply is the reply from GetActiveNodes
type GetActiveNodesReply struct {
	Nodes []string `json:"nodes"`
	Count int      `json:"count"`
}

// GetActiveNodes returns all active service node IDs
func (s *Service) GetActiveNodes(r *http.Request, args *GetActiveNodesArgs, reply *GetActiveNodesReply) error {
	nodes := s.vm.GetActiveServiceNodes()

	reply.Nodes = make([]string, len(nodes))
	for i, node := range nodes {
		reply.Nodes[i] = node.NodeID.String()
	}
	reply.Count = len(nodes)
	return nil
}

// ========== Swarm Endpoints ==========

// GetSwarmForAccountArgs are the arguments to GetSwarmForAccount
type GetSwarmForAccountArgs struct {
	AccountID string `json:"accountID"` // Hex-encoded account ID hash
}

// GetSwarmForAccountReply is the reply from GetSwarmForAccount
type GetSwarmForAccountReply struct {
	SwarmID uint64   `json:"swarmID"`
	Nodes   []string `json:"nodes"`
	EpochID uint64   `json:"epochID"`
}

// GetSwarmForAccount returns the swarm assignment for an account
func (s *Service) GetSwarmForAccount(r *http.Request, args *GetSwarmForAccountArgs, reply *GetSwarmForAccountReply) error {
	// Decode account ID
	accountIDBytes, err := hex.DecodeString(args.AccountID)
	if err != nil {
		return err
	}
	var accountID [32]byte
	copy(accountID[:], accountIDBytes)

	// Get active nodes
	activeNodes := s.vm.registry.GetActiveNodeIDs()

	// Simple modular assignment (full implementation uses epoch coordinator)
	if len(activeNodes) == 0 {
		return ErrSwarmNotFound
	}

	nodesPerSwarm := int(s.vm.config.NodesPerSwarm)
	swarmCount := len(activeNodes) / nodesPerSwarm
	if swarmCount == 0 {
		swarmCount = 1
	}

	// Deterministic swarm selection based on account ID
	swarmID := uint64(accountID[0]) % uint64(swarmCount)

	// Get nodes for this swarm
	start := int(swarmID) * nodesPerSwarm
	end := start + nodesPerSwarm
	if end > len(activeNodes) {
		end = len(activeNodes)
	}

	reply.SwarmID = swarmID
	reply.Nodes = make([]string, 0, end-start)
	for i := start; i < end; i++ {
		reply.Nodes = append(reply.Nodes, activeNodes[i].String())
	}
	reply.EpochID = 0 // Would be set by epoch coordinator
	return nil
}

// ========== Uptime Proof Endpoints ==========

// SubmitUptimeProofArgs are the arguments to SubmitUptimeProof
type SubmitUptimeProofArgs struct {
	NodeID      string `json:"nodeID"`
	EpochID     uint64 `json:"epochID"`
	BlockHeight uint64 `json:"blockHeight"`
	Signature   string `json:"signature"` // Hex-encoded
}

// SubmitUptimeProofReply is the reply from SubmitUptimeProof
type SubmitUptimeProofReply struct {
	Success bool `json:"success"`
}

// SubmitUptimeProof submits an uptime proof
func (s *Service) SubmitUptimeProof(r *http.Request, args *SubmitUptimeProofArgs, reply *SubmitUptimeProofReply) error {
	nodeID, err := ids.NodeIDFromString(args.NodeID)
	if err != nil {
		return err
	}

	signature, err := hex.DecodeString(args.Signature)
	if err != nil {
		return err
	}

	proof := &UptimeProof{
		NodeID:      nodeID,
		EpochID:     args.EpochID,
		BlockHeight: args.BlockHeight,
		Timestamp:   time.Now(),
		Signature:   signature,
	}

	if err := s.vm.SubmitUptimeProof(r.Context(), proof); err != nil {
		return err
	}

	reply.Success = true
	return nil
}

// ========== Storage Commitment Endpoints ==========

// SubmitStorageCommitmentArgs are the arguments to SubmitStorageCommitment
type SubmitStorageCommitmentArgs struct {
	NodeID       string `json:"nodeID"`
	EpochID      uint64 `json:"epochID"`
	StoreRoot    string `json:"storeRoot"`    // Hex-encoded
	MessageCount uint64 `json:"messageCount"`
	TotalSize    uint64 `json:"totalSize"`
	Signature    string `json:"signature"` // Hex-encoded
}

// SubmitStorageCommitmentReply is the reply from SubmitStorageCommitment
type SubmitStorageCommitmentReply struct {
	Success bool `json:"success"`
}

// SubmitStorageCommitment submits a storage commitment
func (s *Service) SubmitStorageCommitment(r *http.Request, args *SubmitStorageCommitmentArgs, reply *SubmitStorageCommitmentReply) error {
	nodeID, err := ids.NodeIDFromString(args.NodeID)
	if err != nil {
		return err
	}

	storeRootBytes, err := hex.DecodeString(args.StoreRoot)
	if err != nil {
		return err
	}
	var storeRoot [32]byte
	copy(storeRoot[:], storeRootBytes)

	signature, err := hex.DecodeString(args.Signature)
	if err != nil {
		return err
	}

	commit := &StorageCommitment{
		NodeID:       nodeID,
		EpochID:      args.EpochID,
		StoreRoot:    storeRoot,
		MessageCount: args.MessageCount,
		TotalSize:    args.TotalSize,
		Timestamp:    time.Now(),
		Signature:    signature,
	}

	if err := s.vm.SubmitStorageCommitment(r.Context(), commit); err != nil {
		return err
	}

	reply.Success = true
	return nil
}

// ========== Network Parameters ==========

// GetNetworkParamsArgs are the arguments to GetNetworkParams
type GetNetworkParamsArgs struct{}

// GetNetworkParamsReply is the reply from GetNetworkParams
type GetNetworkParamsReply struct {
	MinStake         uint64 `json:"minStake"`
	StakeLockPeriod  int64  `json:"stakeLockPeriod"`
	EpochDuration    int64  `json:"epochDuration"`
	NodesPerSwarm    uint32 `json:"nodesPerSwarm"`
	MinActiveNodes   uint32 `json:"minActiveNodes"`
	MessageTTL       int64  `json:"messageTTL"`
	MaxMessageSize   uint64 `json:"maxMessageSize"`
	StorageQuota     uint64 `json:"storageQuota"`
	SlashPercent     uint64 `json:"slashPercent"`
	RewardPerMessage uint64 `json:"rewardPerMessage"`
}

// GetNetworkParams returns network parameters
func (s *Service) GetNetworkParams(r *http.Request, args *GetNetworkParamsArgs, reply *GetNetworkParamsReply) error {
	config := s.vm.GetConfig()

	reply.MinStake = config.MinStake
	reply.StakeLockPeriod = config.StakeLockPeriod
	reply.EpochDuration = config.EpochDuration
	reply.NodesPerSwarm = config.NodesPerSwarm
	reply.MinActiveNodes = config.MinActiveNodes
	reply.MessageTTL = config.MessageTTL
	reply.MaxMessageSize = config.MaxMessageSize
	reply.StorageQuota = config.StorageQuota
	reply.SlashPercent = config.SlashPercent
	reply.RewardPerMessage = config.RewardPerMessage
	return nil
}

// ========== Health Endpoints ==========

// HealthArgs are the arguments to Health
type HealthArgs struct{}

// HealthReply is the reply from Health
type HealthReply struct {
	Healthy     bool   `json:"healthy"`
	ActiveNodes uint32 `json:"activeNodes"`
	MinRequired uint32 `json:"minRequired"`
}

// Health returns the health status
func (s *Service) Health(r *http.Request, args *HealthArgs, reply *HealthReply) error {
	result, err := s.vm.HealthCheck(context.Background())
	if err != nil {
		return err
	}

	reply.Healthy = result.Healthy
	reply.ActiveNodes = s.vm.registry.GetActiveNodeCount()
	reply.MinRequired = s.vm.config.MinActiveNodes
	return nil
}
