// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"errors"
	"sync"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/example/xsvm/block"
	"github.com/luxfi/node/vms/example/xsvm/builder"
	"github.com/luxfi/node/vms/example/xsvm/chain"
	"github.com/luxfi/node/vms/example/xsvm/genesis"
	"github.com/luxfi/node/vms/example/xsvm/state"
	"github.com/luxfi/node/vms/example/xsvm/tx"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/runtime"
)

// errNoWarpSigner is why a message read here carries no signature: this example
// chain is handed no warp key, so it can produce the message and never a
// signature over it.
var errNoWarpSigner = errors.New("warp signer not available")

// Server answers what this chain holds. Its operations are registered by
// [Server.Ops].
type Server struct {
	rt      *runtime.Runtime
	genesis *genesis.Genesis
	state   database.KeyValueReader
	chain   chain.Chain
	builder builder.Builder
	lock    sync.RWMutex
}

func NewServer(
	rt *runtime.Runtime,
	genesis *genesis.Genesis,
	state database.KeyValueReader,
	chain chain.Chain,
	builder builder.Builder,
) *Server {
	return &Server{
		rt:      rt,
		genesis: genesis,
		state:   state,
		chain:   chain,
		builder: builder,
	}
}

type NetworkReply struct {
	NetworkID uint32 `json:"networkID"`
	ChainID   ids.ID `json:"chainID"`
}

// Network is the network this node runs on and the chain it answers for.
//
// Response: {"networkID": 1, "chainID": "11111111111111111111111111111111LpoYY"}
func (s *Server) getNetwork(context.Context, *struct{}) (*NetworkReply, error) {
	return &NetworkReply{
		NetworkID: s.rt.NetworkID,
		ChainID:   s.rt.ChainID,
	}, nil
}

type GenesisReply struct {
	Genesis *genesis.Genesis `json:"genesis"`
}

// Genesis is the state this chain started from.
//
// Response: {"genesis": null}
func (s *Server) getGenesis(context.Context, *struct{}) (*GenesisReply, error) {
	return &GenesisReply{Genesis: s.genesis}, nil
}

type NonceArgs struct {
	Address ids.ShortID `json:"address"`
}

type NonceReply struct {
	Nonce uint64 `json:"nonce"`
}

// Nonce is how many transactions this chain has accepted from an address.
//
// Example: {"address": "6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt"}
// Response: {"nonce": 0}
func (s *Server) getNonce(_ context.Context, in *NonceArgs) (*NonceReply, error) {
	nonce, err := state.GetNonce(s.state, in.Address)
	return &NonceReply{Nonce: nonce}, err
}

type BalanceArgs struct {
	Address ids.ShortID `json:"address"`
	AssetID ids.ID      `json:"assetID"`
}

type BalanceReply struct {
	Balance uint64 `json:"balance"`
}

// Balance is how much of one asset an address holds.
//
// Example: {"address": "6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt", "assetID": "11111111111111111111111111111111LpoYY"}
// Response: {"balance": 0}
func (s *Server) getBalance(_ context.Context, in *BalanceArgs) (*BalanceReply, error) {
	balance, err := state.GetBalance(s.state, in.Address, in.AssetID)
	return &BalanceReply{Balance: balance}, err
}

type LoanArgs struct {
	ChainID ids.ID `json:"chainID"`
}

type LoanReply struct {
	Amount uint64 `json:"amount"`
}

// Loan is how much this chain has exported to another and not had returned.
//
// Example: {"chainID": "11111111111111111111111111111111LpoYY"}
// Response: {"amount": 0}
func (s *Server) getLoan(_ context.Context, in *LoanArgs) (*LoanReply, error) {
	amount, err := state.GetLoan(s.state, in.ChainID)
	return &LoanReply{Amount: amount}, err
}

type IssueTxArgs struct {
	Tx []byte `json:"tx"`
}

type IssueTxReply struct {
	TxID ids.ID `json:"txID"`
}

// IssueTx hands this chain a signed transaction and answers with its id.
//
// Example: {"tx": null}
// Response: {"txID": "11111111111111111111111111111111LpoYY"}
func (s *Server) issueTx(ctx context.Context, in *IssueTxArgs) (*IssueTxReply, error) {
	newTx, err := tx.Parse(in.Tx)
	if err != nil {
		return nil, err
	}

	s.lock.Lock()
	err = s.builder.AddTx(ctx, newTx)
	s.lock.Unlock()
	if err != nil {
		return nil, err
	}

	txID, err := newTx.ID()
	return &IssueTxReply{TxID: txID}, err
}

type LastAcceptedReply struct {
	BlockID ids.ID           `json:"blockID"`
	Block   *block.Stateless `json:"block"`
}

// LastAccepted is the most recent block this chain accepted.
//
// Response: {"blockID": "11111111111111111111111111111111LpoYY", "block": null}
func (s *Server) getLastAccepted(context.Context, *struct{}) (*LastAcceptedReply, error) {
	s.lock.RLock()
	blkID := s.chain.LastAccepted()
	s.lock.RUnlock()

	blk, err := s.read(blkID)
	if err != nil {
		return nil, err
	}
	return &LastAcceptedReply{BlockID: blkID, Block: blk}, nil
}

type BlockArgs struct {
	BlockID ids.ID `json:"blockID"`
}

type BlockReply struct {
	Block *block.Stateless `json:"block"`
}

// Block is one block, the one whose id is asked for.
//
// Example: {"blockID": "11111111111111111111111111111111LpoYY"}
// Response: {"block": null}
func (s *Server) getBlock(_ context.Context, in *BlockArgs) (*BlockReply, error) {
	blk, err := s.read(in.BlockID)
	if err != nil {
		return nil, err
	}
	return &BlockReply{Block: blk}, nil
}

// read is one block out of state, parsed. Both block reads want it.
func (s *Server) read(blkID ids.ID) (*block.Stateless, error) {
	blkBytes, err := state.GetBlock(s.state, blkID)
	if err != nil {
		return nil, err
	}
	return block.Parse(blkBytes)
}

type MessageArgs struct {
	TxID ids.ID `json:"txID"`
}

type MessageReply struct {
	Message   *warp.UnsignedMessage `json:"message"`
	Signature []byte                `json:"signature"`
}

// Message is the warp message an export transaction produced.
//
// Example: {"txID": "11111111111111111111111111111111LpoYY"}
// Response: {"message": null, "signature": null}
func (s *Server) getMessage(_ context.Context, in *MessageArgs) (*MessageReply, error) {
	message, err := state.GetMessage(s.state, in.TxID)
	if err != nil {
		return nil, err
	}
	return &MessageReply{Message: message}, errNoWarpSigner
}
