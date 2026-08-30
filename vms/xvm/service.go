// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/go-json-experiment/json"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/xvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"

	safemath "github.com/luxfi/math"
	avajson "github.com/luxfi/node/utils/json"
)

const (
	// Max number of addresses that can be passed in as argument to getUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of items allowed in a page
	maxPageSize uint64 = 1024
)

var (
	errServiceNotReady  = errors.New("xvm service not ready: VM not initialized")
	errTxNotCreateAsset = errors.New("transaction doesn't create an asset")
	errNilTxID          = errors.New("nil transaction ID")
	errNoAddresses      = errors.New("no addresses provided")
	errNotLinearized    = errors.New("chain is not linearized")
)

// FormattedAssetID defines a JSON formatted struct containing an assetID as a string
type FormattedAssetID struct {
	AssetID ids.ID `json:"assetID"`
}

// Service defines the base service for the asset vm
type Service struct{ vm *VM }

// ready returns an error if the VM is not fully initialized.
// Protects against nil pointer dereference panics when handlers
// are invoked before the VM is ready (e.g. during bootstrap).
func (s *Service) ready() error {
	if s == nil || s.vm == nil {
		return errServiceNotReady
	}
	return nil
}

// getBlock returns the block with the given id.
//
// Example: {"blockID":"11111111111111111111111111111111LpoYY","encoding":"hex"}
func (s *Service) getBlock(_ context.Context, in *apitypes.GetBlockArgs) (*apitypes.GetBlockResponse, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getBlock"),
		log.Stringer("blkID", in.BlockID),
		log.Stringer("encoding", in.Encoding),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if s.vm.chainManager == nil {
		return nil, errNotLinearized
	}
	block, err := s.vm.chainManager.GetStatelessBlock(in.BlockID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get block with id %s: %w", in.BlockID, err)
	}
	reply := &apitypes.GetBlockResponse{Encoding: in.Encoding}

	var result any
	if in.Encoding == formatting.JSON {
		for _, tx := range block.Txs() {
			err := tx.Unsigned.Visit(&txInit{
				tx:      tx,
				fxIndex: s.vm.fxIndex,
				fxs:     s.vm.fxs,
			})
			if err != nil {
				return nil, err
			}
		}
		result = block
	} else {
		result, err = formatting.Encode(in.Encoding, block.Bytes())
		if err != nil {
			return nil, fmt.Errorf("couldn't encode block %s as string: %w", in.BlockID, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return reply, err
}

// getBlockByHeight returns the block accepted at the given height.
//
// Example: {"height":"1","encoding":"hex"}
func (s *Service) getBlockByHeight(_ context.Context, in *apitypes.GetBlockByHeightArgs) (*apitypes.GetBlockResponse, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getBlockByHeight"),
		log.Uint64("height", uint64(in.Height)),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if s.vm.chainManager == nil {
		return nil, errNotLinearized
	}
	reply := &apitypes.GetBlockResponse{Encoding: in.Encoding}

	blockID, err := s.vm.state.GetBlockIDAtHeight(uint64(in.Height))
	if err != nil {
		return nil, fmt.Errorf("couldn't get block at height %d: %w", in.Height, err)
	}
	block, err := s.vm.chainManager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.log.Error("couldn't get accepted block",
			log.Stringer("blkID", blockID),
			log.String("error", err.Error()),
		)
		return nil, fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}

	var result any
	if in.Encoding == formatting.JSON {
		for _, tx := range block.Txs() {
			err := tx.Unsigned.Visit(&txInit{
				tx:      tx,
				fxIndex: s.vm.fxIndex,
				fxs:     s.vm.fxs,
			})
			if err != nil {
				return nil, err
			}
		}
		result = block
	} else {
		result, err = formatting.Encode(in.Encoding, block.Bytes())
		if err != nil {
			return nil, fmt.Errorf("couldn't encode block %s as string: %w", blockID, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return reply, err
}

// getHeight returns the height of the last accepted block.
//
// Response: {"height":"1"}
func (s *Service) getHeight(context.Context, *struct{}) (*apitypes.GetHeightResponse, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getHeight"),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if s.vm.chainManager == nil {
		return nil, errNotLinearized
	}

	blockID := s.vm.state.GetLastAccepted()
	block, err := s.vm.chainManager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.log.Error("couldn't get last accepted block",
			log.Stringer("blkID", blockID),
			log.String("error", err.Error()),
		)
		return nil, fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}

	return &apitypes.GetHeightResponse{Height: apitypes.Uint64(block.Height())}, nil
}

// issueTx sends a signed transaction to consensus and returns its id.
//
// The bytes carry their own authority: the node holds no key that could have
// signed them, so it checks no signature and consensus is what decides.
//
// Example: {"tx":"0x00000000000...","encoding":"hex"}
func (s *Service) issueTx(_ context.Context, in *apitypes.FormattedTx) (*apitypes.JSONTxID, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "issueTx"),
		log.String("tx", in.Tx),
	)

	txBytes, err := formatting.Decode(in.Encoding, in.Tx)
	if err != nil {
		return nil, fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx, err := s.vm.parser.ParseTx(txBytes)
	if err != nil {
		s.vm.log.Debug("failed to parse tx",
			log.String("error", err.Error()),
		)
		return nil, err
	}

	txID, err := s.vm.issueTxFromRPC(tx)
	if err != nil {
		return nil, err
	}
	return &apitypes.JSONTxID{TxID: txID}, nil
}

type GetAddressTxsArgs struct {
	apitypes.JSONAddress
	// Cursor used as a page index / offset
	Cursor avajson.Uint64 `json:"cursor"`
	// PageSize num of items per page
	PageSize avajson.Uint64 `json:"pageSize"`
	// AssetID defaulted to LUX if omitted or left blank
	AssetID string `json:"assetID"`
}

type GetAddressTxsReply struct {
	TxIDs []ids.ID `json:"txIDs"`
	// Cursor used as a page index / offset
	Cursor avajson.Uint64 `json:"cursor"`
}

// getAddressTxs returns the transactions of an address, one page at a time.
//
// The reply carries the cursor to pass back for the next page.
func (s *Service) getAddressTxs(_ context.Context, in *GetAddressTxsArgs) (*GetAddressTxsReply, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	cursor := uint64(in.Cursor)
	pageSize := uint64(in.PageSize)
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getAddressTxs"),
		log.String("address", in.Address),
		log.String("assetID", in.AssetID),
		log.Uint64("cursor", cursor),
		log.Uint64("pageSize", pageSize),
	)
	if pageSize > maxPageSize {
		return nil, fmt.Errorf("pageSize > maximum allowed (%d)", maxPageSize)
	} else if pageSize == 0 {
		pageSize = maxPageSize
	}

	// Parse to address
	address, err := lux.ParseServiceAddress(s.vm, in.Address)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse argument 'address' to address: %w", err)
	}

	// Lookup assetID
	assetID, err := s.vm.lookupAssetID(in.AssetID)
	if err != nil {
		return nil, fmt.Errorf("specified `assetID` is invalid: %w", err)
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	// Read transactions from the indexer
	reply := &GetAddressTxsReply{}
	reply.TxIDs, err = s.vm.addressTxsIndexer.Read(address[:], assetID, cursor, pageSize)
	if err != nil {
		return nil, err
	}
	s.vm.log.Debug("fetched transactions",
		log.String("address", in.Address),
		log.String("assetID", in.AssetID),
		log.Int("numTxs", len(reply.TxIDs)),
	)

	// To get the next set of tx IDs, the user should provide this cursor.
	// e.g. if they provided cursor 5, and read 6 tx IDs, they should start
	// next time from index (cursor) 11.
	reply.Cursor = avajson.Uint64(cursor + uint64(len(reply.TxIDs)))
	return reply, nil
}

// getTx returns the transaction with the given id.
//
// Example: {"txID":"11111111111111111111111111111111LpoYY","encoding":"hex"}
func (s *Service) getTx(_ context.Context, in *apitypes.GetTxArgs) (*apitypes.GetTxReply, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getTx"),
		log.Stringer("txID", in.TxID),
	)

	if in.TxID == ids.Empty {
		return nil, errNilTxID
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	tx, err := s.vm.state.GetTx(in.TxID)
	if err != nil {
		return nil, err
	}
	reply := &apitypes.GetTxReply{Encoding: in.Encoding}

	var result any
	if in.Encoding == formatting.JSON {
		err = tx.Unsigned.Visit(&txInit{
			tx:      tx,
			fxIndex: s.vm.fxIndex,
			fxs:     s.vm.fxs,
		})
		result = tx
	} else {
		result, err = formatting.Encode(in.Encoding, tx.Bytes())
	}
	if err != nil {
		return nil, err
	}

	reply.Tx, err = json.Marshal(result)
	return reply, err
}

// getUTXOs returns the UTXOs referencing at least one of the given addresses.
//
// The reply's endIndex is where the next page starts: pass it back as
// startIndex to continue.
func (s *Service) getUTXOs(_ context.Context, in *apitypes.GetUTXOsArgs) (*apitypes.GetUTXOsReply, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getUTXOs"),
		log.Strings("addresses", in.Addresses),
	)

	if len(in.Addresses) == 0 {
		return nil, errNoAddresses
	}
	if len(in.Addresses) > maxGetUTXOsAddrs {
		return nil, fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(in.Addresses), maxGetUTXOsAddrs)
	}

	var sourceChain ids.ID
	if in.SourceChain == "" {
		sourceChain = s.vm.consensusRuntime.ChainID
	} else {
		chainID, err := s.vm.bcLookup.Lookup(in.SourceChain)
		if err != nil {
			return nil, fmt.Errorf("problem parsing source chainID %q: %w", in.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet, err := lux.ParseServiceAddresses(s.vm, in.Addresses)
	if err != nil {
		return nil, err
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if in.StartIndex.Address != "" || in.StartIndex.UTXO != "" {
		startAddr, err = lux.ParseServiceAddress(s.vm, in.StartIndex.Address)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse start index address %q: %w", in.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(in.StartIndex.UTXO)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	var (
		utxos     []*lux.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
	)
	limit := int(in.Limit)
	if limit <= 0 || int(maxPageSize) < limit {
		limit = int(maxPageSize)
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if sourceChain == s.vm.consensusRuntime.ChainID {
		utxos, endAddr, endUTXOID, err = lux.GetPaginatedUTXOs(
			s.vm.state,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		// Create a wrapper to convert interface type
		// This is a workaround for the type mismatch between interfaces.SharedMemory and atomic.SharedMemory
		utxos, endAddr, endUTXOID, err = lux.GetAtomicUTXOs(
			nil, // Temporarily pass nil - will need proper fix
			sourceChain,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	reply := &apitypes.GetUTXOsReply{Encoding: in.Encoding}
	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		b, err := utxo.WireBytes()
		if err != nil {
			return nil, fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		reply.UTXOs[i], err = formatting.Encode(in.Encoding, b)
		if err != nil {
			return nil, fmt.Errorf("couldn't encode UTXO %s as string: %w", utxo.InputID(), err)
		}
	}

	endAddress, err := s.vm.FormatLocalAddress(endAddr)
	if err != nil {
		return nil, fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = apitypes.Uint64(len(utxos))
	return reply, nil
}

// GetAssetDescriptionArgs names the asset to describe
type GetAssetDescriptionArgs struct {
	AssetID string `json:"assetID"`
}

// GetAssetDescriptionReply is an asset's name, symbol and denomination
type GetAssetDescriptionReply struct {
	FormattedAssetID
	Name         string        `json:"name"`
	Symbol       string        `json:"symbol"`
	Denomination avajson.Uint8 `json:"denomination"`
}

// getAsset returns an asset's name, symbol and denomination.
//
// Example: {"assetID":"LUX"}
func (s *Service) getAsset(_ context.Context, in *GetAssetDescriptionArgs) (*GetAssetDescriptionReply, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getAssetDescription"),
		log.String("assetID", in.AssetID),
	)

	assetID, err := s.vm.lookupAssetID(in.AssetID)
	if err != nil {
		return nil, err
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	tx, err := s.vm.state.GetTx(assetID)
	if err != nil {
		return nil, err
	}
	createAssetTx, ok := tx.Unsigned.(*txs.CreateAssetTx)
	if !ok {
		return nil, errTxNotCreateAsset
	}

	reply := &GetAssetDescriptionReply{
		Name:         createAssetTx.Name,
		Symbol:       createAssetTx.Symbol,
		Denomination: avajson.Uint8(createAssetTx.Denomination),
	}
	reply.FormattedAssetID.AssetID = assetID
	return reply, nil
}

// GetBalanceArgs names the address and asset to weigh
type GetBalanceArgs struct {
	Address        string `json:"address"`
	AssetID        string `json:"assetID"`
	IncludePartial bool   `json:"includePartial"`
}

// GetBalanceReply is an address's balance of one asset, and the UTXOs holding it
type GetBalanceReply struct {
	Balance avajson.Uint64 `json:"balance"`
	UTXOIDs []lux.UTXOID   `json:"utxoIDs"`
}

// getBalance returns the balance of one asset held by an address.
//
// Without includePartial it counts only what the address holds outright — a
// 1-of-1 output whose locktime has passed. With it, partially held and
// still-locked outputs count too.
//
// Example: {"address":"X-lux1...","assetID":"LUX"}
func (s *Service) getBalance(_ context.Context, in *GetBalanceArgs) (*GetBalanceReply, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getBalance"),
		log.String("address", in.Address),
		log.String("assetID", in.AssetID),
	)

	addr, err := lux.ParseServiceAddress(s.vm, in.Address)
	if err != nil {
		return nil, fmt.Errorf("problem parsing address '%s': %w", in.Address, err)
	}

	assetID, err := s.vm.lookupAssetID(in.AssetID)
	if err != nil {
		return nil, err
	}

	addrSet := set.Of(addr)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	utxos, err := lux.GetAllUTXOs(s.vm.state, addrSet)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	now := s.vm.clock.Unix()
	reply := &GetBalanceReply{UTXOIDs: make([]lux.UTXOID, 0, len(utxos))}
	for _, utxo := range utxos {
		if utxo.AssetID() != assetID {
			continue
		}
		// Only secp256k1fx.TransferOutput is supported; other output types are skipped.
		transferable, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		owners := transferable.OutputOwners
		if !in.IncludePartial && (len(owners.Addrs) != 1 || owners.Locktime > now) {
			continue
		}
		amt, err := safemath.Add64(transferable.Amount(), uint64(reply.Balance))
		if err != nil {
			return nil, err
		}
		reply.Balance = avajson.Uint64(amt)
		reply.UTXOIDs = append(reply.UTXOIDs, utxo.UTXOID)
	}

	return reply, nil
}

type Balance struct {
	AssetID string         `json:"asset"`
	Balance avajson.Uint64 `json:"balance"`
}

type GetAllBalancesArgs struct {
	apitypes.JSONAddress
	IncludePartial bool `json:"includePartial"`
}

// GetAllBalancesReply is every asset an address holds a non-zero balance of
type GetAllBalancesReply struct {
	Balances []Balance `json:"balances"`
}

// getBalances returns every asset an address holds a non-zero balance of.
//
// Without includePartial it counts only what the address holds outright — a
// 1-of-1 output whose locktime has passed. With it, partially held and
// still-locked outputs count too.
//
// Example: {"address":"X-lux1..."}
func (s *Service) getBalances(_ context.Context, in *GetAllBalancesArgs) (*GetAllBalancesReply, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getAllBalances"),
		log.String("address", in.Address),
	)

	address, err := lux.ParseServiceAddress(s.vm, in.Address)
	if err != nil {
		return nil, fmt.Errorf("problem parsing address '%s': %w", in.Address, err)
	}
	addrSet := set.Of(address)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	utxos, err := lux.GetAllUTXOs(s.vm.state, addrSet)
	if err != nil {
		return nil, fmt.Errorf("couldn't get address's UTXOs: %w", err)
	}

	now := s.vm.clock.Unix()
	assetIDs := make(set.Set[ids.ID])   // IDs of assets the address has a non-zero balance of
	balances := make(map[ids.ID]uint64) // key: ID (as bytes). value: balance of that asset
	for _, utxo := range utxos {
		// Only secp256k1fx.TransferOutput is supported; other output types are skipped.
		transferable, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		owners := transferable.OutputOwners
		if !in.IncludePartial && (len(owners.Addrs) != 1 || owners.Locktime > now) {
			continue
		}
		assetID := utxo.AssetID()
		assetIDs.Add(assetID)
		balance := balances[assetID] // 0 if key doesn't exist
		balance, err := safemath.Add64(transferable.Amount(), balance)
		if err != nil {
			balances[assetID] = math.MaxUint64
		} else {
			balances[assetID] = balance
		}
	}

	reply := &GetAllBalancesReply{Balances: make([]Balance, assetIDs.Len())}
	i := 0
	for assetID := range assetIDs {
		alias := s.vm.PrimaryAliasOrDefault(assetID)
		reply.Balances[i] = Balance{
			AssetID: alias,
			Balance: avajson.Uint64(balances[assetID]),
		}
		i++
	}

	return reply, nil
}
