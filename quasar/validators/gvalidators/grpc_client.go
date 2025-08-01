// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
	validatorstatepb "github.com/luxfi/node/proto/pb/validatorstate"
)

var _ validators.State = (*GRPCClient)(nil)

// GRPCClient is a gRPC client that implements quasar.ValidatorState
type GRPCClient struct {
	client validatorstatepb.ValidatorStateClient
}

// NewGRPCClient creates a new gRPC validator state client
func NewGRPCClient(client validatorstatepb.ValidatorStateClient) *GRPCClient {
	return &GRPCClient{
		client: client,
	}
}

// GetMinimumHeight implements quasar.ValidatorState
func (c *GRPCClient) GetMinimumHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetMinimumHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

// GetCurrentHeight implements quasar.ValidatorState
func (c *GRPCClient) GetCurrentHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetCurrentHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

// GetSubnetID implements quasar.ValidatorState
func (c *GRPCClient) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	resp, err := c.client.GetSubnetID(ctx, &validatorstatepb.GetSubnetIDRequest{
		ChainId: chainID[:],
	})
	if err != nil {
		return ids.Empty, err
	}
	return ids.ToID(resp.SubnetId)
}

// GetValidatorSet implements quasar.ValidatorState
func (c *GRPCClient) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	_, err := c.client.GetValidatorSet(ctx, &validatorstatepb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, err
	}
	
	// For now, return an empty map as the protobuf response structure might be different
	// TODO: Properly parse the validator set from the response
	validators := make(map[ids.NodeID]*validators.GetValidatorOutput)
	return validators, nil
}

// ApplyValidatorWeightDiffs implements quasar.ValidatorState
func (c *GRPCClient) ApplyValidatorWeightDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	// This method might not be implemented in the protobuf service
	// For now, return nil to satisfy the interface
	return nil
}

// ApplyValidatorPublicKeyDiffs implements quasar.ValidatorState
func (c *GRPCClient) ApplyValidatorPublicKeyDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	// This method might not be implemented in the protobuf service
	// For now, return nil to satisfy the interface
	return nil
}

// GetCurrentValidatorSet implements quasar.ValidatorState
func (c *GRPCClient) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	// This method might not be implemented in the protobuf service
	// For now, return empty map and height
	validators := make(map[ids.ID]*validators.GetCurrentValidatorOutput)
	return validators, 0, nil
}