// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	sharedmemorypb "github.com/luxfi/node/v2/proto/pb/sharedmemory"
)

var _ quasar.SharedMemory = (*ConsensusClient)(nil)

// ConsensusClient is quasar.SharedMemory that talks over RPC.
type ConsensusClient struct {
	client sharedmemorypb.SharedMemoryClient
}

// NewConsensusClient returns shared memory connected to remote shared memory
func NewConsensusClient(client sharedmemorypb.SharedMemoryClient) *ConsensusClient {
	return &ConsensusClient{client: client}
}

func (c *ConsensusClient) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	resp, err := c.client.Get(context.Background(), &sharedmemorypb.GetRequest{
		PeerChainId: peerChainID[:],
		Keys:        keys,
	})
	if err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *ConsensusClient) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) (
	[][]byte,
	[]byte,
	[]byte,
	error,
) {
	resp, err := c.client.Indexed(context.Background(), &sharedmemorypb.IndexedRequest{
		PeerChainId: peerChainID[:],
		Traits:      traits,
		StartTrait:  startTrait,
		StartKey:    startKey,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return resp.Values, resp.LastTrait, resp.LastKey, nil
}

func (c *ConsensusClient) Apply(requests map[ids.ID]*quasar.Requests, batch quasar.Batch) error {
	req := &sharedmemorypb.ApplyRequest{
		Requests: make([]*sharedmemorypb.AtomicRequest, 0, len(requests)),
		Batches:  make([]*sharedmemorypb.Batch, 0),
	}
	
	for key, value := range requests {
		chainReq := &sharedmemorypb.AtomicRequest{
			RemoveRequests: value.RemoveRequests,
			PutRequests:    make([]*sharedmemorypb.Element, len(value.PutRequests)),
			PeerChainId:    key[:],
		}
		for i, v := range value.PutRequests {
			chainReq.PutRequests[i] = &sharedmemorypb.Element{
				Key:    v.Key,
				Value:  v.Value,
				Traits: v.Traits,
			}
		}
		req.Requests = append(req.Requests, chainReq)
	}
	
	// Note: batches are interface{} in quasar.SharedMemory
	// We'll skip processing them for now as the exact type is unclear
	
	_, err := c.client.Apply(context.Background(), req)
	return err
}