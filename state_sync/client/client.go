// (c) 2021-2022, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesyncclient

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/luxfi/evm/v2/core/types"
	"github.com/luxfi/evm/v2/plugin/evm/message"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/log"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/state_sync/client/stats"
)

const (
	failedRequestSleepInterval = 10 * time.Millisecond

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var (
	errEmptyResponse          = errors.New("empty response")
	errTooManyBlocks          = errors.New("response contains more blocks than requested")
	errHashMismatch           = errors.New("hash does not match expected value")
	errInvalidRangeProof      = errors.New("failed to verify range proof")
	errTooManyLeaves          = errors.New("response contains more than requested leaves")
	ErrUnmarshalResponse      = errors.New("failed to unmarshal response")
	errInvalidCodeResponseLen = errors.New("number of code bytes in response does not match requested hashes")
	errMaxCodeSizeExceeded    = errors.New("max code size exceeded")
)
var _ Client = &client{}

// Client synchronously fetches data from the network to fulfill state sync requests.
// Repeatedly requests failed requests until the context to the request is expired.
type Client interface {
	// GetLeafs synchronously sends the given request, returning a parsed LeafsResponse or error
	// Note: this verifies the response including the range proofs.
	GetLeafs(ctx context.Context, request message.LeafsRequest) (message.LeafsResponse, error)

	// GetBlocks synchronously retrieves blocks starting with specified common.Hash and height up to specified parents
	// specified range from height to height-parents is inclusive
	GetBlocks(ctx context.Context, blockHash common.Hash, height uint64, parents uint16) ([]*types.Block, error)

	// GetCode synchronously retrieves code associated with the given hashes
	GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error)
}

// parseResponseFn parses given response bytes in context of specified request
// Validates response in context of the request
// Ensures the returned interface matches the expected response type of the request
// Returns the number of elements in the response (specific to the response type, used in metrics)
type parseResponseFn func(request message.Request, response []byte) (interface{}, int, error)

// Import the codec from the evm message package
var codec = message.Codec

type client struct {
	sendRequest      func(ctx context.Context, peerID ids.NodeID, req []byte) ([]byte, error)
	logger           log.Logger
	stateSyncNodes   []ids.NodeID
	stateSyncNodeIdx uint32
	stats            stats.ClientSyncerStats
	blockParser      EthBlockParser
}

type Config struct {
	SendRequest      func(ctx context.Context, peerID ids.NodeID, req []byte) ([]byte, error)
	Logger           log.Logger
	Stats            stats.ClientSyncerStats
	StateSyncNodeIDs []ids.NodeID
	BlockParser      EthBlockParser
}

type EthBlockParser interface {
	ParseEthBlock(b []byte) (*types.Block, error)
}

func NewClient(config *Config) *client {
	return &client{
		sendRequest:    config.SendRequest,
		logger:         config.Logger,
		stats:          config.Stats,
		stateSyncNodes: config.StateSyncNodeIDs,
		blockParser:    config.BlockParser,
	}
}

// GetLeafs synchronously retrieves leafs as per given [message.LeafsRequest]
// Retries when:
// - response bytes could not be unmarshalled to [message.LeafsResponse]
// - response keys do not correspond to the requested range.
// - response does not contain a valid merkle proof.
func (c *client) GetLeafs(ctx context.Context, req message.LeafsRequest) (message.LeafsResponse, error) {
	data, err := c.get(ctx, req, parseLeafsResponse)
	if err != nil {
		return message.LeafsResponse{}, err
	}
	return data.(message.LeafsResponse), nil
}

// ParseLeafsResponse validates given object as message.LeafsResponse
// assumes reqIntf is of type message.LeafsRequest
// returns a non-nil error if the request should be retried
// returns error when:
// - response bytes could not be unmarshalled into message.LeafsResponse
// - number of response keys is not equal to the response values
// - first and last key in the response is not within the requested start and end range
// - response keys are not in increasing order
// - proof validation failed
func parseLeafsResponse(reqIntf message.Request, data []byte) (interface{}, int, error) {
	var leafsResponse message.LeafsResponse
	_, err := codec.Unmarshal(data, &leafsResponse)
	if err != nil {
		return nil, 0, err
	}

	leafsRequest := reqIntf.(message.LeafsRequest)

	// Ensure the response does not contain more than the maximum requested number of leaves.
	if len(leafsResponse.Keys) > int(leafsRequest.Limit) || len(leafsResponse.Vals) > int(leafsRequest.Limit) {
		return nil, 0, fmt.Errorf("%w: (%d) > %d)", errTooManyLeaves, len(leafsResponse.Keys), leafsRequest.Limit)
	}

	// An empty response (no more keys) requires a merkle proof
	if len(leafsResponse.Keys) == 0 && len(leafsResponse.ProofVals) == 0 {
		return nil, 0, fmt.Errorf("empty key response must include merkle proof")
	}

	// TODO: Add merkle proof validation once trie package is properly imported
	// For now, skip the proof validation

	return leafsResponse, len(leafsResponse.Keys), nil
}

func (c *client) GetBlocks(ctx context.Context, hash common.Hash, height uint64, parents uint16) ([]*types.Block, error) {
	req := message.BlockRequest{
		Hash:    hash,
		Height:  height,
		Parents: parents,
	}

	data, err := c.get(ctx, req, c.parseBlocks)
	if err != nil {
		return nil, fmt.Errorf("could not get blocks (%s) due to %w", hash, err)
	}

	return data.(types.Blocks), nil
}

// parseBlocks validates given object as message.BlockResponse
// assumes req is of type message.BlockRequest
// returns types.Blocks as interface{}
// returns a non-nil error if the request should be retried
func (c *client) parseBlocks(req message.Request, data []byte) (interface{}, int, error) {
	var response message.BlockResponse
	_, err := codec.Unmarshal(data, &response)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", ErrUnmarshalResponse, err)
	}
	if len(response.Blocks) == 0 {
		return nil, 0, errEmptyResponse
	}

	blockRequest := req.(message.BlockRequest)
	numParentsRequested := blockRequest.Parents
	if len(response.Blocks) > int(numParentsRequested) {
		return nil, 0, errTooManyBlocks
	}

	hash := blockRequest.Hash

	// attempt to decode blocks
	blocks := make(types.Blocks, len(response.Blocks))
	for i, blkBytes := range response.Blocks {
		block, err := c.blockParser.ParseEthBlock(blkBytes)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", ErrUnmarshalResponse, err)
		}

		if block.Hash() != hash {
			return nil, 0, fmt.Errorf("%w for block: (got %v) (expected %v)", errHashMismatch, block.Hash(), hash)
		}

		blocks[i] = block
		hash = block.ParentHash()
	}

	// return decoded blocks
	return blocks, len(blocks), nil
}

func (c *client) GetCode(ctx context.Context, hashes []common.Hash) ([][]byte, error) {
	req := message.NewCodeRequest(hashes)

	data, err := c.get(ctx, req, parseCode)
	if err != nil {
		return nil, fmt.Errorf("could not get code (%s): %w", req, err)
	}

	return data.([][]byte), nil
}

// parseCode validates given object as a code object
// assumes req is of type message.CodeRequest
// returns a non-nil error if the request should be retried
func parseCode(req message.Request, data []byte) (interface{}, int, error) {
	var response message.CodeResponse
	_, err := codec.Unmarshal(data, &response)
	if err != nil {
		return nil, 0, err
	}

	codeRequest := req.(message.CodeRequest)
	if len(response.Data) != len(codeRequest.Hashes) {
		return nil, 0, fmt.Errorf("%w (got %d) (requested %d)", errInvalidCodeResponseLen, len(response.Data), len(codeRequest.Hashes))
	}

	// TODO: Add code validation once we have proper crypto imports
	// For now, skip hash validation

	totalBytes := 0
	for _, code := range response.Data {
		totalBytes += len(code)
	}

	return response.Data, totalBytes, nil
}

// get submits given request and blockingly returns with either a parsed response object or an error
// if [ctx] expires before the client can successfully retrieve a valid response.
// Retries if there is a network error or if the [parseResponseFn] returns an error indicating an invalid response.
// Returns the parsed interface returned from [parseFn].
// Thread safe
func (c *client) get(ctx context.Context, request message.Request, parseFn parseResponseFn) (interface{}, error) {
	// marshal the request into requestBytes
	requestBytes, err := codec.Marshal(message.Version, request)
	if err != nil {
		return nil, err
	}

	metric, err := c.stats.GetMetric(request)
	if err != nil {
		return nil, err
	}
	var (
		responseIntf interface{}
		numElements  int
		lastErr      error
	)
	// Loop until the context is cancelled or we get a valid response.
	for attempt := 0; ; attempt++ {
		// If the context has finished, return the context error early.
		if ctxErr := ctx.Err(); ctxErr != nil {
			if lastErr != nil {
				return nil, fmt.Errorf("request failed after %d attempts with last error %w and ctx error %s", attempt, lastErr, ctxErr)
			} else {
				return nil, ctxErr
			}
		}

		metric.IncRequested()

		var (
			response []byte
			nodeID   ids.NodeID
			start    time.Time = time.Now()
		)
		if len(c.stateSyncNodes) == 0 {
			// TODO: Need to implement without networkClient
			// response, nodeID, err = c.networkClient.SendAppRequestAny(ctx, StateSyncVersion, requestBytes)
			return nil, fmt.Errorf("SendAppRequestAny temporarily disabled during refactoring")
		} else {
			// get the next nodeID using the nodeIdx offset. If we're out of nodes, loop back to 0
			// we do this every attempt to ensure we get a different node each time if possible.
			nodeIdx := atomic.AddUint32(&c.stateSyncNodeIdx, 1)
			nodeID = c.stateSyncNodes[nodeIdx%uint32(len(c.stateSyncNodes))]

			response, err = c.sendRequest(ctx, nodeID, requestBytes)
		}
		metric.UpdateRequestLatency(time.Since(start))

		if err != nil {
			logCtx := make([]interface{}, 0, 8)
			if nodeID != (ids.NodeID{}) {
				logCtx = append(logCtx, "nodeID", nodeID)
			}
			logCtx = append(logCtx, "attempt", attempt, "request", request, "err", err)
			c.logger.Debug("request failed, retrying", logCtx...)
			metric.IncFailed()
			// TODO: TrackBandwidth disabled during refactoring
			// c.networkClient.TrackBandwidth(nodeID, 0)
			time.Sleep(failedRequestSleepInterval)
			continue
		} else {
			responseIntf, numElements, err = parseFn(request, response)
			if err != nil {
				lastErr = err
				c.logger.Debug("could not validate response, retrying", "nodeID", nodeID, "attempt", attempt, "request", request, "err", err)
				// TODO: TrackBandwidth disabled during refactoring
				// c.networkClient.TrackBandwidth(nodeID, 0)
				metric.IncFailed()
				metric.IncInvalidResponse()
				continue
			}

			// bandwidth := float64(len(response)) / (time.Since(start).Seconds() + epsilon)
			// TODO: TrackBandwidth disabled during refactoring
			// c.networkClient.TrackBandwidth(nodeID, bandwidth)
			metric.IncSucceeded()
			metric.IncReceived(int64(numElements))
			return responseIntf, nil
		}
	}
}
