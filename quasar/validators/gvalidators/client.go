// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
	validatorstate "github.com/luxfi/node/proto/pb/validatorstate"
)

// Request types for gRPC methods
type GetMinimumHeightRequest struct{}

type GetMinimumHeightResponse struct {
	Height uint64
}

type GetCurrentHeightRequest struct{}

type GetCurrentHeightResponse struct {
	Height uint64
}

type GetSubnetIDRequest struct {
	ChainID []byte
}

type GetSubnetIDResponse struct {
	SubnetID []byte
}

type GetValidatorSetRequest struct {
	Height   uint64
	SubnetID []byte
}

type GetValidatorSetResponse struct {
	Validators []*ValidatorOutput
}

type ValidatorOutput struct {
	NodeID    []byte
	PublicKey []byte
	Weight    uint64
}

// Client is a gRPC client for validator operations.
type Client struct {
	client interface{}
}

// NewClient creates a new gRPC validator client.
func NewClient(conn interface{}) *Client {
	return &Client{
		client: conn,
	}
}

// GetMinimumHeight returns the minimum height of the given validator set.
func (c *Client) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// gRPC implementation
	return 0, nil
}

// GetCurrentHeight returns the current height of the validator set.
func (c *Client) GetCurrentHeight(ctx context.Context) (uint64, error) {
	// gRPC implementation
	return 0, nil
}

// GetSubnetID returns the subnet ID of the validator set.
func (c *Client) GetSubnetID(ctx context.Context) (ids.ID, error) {
	// gRPC implementation
	return ids.Empty, nil
}

// GetValidatorSet returns the validator set at the given height.
func (c *Client) GetValidatorSet(ctx context.Context, height uint64) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// gRPC implementation
	return nil, nil
}

// Server is a gRPC server for validator operations.
type Server struct {
	state validators.State
}

// NewServer creates a new gRPC validator server.
func NewServer(state validators.State) *Server {
	return &Server{
		state: state,
	}
}

// GetMinimumHeight implements the gRPC server method.
func (s *Server) GetMinimumHeight(ctx context.Context, req interface{}) (interface{}, error) {
	height, err := s.state.GetMinimumHeight(ctx)
	if err != nil {
		return nil, err
	}
	return &GetMinimumHeightResponse{Height: height}, nil
}

// GetCurrentHeight implements the gRPC server method.
func (s *Server) GetCurrentHeight(ctx context.Context, _ *emptypb.Empty) (*validatorstate.GetCurrentHeightResponse, error) {
	height, err := s.state.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}
	return &validatorstate.GetCurrentHeightResponse{Height: height}, nil
}

// GetSubnetID implements the gRPC server method.
func (s *Server) GetSubnetID(ctx context.Context, req interface{}) (interface{}, error) {
	getSubnetIDReq := req.(*GetSubnetIDRequest)
	chainID := ids.ID{}
	copy(chainID[:], getSubnetIDReq.ChainID)
	subnetID, err := s.state.GetSubnetID(ctx, chainID)
	if err != nil {
		return nil, err
	}
	return &GetSubnetIDResponse{SubnetID: subnetID[:]}, nil
}

// GetValidatorSet implements the gRPC server method.
func (s *Server) GetValidatorSet(ctx context.Context, req interface{}) (interface{}, error) {
	getValSetReq := req.(*GetValidatorSetRequest)
	height := getValSetReq.Height
	subnetID := ids.ID{}
	copy(subnetID[:], getValSetReq.SubnetID)
	vdrs, err := s.state.GetValidatorSet(ctx, height, subnetID)
	if err != nil {
		return nil, err
	}
	
	resp := &GetValidatorSetResponse{
		Validators: make([]*ValidatorOutput, 0, len(vdrs)),
	}
	
	for nodeID, vdr := range vdrs {
		resp.Validators = append(resp.Validators, &ValidatorOutput{
			NodeID:    nodeID[:],
			PublicKey: vdr.PublicKey,
			Weight:    vdr.Weight,
		})
	}
	
	return resp, nil
}