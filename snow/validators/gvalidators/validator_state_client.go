// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

var _ validators.State = (*Client)(nil)

type Client struct {
	client pb.ValidatorStateClient
}

func NewClient(client pb.ValidatorStateClient) *Client {
	return &Client{client: client}
}

func (c *Client) GetMinimumHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetMinimumHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetCurrentHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetCurrentHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 85ab999a4 (Improve subnetID lookup to support non-whitelisted subnets (#2354))
func (c *Client) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	resp, err := c.client.GetSubnetID(ctx, &pb.GetSubnetIDRequest{
		ChainId: chainID[:],
	})
	if err != nil {
		return ids.Empty, err
	}
	return ids.ToID(resp.SubnetId)
}

<<<<<<< HEAD
=======
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
>>>>>>> 85ab999a4 (Improve subnetID lookup to support non-whitelisted subnets (#2354))
func (c *Client) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
<<<<<<< HEAD
<<<<<<< HEAD
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
=======
func (c *Client) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
=======
) (map[ids.NodeID]*validators.Validator, error) {
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
	resp, err := c.client.GetValidatorSet(ctx, &pb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, err
	}

<<<<<<< HEAD
<<<<<<< HEAD
	vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, len(resp.Validators))
=======
	vdrs := make(map[ids.NodeID]*validators.Validator, len(resp.Validators))
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
	vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, len(resp.Validators))
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
	for _, validator := range resp.Validators {
		nodeID, err := ids.ToNodeID(validator.NodeId)
		if err != nil {
			return nil, err
		}
		var publicKey *bls.PublicKey
		if len(validator.PublicKey) > 0 {
			publicKey, err = bls.PublicKeyFromBytes(validator.PublicKey)
			if err != nil {
				return nil, err
			}
		}
<<<<<<< HEAD
<<<<<<< HEAD
		vdrs[nodeID] = &validators.GetValidatorOutput{
=======
		vdrs[nodeID] = &validators.Validator{
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
		vdrs[nodeID] = &validators.GetValidatorOutput{
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
			NodeID:    nodeID,
			PublicKey: publicKey,
			Weight:    validator.Weight,
		}
	}
	return vdrs, nil
}
