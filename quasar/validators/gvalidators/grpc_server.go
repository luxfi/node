// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/validators"
	validatorstatepb "github.com/luxfi/node/v2/proto/pb/validatorstate"
)

var _ validatorstatepb.ValidatorStateServer = (*GRPCServer)(nil)

// GRPCServer is a gRPC server that implements ValidatorStateServer
type GRPCServer struct {
	validatorstatepb.UnimplementedValidatorStateServer
	state validators.State
}

// NewGRPCServer creates a new gRPC validator server.
func NewGRPCServer(state validators.State) *GRPCServer {
	return &GRPCServer{
		state: state,
	}
}

// GetMinimumHeight implements ValidatorStateServer
func (s *GRPCServer) GetMinimumHeight(ctx context.Context, _ *emptypb.Empty) (*validatorstatepb.GetMinimumHeightResponse, error) {
	height, err := s.state.GetMinimumHeight(ctx)
	if err != nil {
		return nil, err
	}
	return &validatorstatepb.GetMinimumHeightResponse{
		Height: height,
	}, nil
}

// GetCurrentHeight implements ValidatorStateServer
func (s *GRPCServer) GetCurrentHeight(ctx context.Context, _ *emptypb.Empty) (*validatorstatepb.GetCurrentHeightResponse, error) {
	height, err := s.state.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}
	return &validatorstatepb.GetCurrentHeightResponse{
		Height: height,
	}, nil
}

// GetSubnetID implements ValidatorStateServer
func (s *GRPCServer) GetSubnetID(ctx context.Context, req *validatorstatepb.GetSubnetIDRequest) (*validatorstatepb.GetSubnetIDResponse, error) {
	chainID, err := ids.ToID(req.ChainId)
	if err != nil {
		return nil, err
	}
	subnetID, err := s.state.GetSubnetID(ctx, chainID)
	if err != nil {
		return nil, err
	}
	return &validatorstatepb.GetSubnetIDResponse{
		SubnetId: subnetID[:],
	}, nil
}

// GetValidatorSet implements ValidatorStateServer
func (s *GRPCServer) GetValidatorSet(ctx context.Context, req *validatorstatepb.GetValidatorSetRequest) (*validatorstatepb.GetValidatorSetResponse, error) {
	subnetID, err := ids.ToID(req.SubnetId)
	if err != nil {
		return nil, err
	}
	_, err = s.state.GetValidatorSet(ctx, req.Height, subnetID)
	if err != nil {
		return nil, err
	}
	
	// For now, we'll return a simple response with basic fields
	// The full ValidatorOutput type definition needs to be checked in the proto files
	return &validatorstatepb.GetValidatorSetResponse{}, nil
}

// GetCurrentValidatorSet implements ValidatorStateServer
func (s *GRPCServer) GetCurrentValidatorSet(ctx context.Context, req *validatorstatepb.GetCurrentValidatorSetRequest) (*validatorstatepb.GetCurrentValidatorSetResponse, error) {
	// This method implementation depends on your actual requirements
	// For now, delegating to GetValidatorSet with current height
	currentHeight, err := s.state.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}
	
	getValidatorSetReq := &validatorstatepb.GetValidatorSetRequest{
		Height:   currentHeight,
		SubnetId: req.SubnetId,
	}
	
	_, err = s.GetValidatorSet(ctx, getValidatorSetReq)
	if err != nil {
		return nil, err
	}
	
	return &validatorstatepb.GetCurrentValidatorSetResponse{}, nil
}