package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	validators "github.com/luxfi/consensus/validator"
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

func (c *Client) GetCurrentHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetCurrentHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	resp, err := c.client.GetValidatorSet(ctx, &validatorstatepb.GetValidatorSetRequest{
		Height:   height,
		NetId: netID[:],
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
			Light:  v.Weight,
			Weight: v.Weight, // Both fields for compatibility
		}
	}
	return validatorSet, nil
}

func (c *Client) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Call GetValidatorSet with the same parameters
	return c.GetValidatorSet(ctx, height, netID)
}
