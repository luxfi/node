package gvalidators

import (
	"context"
	
	"google.golang.org/protobuf/types/known/emptypb"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/validators"
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

func (c *Client) GetCurrentHeight() (uint64, error) {
	resp, err := c.client.GetCurrentHeight(context.Background(), &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	resp, err := c.client.GetValidatorSet(context.Background(), &validatorstatepb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, err
	}

	validators := make(map[ids.NodeID]uint64, len(resp.Validators))
	for _, v := range resp.Validators {
		nodeID, err := ids.ToNodeID(v.NodeId)
		if err != nil {
			return nil, err
		}
		validators[nodeID] = v.Weight
	}
	return validators, nil
}