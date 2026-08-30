// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/go-json-experiment/json"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/database"
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
	// Max number of addresses that can be passed in as argument to GetUTXOs
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
// Protects against nil pointer dereference panics when HTTP handlers
// are invoked before the VM is ready (e.g. during bootstrap).
func (s *Service) ready() error {
	if s == nil || s.vm == nil {
		return errServiceNotReady
	}
	return nil
}

// GetBlock returns the requested block.
func (s *Service) GetBlock(_ *http.Request, args *apitypes.GetBlockArgs, reply *apitypes.GetBlockResponse) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getBlock"),
		log.Stringer("blkID", args.BlockID),
		log.Stringer("encoding", args.Encoding),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if s.vm.chainManager == nil {
		return errNotLinearized
	}
	block, err := s.vm.chainManager.GetStatelessBlock(args.BlockID)
	if err != nil {
		return fmt.Errorf("couldn't get block with id %s: %w", args.BlockID, err)
	}
	reply.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		// InitRuntime is no longer needed with new consensus
		for _, tx := range block.Txs() {
			err := tx.Unsigned.Visit(&txInit{
				tx:      tx,
				fxIndex: s.vm.fxIndex,
				fxs:     s.vm.fxs,
			})
			if err != nil {
				return err
			}
		}
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as string: %w", args.BlockID, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return err
}

// GetBlockByHeight returns the block at the given height.
func (s *Service) GetBlockByHeight(_ *http.Request, args *apitypes.GetBlockByHeightArgs, reply *apitypes.GetBlockResponse) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getBlockByHeight"),
		log.Uint64("height", uint64(args.Height)),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if s.vm.chainManager == nil {
		return errNotLinearized
	}
	reply.Encoding = args.Encoding

	blockID, err := s.vm.state.GetBlockIDAtHeight(uint64(args.Height))
	if err != nil {
		return fmt.Errorf("couldn't get block at height %d: %w", args.Height, err)
	}
	block, err := s.vm.chainManager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.log.Error("couldn't get accepted block",
			log.Stringer("blkID", blockID),
			log.String("error", err.Error()),
		)
		return fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}

	var result any
	if args.Encoding == formatting.JSON {
		// InitRuntime is no longer needed with new consensus
		for _, tx := range block.Txs() {
			err := tx.Unsigned.Visit(&txInit{
				tx:      tx,
				fxIndex: s.vm.fxIndex,
				fxs:     s.vm.fxs,
			})
			if err != nil {
				return err
			}
		}
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as string: %w", blockID, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return err
}

// GetHeight returns the height of the last accepted block.
func (s *Service) GetHeight(_ *http.Request, _ *struct{}, reply *apitypes.GetHeightResponse) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getHeight"),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	if s.vm.chainManager == nil {
		return errNotLinearized
	}

	blockID := s.vm.state.GetLastAccepted()
	block, err := s.vm.chainManager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.log.Error("couldn't get last accepted block",
			log.Stringer("blkID", blockID),
			log.String("error", err.Error()),
		)
		return fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}

	reply.Height = apitypes.Uint64(block.Height())
	return nil
}

// IssueTx attempts to issue a transaction into consensus
func (s *Service) IssueTx(_ *http.Request, args *apitypes.FormattedTx, reply *apitypes.JSONTxID) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "issueTx"),
		log.String("tx", args.Tx),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}

	tx, err := s.vm.parser.ParseTx(txBytes)
	if err != nil {
		s.vm.log.Debug("failed to parse tx",
			log.String("error", err.Error()),
		)
		return err
	}

	reply.TxID, err = s.vm.issueTxFromRPC(tx)
	return err
}

// GetTxStatusReply defines the GetTxStatus replies returned from the API
type GetTxStatusReply struct {
	Status choices.Status `json:"status"`
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

// GetAddressTxs returns list of transactions for a given address
func (s *Service) GetAddressTxs(_ *http.Request, args *GetAddressTxsArgs, reply *GetAddressTxsReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	cursor := uint64(args.Cursor)
	pageSize := uint64(args.PageSize)
	s.vm.log.Warn("deprecated API called",
		log.String("service", "xvm"),
		log.String("method", "getAddressTxs"),
		log.String("address", args.Address),
		log.String("assetID", args.AssetID),
		log.Uint64("cursor", cursor),
		log.Uint64("pageSize", pageSize),
	)
	if pageSize > maxPageSize {
		return fmt.Errorf("pageSize > maximum allowed (%d)", maxPageSize)
	} else if pageSize == 0 {
		pageSize = maxPageSize
	}

	// Parse to address
	address, err := lux.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse argument 'address' to address: %w", err)
	}

	// Lookup assetID
	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return fmt.Errorf("specified `assetID` is invalid: %w", err)
	}

	s.vm.log.Debug("fetching transactions",
		log.String("address", args.Address),
		log.String("assetID", args.AssetID),
		log.Uint64("cursor", cursor),
		log.Uint64("pageSize", pageSize),
	)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	// Read transactions from the indexer
	reply.TxIDs, err = s.vm.addressTxsIndexer.Read(address[:], assetID, cursor, pageSize)
	if err != nil {
		return err
	}
	s.vm.log.Debug("fetched transactions",
		log.String("address", args.Address),
		log.String("assetID", args.AssetID),
		log.Int("numTxs", len(reply.TxIDs)),
	)

	// To get the next set of tx IDs, the user should provide this cursor.
	// e.g. if they provided cursor 5, and read 6 tx IDs, they should start
	// next time from index (cursor) 11.
	reply.Cursor = avajson.Uint64(cursor + uint64(len(reply.TxIDs)))
	return nil
}

// GetTxStatus returns the status of the specified transaction
//
// Deprecated: GetTxStatus only returns Accepted or Unknown, GetTx should be
// used instead to determine if the tx was accepted.
func (s *Service) GetTxStatus(_ *http.Request, args *apitypes.JSONTxID, reply *GetTxStatusReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("deprecated API called",
		log.String("service", "xvm"),
		log.String("method", "getTxStatus"),
		log.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	_, err := s.vm.state.GetTx(args.TxID)
	switch err {
	case nil:
		reply.Status = choices.Accepted
	case database.ErrNotFound:
		reply.Status = choices.Unknown
	default:
		return err
	}
	return nil
}

// GetTx returns the specified transaction
func (s *Service) GetTx(_ *http.Request, args *apitypes.GetTxArgs, reply *apitypes.GetTxReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getTx"),
		log.Stringer("txID", args.TxID),
	)

	if args.TxID == ids.Empty {
		return errNilTxID
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	tx, err := s.vm.state.GetTx(args.TxID)
	if err != nil {
		return err
	}
	reply.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		err = tx.Unsigned.Visit(&txInit{
			tx:      tx,
			fxIndex: s.vm.fxIndex,
			fxs:     s.vm.fxs,
		})
		result = tx
	} else {
		result, err = formatting.Encode(args.Encoding, tx.Bytes())
	}
	if err != nil {
		return err
	}

	reply.Tx, err = json.Marshal(result)
	return err
}

// GetUTXOs gets all utxos for passed in addresses
func (s *Service) GetUTXOs(_ *http.Request, args *apitypes.GetUTXOsArgs, reply *apitypes.GetUTXOsReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getUTXOs"),
		log.Strings("addresses", args.Addresses),
	)

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	var sourceChain ids.ID
	if args.SourceChain == "" {
		sourceChain = s.vm.consensusRuntime.ChainID
	} else {
		chainID, err := s.vm.bcLookup.Lookup(args.SourceChain)
		if err != nil {
			return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet, err := lux.ParseServiceAddresses(s.vm, args.Addresses)
	if err != nil {
		return err
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = lux.ParseServiceAddress(s.vm, args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	var (
		utxos     []*lux.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
	)
	limit := int(args.Limit)
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
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		b, err := utxo.WireBytes()
		if err != nil {
			return fmt.Errorf("problem marshalling UTXO: %w", err)
		}
		reply.UTXOs[i], err = formatting.Encode(args.Encoding, b)
		if err != nil {
			return fmt.Errorf("couldn't encode UTXO %s as string: %w", utxo.InputID(), err)
		}
	}

	endAddress, err := s.vm.FormatLocalAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	reply.EndIndex.Address = endAddress
	reply.EndIndex.UTXO = endUTXOID.String()
	reply.NumFetched = apitypes.Uint64(len(utxos))
	reply.Encoding = args.Encoding
	return nil
}

// GetAssetDescriptionArgs are arguments for passing into GetAssetDescription requests
type GetAssetDescriptionArgs struct {
	AssetID string `json:"assetID"`
}

// GetAssetDescriptionReply defines the GetAssetDescription replies returned from the API
type GetAssetDescriptionReply struct {
	FormattedAssetID
	Name         string        `json:"name"`
	Symbol       string        `json:"symbol"`
	Denomination avajson.Uint8 `json:"denomination"`
}

// GetAssetDescription creates an empty account with the name passed in
func (s *Service) GetAssetDescription(_ *http.Request, args *GetAssetDescriptionArgs, reply *GetAssetDescriptionReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("API called",
		log.String("service", "xvm"),
		log.String("method", "getAssetDescription"),
		log.String("assetID", args.AssetID),
	)

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	tx, err := s.vm.state.GetTx(assetID)
	if err != nil {
		return err
	}
	createAssetTx, ok := tx.Unsigned.(*txs.CreateAssetTx)
	if !ok {
		return errTxNotCreateAsset
	}

	reply.FormattedAssetID.AssetID = assetID
	reply.Name = createAssetTx.Name
	reply.Symbol = createAssetTx.Symbol
	reply.Denomination = avajson.Uint8(createAssetTx.Denomination)

	return nil
}

// GetBalanceArgs are arguments for passing into GetBalance requests
type GetBalanceArgs struct {
	Address        string `json:"address"`
	AssetID        string `json:"assetID"`
	IncludePartial bool   `json:"includePartial"`
}

// GetBalanceReply defines the GetBalance replies returned from the API
type GetBalanceReply struct {
	Balance avajson.Uint64 `json:"balance"`
	UTXOIDs []lux.UTXOID   `json:"utxoIDs"`
}

// GetBalance returns the balance of an asset held by an address.
// If ![args.IncludePartial], returns only the balance held solely
// (1 out of 1 multisig) by the address and with a locktime in the past.
// Otherwise, returned balance includes assets held only partially by the
// address, and includes balances with locktime in the future.
func (s *Service) GetBalance(_ *http.Request, args *GetBalanceArgs, reply *GetBalanceReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("deprecated API called",
		log.String("service", "xvm"),
		log.String("method", "getBalance"),
		log.String("address", args.Address),
		log.String("assetID", args.AssetID),
	)

	addr, err := lux.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address '%s': %w", args.Address, err)
	}

	assetID, err := s.vm.lookupAssetID(args.AssetID)
	if err != nil {
		return err
	}

	addrSet := set.Of(addr)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	utxos, err := lux.GetAllUTXOs(s.vm.state, addrSet)
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	now := s.vm.clock.Unix()
	reply.UTXOIDs = make([]lux.UTXOID, 0, len(utxos))
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
		if !args.IncludePartial && (len(owners.Addrs) != 1 || owners.Locktime > now) {
			continue
		}
		amt, err := safemath.Add64(transferable.Amount(), uint64(reply.Balance))
		if err != nil {
			return err
		}
		reply.Balance = avajson.Uint64(amt)
		reply.UTXOIDs = append(reply.UTXOIDs, utxo.UTXOID)
	}

	return nil
}

type Balance struct {
	AssetID string         `json:"asset"`
	Balance avajson.Uint64 `json:"balance"`
}

type GetAllBalancesArgs struct {
	apitypes.JSONAddress
	IncludePartial bool `json:"includePartial"`
}

// GetAllBalancesReply is the response from a call to GetAllBalances
type GetAllBalancesReply struct {
	Balances []Balance `json:"balances"`
}

// GetAllBalances returns a map where:
//
// Key: ID of an asset such that [args.Address] has a non-zero balance of the asset
// Value: The balance of the asset held by the address
//
// If ![args.IncludePartial], returns only unlocked balance/UTXOs with a 1-out-of-1 multisig.
// Otherwise, returned balance/UTXOs includes assets held only partially by the
// address, and includes balances with locktime in the future.
func (s *Service) GetAllBalances(_ *http.Request, args *GetAllBalancesArgs, reply *GetAllBalancesReply) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.vm.log.Debug("deprecated API called",
		log.String("service", "xvm"),
		log.String("method", "getAllBalances"),
		log.String("address", args.Address),
	)

	address, err := lux.ParseServiceAddress(s.vm, args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address '%s': %w", args.Address, err)
	}
	addrSet := set.Of(address)

	s.vm.Lock.Lock()
	defer s.vm.Lock.Unlock()

	utxos, err := lux.GetAllUTXOs(s.vm.state, addrSet)
	if err != nil {
		return fmt.Errorf("couldn't get address's UTXOs: %w", err)
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
		if !args.IncludePartial && (len(owners.Addrs) != 1 || owners.Locktime > now) {
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

	reply.Balances = make([]Balance, assetIDs.Len())
	i := 0
	for assetID := range assetIDs {
		alias := s.vm.PrimaryAliasOrDefault(assetID)
		reply.Balances[i] = Balance{
			AssetID: alias,
			Balance: avajson.Uint64(balances[assetID]),
		}
		i++
	}

	return nil
}
