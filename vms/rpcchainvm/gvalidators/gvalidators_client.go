package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
	validatorstatepb "github.com/luxfi/node/proto/pb/validatorstate"
)

// NewClient creates a new validator state client
func NewClient(client validatorstatepb.ValidatorStateClient) validators.State {
	return &Client{client: client}
}

// Client is a ValidatorState client
type Client struct {
	client validatorstatepb.ValidatorStateClient
}

func (c *Client) GetMinimumHeight(ctx context.Context) (uint64, error) {
	// validators.State doesn't have GetMinimumHeight - return 0
	return 0, nil
}

func (c *Client) GetCurrentHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetCurrentHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	resp, err := c.client.GetSubnetID(ctx, &validatorstatepb.GetSubnetIDRequest{
		ChainId: chainID[:],
	})
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(resp.SubnetId)
}

func (c *Client) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	resp, err := c.client.GetValidatorSet(ctx, &validatorstatepb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, err
	}

	validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput, len(resp.Validators))
	for _, v := range resp.Validators {
		nodeID, err := ids.ToNodeID(v.NodeId)
		if err != nil {
			return nil, err
		}
		validatorSet[nodeID] = &validators.GetValidatorOutput{
			NodeID: nodeID,
			Weight: v.Weight,
		}
	}
	return validatorSet, nil
}

func (c *Client) GetCurrentValidatorSet(ctx context.Context, subnetID ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	// For now, just return empty map and current height since we don't have full support for this
	height, err := c.GetCurrentHeight(ctx)
	if err != nil {
		return nil, 0, err
	}
	return nil, height, nil
}
