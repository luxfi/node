// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"
	"github.com/luxfi/log"

	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	safemath "github.com/luxfi/math"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/vm/types"

	avajson "github.com/luxfi/node/utils/json"
	platformapitypes "github.com/luxfi/node/vms/platformvm/api"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of addresses that can be passed in as argument to GetStake
	maxGetStakeAddrs = 256

	// Max number of items allowed in a page
	maxPageSize = 1024

	// Note: Staker attributes cache should be large enough so that no evictions
	// happen when the API loops through all stakers.
	stakerAttributesCacheSize = 100_000
)

var (
	errMissingDecisionBlock    = errors.New("should have a decision block within the past two blocks")
	errPrimaryNetworkIsNotANet = errors.New("the primary network isn't a net")
	errNoAddresses             = errors.New("no addresses provided")
	errMissingBlockchainID     = errors.New("argument 'blockchainID' not given")
)

// Service defines the API calls that can be made to the platform chain
type Service struct {
	vm                    *VM
	addrManager           lux.AddressManager
	stakerAttributesCache *lru.Cache[ids.ID, *stakerAttributes]
}

// All attributes are optional and may not be filled for each stakerTx.
type stakerAttributes struct {
	shares                 uint32
	rewardsOwner           fx.Owner
	validationRewardsOwner fx.Owner
	delegationRewardsOwner fx.Owner
	proofOfPossession      *signer.ProofOfPossession
}

// getHeight returns the height of the last accepted block.
func (s *Service) getHeight(ctx context.Context, _ *struct{}) (*apitypes.GetHeightResponse, error) {
	response := &apitypes.GetHeightResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getHeight",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	height, err := s.vm.GetCurrentHeight(ctx)
	response.Height = apitypes.Uint64(height)
	return nil, err
}

// getProposedHeight returns the height the next proposal will be built at.
func (s *Service) getProposedHeight(ctx context.Context, _ *struct{}) (*apitypes.GetHeightResponse, error) {
	reply := &apitypes.GetHeightResponse{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getProposedHeight"),
	)
	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	lastAcceptedID := s.vm.state.GetLastAccepted()
	lastAcceptedBlock, err := s.vm.manager.GetStatelessBlock(lastAcceptedID)
	if err != nil {
		return nil, err
	}
	reply.Height = apitypes.Uint64(lastAcceptedBlock.Height())
	return reply, nil
}

type GetBalanceRequest struct {
	Addresses []string `json:"addresses"`
}

// Note: We explicitly duplicate LUX out of the maps to ensure backwards
// compatibility.
type GetBalanceResponse struct {
	// Balance, in µLUX, of the address
	Balance             avajson.Uint64 `json:"balance"`
	Unlocked            avajson.Uint64 `json:"unlocked"`
	LockedStakeable     avajson.Uint64 `json:"lockedStakeable"`
	LockedNotStakeable  avajson.Uint64 `json:"lockedNotStakeable"`
	Balances            Amounts        `json:"balances"`
	Unlockeds           Amounts        `json:"unlockeds"`
	LockedStakeables    Amounts        `json:"lockedStakeables"`
	LockedNotStakeables Amounts        `json:"lockedNotStakeables"`
	UTXOIDs             []*lux.UTXOID  `json:"utxoIDs"`
}

// getBalance returns what a set of addresses holds, per asset and in total.
//
// Deprecated: read the UTXOs with getUTXOs and add them up. The totals here are
// a walk of every UTXO an address owns, which is unbounded work for a caller
// that usually wants one asset.
func (s *Service) getBalance(ctx context.Context, args *GetBalanceRequest) (*GetBalanceResponse, error) {
	response := &GetBalanceResponse{}
	s.vm.log.Debug("deprecated API called",
		"service", "platform",
		"method", "getBalance",
		"addresses", args.Addresses,
	)

	addrs, err := lux.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return nil, err
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	utxos, err := lux.GetAllUTXOs(s.vm.state, addrs)
	if err != nil {
		return nil, fmt.Errorf("couldn't get UTXO set of %v: %w", args.Addresses, err)
	}

	currentTime := s.vm.nodeClock.Unix()

	unlockeds := map[ids.ID]uint64{}
	lockedStakeables := map[ids.ID]uint64{}
	lockedNotStakeables := map[ids.ID]uint64{}

utxoFor:
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		switch out := utxo.Out.(type) {
		case *secp256k1fx.TransferOutput:
			if out.Locktime <= currentTime {
				newBalance, err := safemath.Add(unlockeds[assetID], out.Amount())
				if err != nil {
					unlockeds[assetID] = math.MaxUint64
				} else {
					unlockeds[assetID] = newBalance
				}
			} else {
				newBalance, err := safemath.Add(lockedNotStakeables[assetID], out.Amount())
				if err != nil {
					lockedNotStakeables[assetID] = math.MaxUint64
				} else {
					lockedNotStakeables[assetID] = newBalance
				}
			}
		case *stakeable.LockOut:
			innerOut, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
			switch {
			case !ok:
				s.vm.log.Warn("unexpected output type in UTXO",
					"type", fmt.Sprintf("%T", out.TransferableOut),
				)
				continue utxoFor
			case innerOut.Locktime > currentTime:
				newBalance, err := safemath.Add(lockedNotStakeables[assetID], out.Amount())
				if err != nil {
					lockedNotStakeables[assetID] = math.MaxUint64
				} else {
					lockedNotStakeables[assetID] = newBalance
				}
			case out.Locktime <= currentTime:
				newBalance, err := safemath.Add(unlockeds[assetID], out.Amount())
				if err != nil {
					unlockeds[assetID] = math.MaxUint64
				} else {
					unlockeds[assetID] = newBalance
				}
			default:
				newBalance, err := safemath.Add(lockedStakeables[assetID], out.Amount())
				if err != nil {
					lockedStakeables[assetID] = math.MaxUint64
				} else {
					lockedStakeables[assetID] = newBalance
				}
			}
		default:
			continue utxoFor
		}

		response.UTXOIDs = append(response.UTXOIDs, &utxo.UTXOID)
	}

	balances := maps.Clone(lockedStakeables)
	for assetID, amount := range lockedNotStakeables {
		newBalance, err := safemath.Add(balances[assetID], amount)
		if err != nil {
			balances[assetID] = math.MaxUint64
		} else {
			balances[assetID] = newBalance
		}
	}
	for assetID, amount := range unlockeds {
		newBalance, err := safemath.Add(balances[assetID], amount)
		if err != nil {
			balances[assetID] = math.MaxUint64
		} else {
			balances[assetID] = newBalance
		}
	}

	response.Balances = newAmounts(balances)
	response.Unlockeds = newAmounts(unlockeds)
	response.LockedStakeables = newAmounts(lockedStakeables)
	response.LockedNotStakeables = newAmounts(lockedNotStakeables)
	response.Balance = avajson.Uint64(balances[s.vm.utxoAssetID])
	response.Unlocked = avajson.Uint64(unlockeds[s.vm.utxoAssetID])
	response.LockedStakeable = avajson.Uint64(lockedStakeables[s.vm.utxoAssetID])
	response.LockedNotStakeable = avajson.Uint64(lockedNotStakeables[s.vm.utxoAssetID])
	return response, nil
}

// Index is an address and an associated UTXO.
// Marks a starting or stopping point when fetching UTXOs. Used for pagination.
type Index struct {
	Address string `json:"address"` // The address as a string
	UTXO    string `json:"utxo"`    // The UTXO ID as a string
}

// getUTXOs returns one page of the UTXOs a set of addresses owns.
//
// A page ends at a cursor, and the next page starts from it: pass the endIndex
// of one answer as the startIndex of the next. The same UTXO may appear in two
// pages if the set changes underneath the walk.
func (s *Service) getUTXOs(ctx context.Context, args *apitypes.GetUTXOsArgs) (*apitypes.GetUTXOsReply, error) {
	response := &apitypes.GetUTXOsReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getUTXOs",
	)

	if len(args.Addresses) == 0 {
		return nil, errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return nil, fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	var sourceChain ids.ID
	if args.SourceChain == "" {
		sourceChain = s.vm.chainID
	} else {
		// Try to parse as ID first
		chainID, err := ids.FromString(args.SourceChain)
		if err != nil {
			// If not a valid ID, try as an alias
			// Note: bcLookup doesn't have Lookup method, would need reverse lookup
			// For now, just return error
			return nil, fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet, err := lux.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return nil, err
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = lux.ParseServiceAddress(s.addrManager, args.StartIndex.Address)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	var (
		utxos     []*lux.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
	)
	limit := int(args.Limit)
	if limit <= 0 || maxPageSize < limit {
		limit = maxPageSize
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	if sourceChain == s.vm.chainID {
		utxos, endAddr, endUTXOID, err = lux.GetPaginatedUTXOs(
			s.vm.state,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		// For now, return empty results when shared memory is used
		utxos = []*lux.UTXO{}
		endAddr = ids.ShortEmpty
		endUTXOID = ids.Empty
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	response.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		bytes, err := utxo.WireBytes()
		if err != nil {
			return nil, fmt.Errorf("couldn't serialize UTXO %q: %w", utxo.InputID(), err)
		}
		response.UTXOs[i], err = formatting.Encode(args.Encoding, bytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't encode UTXO %s as %s: %w", utxo.InputID(), args.Encoding, err)
		}
	}

	endAddress, err := s.addrManager.FormatLocalAddress(endAddr)
	if err != nil {
		return nil, fmt.Errorf("problem formatting address: %w", err)
	}

	response.EndIndex.Address = endAddress
	response.EndIndex.UTXO = endUTXOID.String()
	response.NumFetched = apitypes.Uint64(len(utxos))
	response.Encoding = args.Encoding
	return response, nil
}

// GetNetArgs are the arguments to GetNet
type GetNetArgs struct {
	// ID of the net to retrieve information about
	ChainID ids.ID `json:"netID"`
}

// GetNetResponse is the response from calling GetNet
type GetNetResponse struct {
	// whether it is permissioned or not
	IsPermissioned bool `json:"isPermissioned"`
	// net auth information for a permissioned net
	ControlKeys []string       `json:"controlKeys"`
	Threshold   avajson.Uint32 `json:"threshold"`
	Locktime    avajson.Uint64 `json:"locktime"`
	// net transformation tx ID for an elastic net
	NetTransformationTxID ids.ID `json:"netTransformationTxID"`
	// net conversion information for an L1
	ConversionID   ids.ID              `json:"conversionID"`
	ManagerChainID ids.ID              `json:"managerChainID"`
	ManagerAddress types.JSONByteSlice `json:"managerAddress"`
}

// getNet returns a net's ownership and, if it has been converted to an L1, the
// chain and address that manage its validators.
func (s *Service) getNet(ctx context.Context, args *GetNetArgs) (*GetNetResponse, error) {
	response := &GetNetResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getNet",
		"netID", args.ChainID,
	)

	if args.ChainID == constants.PrimaryNetworkID {
		return nil, errPrimaryNetworkIsNotANet
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	netOwner, err := s.vm.state.GetNetOwner(args.ChainID)
	if err != nil {
		return nil, err
	}
	owner, ok := netOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", netOwner)
	}
	controlAddrs := make([]string, len(owner.Addrs))
	for i, controlKeyID := range owner.Addrs {
		addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
		if err != nil {
			return nil, fmt.Errorf("problem formatting address: %w", err)
		}
		controlAddrs[i] = addr
	}

	response.ControlKeys = controlAddrs
	response.Threshold = avajson.Uint32(owner.Threshold)
	response.Locktime = avajson.Uint64(owner.Locktime)

	switch netTransformationTx, err := s.vm.state.GetNetTransformation(args.ChainID); err {
	case nil:
		response.IsPermissioned = false
		response.NetTransformationTxID = netTransformationTx.ID()
	case database.ErrNotFound:
		response.IsPermissioned = true
		response.NetTransformationTxID = ids.Empty
	default:
		return nil, err
	}

	switch c, err := s.vm.state.GetNetToL1Conversion(args.ChainID); err {
	case nil:
		response.IsPermissioned = false
		response.ConversionID = c.ConversionID
		response.ManagerChainID = c.ChainID
		response.ManagerAddress = c.Addr
	case database.ErrNotFound:
		response.ConversionID = ids.Empty
		response.ManagerChainID = ids.Empty
		response.ManagerAddress = []byte(nil)
	default:
		return nil, err
	}

	return response, nil
}

// APINet is a representation of a net used in API calls
type APINet struct {
	// ID of the net
	ID ids.ID `json:"id"`

	// Each element of [ControlKeys] the address of a public key.
	// A transaction to add a validator to this net requires
	// signatures from [Threshold] of these keys to be valid.
	ControlKeys []string       `json:"controlKeys"`
	Threshold   avajson.Uint32 `json:"threshold"`
}

// GetNetsArgs are the arguments to GetNets
type GetNetsArgs struct {
	// IDs of the nets to retrieve information about
	// If omitted, gets all nets
	IDs []ids.ID `json:"ids"`
}

// GetNetsResponse is the response from calling GetNets
type GetNetsResponse struct {
	// Each element is a net that exists
	// Null if there are no nets other than the primary network
	Nets []APINet `json:"nets"`
}

// getNets returns the nets named, or every net when none is named. The primary
// network is always among them.
//
// Deprecated: use getChains, which answers the same thing under the name the
// concept now has.
func (s *Service) getNets(ctx context.Context, args *GetNetsArgs) (*GetNetsResponse, error) {
	response := &GetNetsResponse{}
	s.vm.log.Debug("deprecated API called",
		"service", "platform",
		"method", "getNets",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	getAll := len(args.IDs) == 0
	if getAll {
		netIDs, err := s.vm.state.GetChainIDs() // all nets
		if err != nil {
			return nil, fmt.Errorf("error getting nets from database: %w", err)
		}

		response.Nets = make([]APINet, len(netIDs)+1)
		for i, netID := range netIDs {
			if _, err := s.vm.state.GetNetTransformation(netID); err == nil {
				response.Nets[i] = APINet{
					ID:          netID,
					ControlKeys: []string{},
					Threshold:   avajson.Uint32(0),
				}
				continue
			}

			netOwner, err := s.vm.state.GetNetOwner(netID)
			if err != nil {
				return nil, err
			}

			owner, ok := netOwner.(*secp256k1fx.OutputOwners)
			if !ok {
				return nil, fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", netOwner)
			}

			controlAddrs := make([]string, len(owner.Addrs))
			for i, controlKeyID := range owner.Addrs {
				addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
				if err != nil {
					return nil, fmt.Errorf("problem formatting address: %w", err)
				}
				controlAddrs[i] = addr
			}
			response.Nets[i] = APINet{
				ID:          netID,
				ControlKeys: controlAddrs,
				Threshold:   avajson.Uint32(owner.Threshold),
			}
		}
		// Include primary network
		response.Nets[len(netIDs)] = APINet{
			ID:          constants.PrimaryNetworkID,
			ControlKeys: []string{},
			Threshold:   avajson.Uint32(0),
		}
		return response, nil
	}

	netSet := set.NewSet[ids.ID](len(args.IDs))
	for _, netID := range args.IDs {
		if netSet.Contains(netID) {
			continue
		}
		netSet.Add(netID)

		if netID == constants.PrimaryNetworkID {
			response.Nets = append(response.Nets,
				APINet{
					ID:          constants.PrimaryNetworkID,
					ControlKeys: []string{},
					Threshold:   avajson.Uint32(0),
				},
			)
			continue
		}

		if _, err := s.vm.state.GetNetTransformation(netID); err == nil {
			response.Nets = append(response.Nets, APINet{
				ID:          netID,
				ControlKeys: []string{},
				Threshold:   avajson.Uint32(0),
			})
			continue
		}

		netOwner, err := s.vm.state.GetNetOwner(netID)
		if err == database.ErrNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}

		owner, ok := netOwner.(*secp256k1fx.OutputOwners)
		if !ok {
			return nil, fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", netOwner)
		}

		controlAddrs := make([]string, len(owner.Addrs))
		for i, controlKeyID := range owner.Addrs {
			addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
			if err != nil {
				return nil, fmt.Errorf("problem formatting address: %w", err)
			}
			controlAddrs[i] = addr
		}

		response.Nets = append(response.Nets, APINet{
			ID:          netID,
			ControlKeys: controlAddrs,
			Threshold:   avajson.Uint32(owner.Threshold),
		})
	}
	return response, nil
}

// APIChain is the canonical wire-shape for a chain registered on the
// platform via CreateNetworkTx. Replaces APINet — same fields, named
// after the user-facing concept ("chain") rather than the internal
// canonical naming. The wire encoding is byte-identical so a
// deserializer that targets APIChain reads APINet responses and vice
// versa; no proxies need to translate.
type APIChain struct {
	ID          ids.ID         `json:"id"`
	ControlKeys []string       `json:"controlKeys"`
	Threshold   avajson.Uint32 `json:"threshold"`
}

// GetChainsArgs are the arguments to GetChains. IDs is the optional
// allowlist filter — empty list returns every chain (including the
// primary network). Same shape as GetNetsArgs.
type GetChainsArgs struct {
	IDs []ids.ID `json:"ids"`
}

// GetChainsResponse is the response from GetChains. Same shape as
// GetNetsResponse — `Chains` instead of `Nets`. Field names are
// canonical going forward; downstream parsers should target this
// struct.
type GetChainsResponse struct {
	Chains []APIChain `json:"chains"`
}

// getChains returns the chains named, or every chain when none is named. The
// primary network is always among them.
func (s *Service) getChains(ctx context.Context, args *GetChainsArgs) (*GetChainsResponse, error) {
	response := &GetChainsResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getChains",
	)
	// Delegate to the existing implementation by translating shapes —
	// avoids re-implementing the state walk and keeps the two methods
	// in lock-step. APIChain is structurally identical to APINet (same
	// JSON tags), so the cast is safe and free at runtime.
	nets, err := s.getNets(ctx, &GetNetsArgs{IDs: args.IDs})
	if err != nil {
		return nil, err
	}
	response.Chains = make([]APIChain, len(nets.Nets))
	for i, n := range nets.Nets {
		response.Chains[i] = APIChain(n)
	}
	return response, nil
}

// GetStakingAssetIDArgs are the arguments to GetStakingAssetID
type GetStakingAssetIDArgs struct {
	ChainID ids.ID `json:"netID"`
}

// GetStakingAssetIDResponse is the response from calling GetStakingAssetID
type GetStakingAssetIDResponse struct {
	AssetID ids.ID `json:"assetID"`
}

// getStakingAssetID returns the asset a net is staked in.
func (s *Service) getStakingAssetID(ctx context.Context, args *GetStakingAssetIDArgs) (*GetStakingAssetIDResponse, error) {
	response := &GetStakingAssetIDResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getStakingAssetID",
	)

	if args.ChainID == constants.PrimaryNetworkID {
		response.AssetID = s.vm.utxoAssetID
		return response, nil
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	transformNetIntf, err := s.vm.state.GetNetTransformation(args.ChainID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed fetching net transformation for %s: %w",
			args.ChainID,
			err,
		)
	}
	transformNet, ok := transformNetIntf.Unsigned.(*txs.TransformChainTx)
	if !ok {
		return nil, fmt.Errorf(
			"unexpected net transformation tx type fetched %T",
			transformNetIntf.Unsigned,
		)
	}

	response.AssetID = transformNet.AssetID()
	return response, nil
}

// GetCurrentValidatorsArgs are the arguments for calling GetCurrentValidators
type GetCurrentValidatorsArgs struct {
	// Net we're listing the validators of
	// If omitted, defaults to primary network
	ChainID ids.ID `json:"netID"`
	// NodeIDs of validators to request. If [NodeIDs]
	// is empty, it fetches all current validators. If
	// some nodeIDs are not currently validators, they
	// will be omitted from the response.
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

// GetCurrentValidatorsReply are the results from calling GetCurrentValidators.
// Each validator contains a list of delegators to itself.
type GetCurrentValidatorsReply struct {
	Validators []CurrentValidator `json:"validators"`
}

func (s *Service) loadStakerTxAttributes(txID ids.ID) (*stakerAttributes, error) {
	// Lookup tx from the cache first.
	attr, found := s.stakerAttributesCache.Get(txID)
	if found {
		return attr, nil
	}

	// Tx not available in cache; pull it from disk and populate the cache.
	tx, _, err := s.vm.state.GetTx(txID)
	if err != nil {
		return nil, err
	}

	switch stakerTx := tx.Unsigned.(type) {
	case txs.ValidatorTx:
		var pop *signer.ProofOfPossession
		if staker, ok := stakerTx.(*txs.AddPermissionlessValidatorTx); ok {
			if s, ok := staker.Signer().(*signer.ProofOfPossession); ok {
				pop = s
			}
		}

		attr = &stakerAttributes{
			shares:                 stakerTx.Shares(),
			validationRewardsOwner: stakerTx.ValidationRewardsOwner(),
			delegationRewardsOwner: stakerTx.DelegationRewardsOwner(),
			proofOfPossession:      pop,
		}

	case txs.DelegatorTx:
		attr = &stakerAttributes{
			rewardsOwner: stakerTx.RewardsOwner(),
		}

	default:
		return nil, fmt.Errorf("unexpected staker tx type %T", tx.Unsigned)
	}

	s.stakerAttributesCache.Put(txID, attr)
	return attr, nil
}

// getCurrentValidators returns a net's current validators.
//
// Naming nodes narrows the answer to those nodes and adds each one's delegators
// in full; naming one node is how a caller reads a single validator's
// delegation. Naming none returns every validator with its delegators counted
// and weighed but not listed.
func (s *Service) getCurrentValidators(ctx context.Context, args *GetCurrentValidatorsArgs) (*GetCurrentValidatorsReply, error) {
	reply := &GetCurrentValidatorsReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getCurrentValidators"),
	)

	// Create set of nodeIDs
	nodeIDs := set.Of(args.NodeIDs...)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	// Check if net is L1
	_, err := s.vm.state.GetNetToL1Conversion(args.ChainID)
	if errors.Is(err, database.ErrNotFound) {
		// Net is not L1, get validators for the net
		reply.Validators, err = s.getPrimaryOrNetValidators(
			args.ChainID,
			nodeIDs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get primary or net validators: %w", err)
		}
		return reply, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get net to L1 conversion: %w", err)
	}

	// Net is L1, get validators for L1
	reply.Validators, err = s.getL1Validators(
		ctx,
		args.ChainID,
		nodeIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get L1 validators: %w", err)
	}
	return reply, nil
}

func (s *Service) getL1Validators(
	ctx context.Context,
	netID ids.ID,
	nodeIDs set.Set[ids.NodeID],
) ([]CurrentValidator, error) {
	current := []CurrentValidator{}
	baseStakers, l1Validators, _, err := s.vm.state.GetCurrentValidators(ctx, netID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current validators: %w", err)
	}

	fetchAll := nodeIDs.Len() == 0

	for _, staker := range baseStakers {
		if !fetchAll && !nodeIDs.Contains(staker.NodeID) {
			continue
		}

		apiStaker := toPlatformStaker(staker)
		current = append(current, CurrentValidator{Permissioned: &apiStaker})
	}

	for _, l1Validator := range l1Validators {
		if !fetchAll && !nodeIDs.Contains(l1Validator.NodeID) {
			continue
		}

		apiL1Vdr, err := s.convertL1ValidatorToAPI(l1Validator)
		if err != nil {
			return nil, fmt.Errorf("converting L1 validator to API format: %w", err)
		}

		current = append(current, CurrentValidator{L1: &apiL1Vdr})
	}

	return current, nil
}

func (s *Service) getPrimaryOrNetValidators(netID ids.ID, nodeIDs set.Set[ids.NodeID]) ([]CurrentValidator, error) {
	numNodeIDs := nodeIDs.Len()

	targetStakers := make([]*state.Staker, 0, numNodeIDs)

	// Validator's node ID as string --> Delegators to them
	vdrToDelegators := map[ids.NodeID][]platformapitypes.PrimaryDelegator{}

	current := []CurrentValidator{}

	if numNodeIDs == 0 { // Include all nodes
		currentStakerIterator, err := s.vm.state.GetCurrentStakerIterator()
		if err != nil {
			return nil, err
		}
		for currentStakerIterator.Next() {
			staker := currentStakerIterator.Value()
			if netID != staker.ChainID {
				continue
			}
			targetStakers = append(targetStakers, staker)
		}
		currentStakerIterator.Release()
	} else {
		for nodeID := range nodeIDs {
			staker, err := s.vm.state.GetCurrentValidator(netID, nodeID)
			switch err {
			case nil:
			case database.ErrNotFound:
				// nothing to do, continue
				continue
			default:
				return nil, err
			}
			targetStakers = append(targetStakers, staker)

			// Delegator iteration happens per-nodeID; acceptable for small numNodeIDs.
			delegatorsIt, err := s.vm.state.GetCurrentDelegatorIterator(netID, nodeID)
			if err != nil {
				return nil, err
			}
			for delegatorsIt.Next() {
				staker := delegatorsIt.Value()
				targetStakers = append(targetStakers, staker)
			}
			delegatorsIt.Release()
		}
	}

	for _, currentStaker := range targetStakers {
		apiStaker := toPlatformStaker(currentStaker)
		potentialReward := avajson.Uint64(currentStaker.PotentialReward)

		delegateeReward, err := s.vm.state.GetDelegateeReward(currentStaker.ChainID, currentStaker.NodeID)
		if err != nil {
			return nil, err
		}
		jsonDelegateeReward := avajson.Uint64(delegateeReward)

		switch currentStaker.Priority {
		case txs.PrimaryNetworkValidatorCurrentPriority, txs.ChainPermissionlessValidatorCurrentPriority:
			attr, err := s.loadStakerTxAttributes(currentStaker.TxID)
			if err != nil {
				return nil, err
			}

			shares := attr.shares
			delegationFee := avajson.Float32(100 * float32(shares) / float32(reward.PercentDenominator))
			var (
				uptime    *avajson.Float32
				connected *bool
			)
			if netID == constants.PrimaryNetworkID {
				rawUptime, err := s.vm.uptimeManager.CalculateUptimePercentFrom(currentStaker.NodeID, netID, currentStaker.StartTime)
				if err != nil {
					return nil, err
				}
				// Transform this to a percentage (0-100) to make it consistent
				// with observedUptime in info.peers API
				currentUptime := avajson.Float32(rawUptime * 100)
				uptime = &currentUptime

				// Report whether this validator currently has a live connection
				// to us, read from the same tracker that measured its uptime.
				isConnected := s.vm.tracker != nil && s.vm.tracker.IsConnected(currentStaker.NodeID)
				connected = &isConnected
			}

			var (
				validationRewardOwner *platformapitypes.Owner
				delegationRewardOwner *platformapitypes.Owner
			)
			validationOwner, ok := attr.validationRewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				validationRewardOwner, err = s.getAPIOwner(validationOwner)
				if err != nil {
					return nil, err
				}
			}
			delegationOwner, ok := attr.delegationRewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				delegationRewardOwner, err = s.getAPIOwner(delegationOwner)
				if err != nil {
					return nil, err
				}
			}

			vdr := platformapitypes.PermissionlessValidator{
				Staker:                 apiStaker,
				Uptime:                 uptime,
				Connected:              connected,
				PotentialReward:        &potentialReward,
				AccruedDelegateeReward: &jsonDelegateeReward,
				ValidationRewardOwner:  validationRewardOwner,
				DelegationRewardOwner:  delegationRewardOwner,
				DelegationFee:          delegationFee,
				Signer:                 attr.proofOfPossession,
			}
			current = append(current, CurrentValidator{Permissionless: &vdr})

		case txs.PrimaryNetworkDelegatorCurrentPriority, txs.ChainPermissionlessDelegatorCurrentPriority:
			var rewardOwner *platformapitypes.Owner
			// If we are handling multiple nodeIDs, we don't return the
			// delegator information.
			if numNodeIDs == 1 {
				attr, err := s.loadStakerTxAttributes(currentStaker.TxID)
				if err != nil {
					return nil, err
				}
				owner, ok := attr.rewardsOwner.(*secp256k1fx.OutputOwners)
				if ok {
					rewardOwner, err = s.getAPIOwner(owner)
					if err != nil {
						return nil, err
					}
				}
			}

			delegator := platformapitypes.PrimaryDelegator{
				Staker:          apiStaker,
				RewardOwner:     rewardOwner,
				PotentialReward: &potentialReward,
			}
			vdrToDelegators[delegator.NodeID] = append(vdrToDelegators[delegator.NodeID], delegator)

		case txs.ChainPermissionedValidatorCurrentPriority:
			staker := apiStaker
			current = append(current, CurrentValidator{Permissioned: &staker})

		default:
			return nil, fmt.Errorf("unexpected staker priority %d", currentStaker.Priority)
		}
	}

	// handle delegators' information
	for _, entry := range current {
		vdr := entry.Permissionless
		if vdr == nil {
			continue
		}
		delegators, ok := vdrToDelegators[vdr.NodeID]
		if !ok {
			// If we are expected to populate the delegators field, we should
			// always return a non-nil value.
			delegators = []platformapitypes.PrimaryDelegator{}
		}
		delegatorCount := avajson.Uint64(len(delegators))
		delegatorWeight := avajson.Uint64(0)
		for _, d := range delegators {
			delegatorWeight += d.Weight
		}

		vdr.DelegatorCount = &delegatorCount
		vdr.DelegatorWeight = &delegatorWeight

		if numNodeIDs == 1 {
			// queried a specific validator, load all of its delegators
			vdr.Delegators = &delegators
		}
	}

	return current, nil
}

type GetL1ValidatorArgs struct {
	ValidationID ids.ID `json:"validationID"`
}

type GetL1ValidatorReply struct {
	platformapitypes.APIL1Validator
	ChainID ids.ID `json:"netID"`
	// Height is the height of the last accepted block
	Height avajson.Uint64 `json:"height"`
}

// getL1Validator returns one L1 validator by its validation id, with the
// balance left to pay for its continued validation.
func (s *Service) getL1Validator(ctx context.Context, args *GetL1ValidatorArgs) (*GetL1ValidatorReply, error) {
	reply := &GetL1ValidatorReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getL1Validator"),
		log.Stringer("validationID", args.ValidationID),
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	l1Validator, err := s.vm.state.GetL1Validator(args.ValidationID)
	if err != nil {
		return nil, fmt.Errorf("fetching L1 validator %q failed: %w", args.ValidationID, err)
	}

	height, err := s.vm.GetCurrentHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the current height: %w", err)
	}
	apiVdr, err := s.convertL1ValidatorToAPI(l1Validator)
	if err != nil {
		return nil, fmt.Errorf("failed to convert L1 validator to API format: %w", err)
	}

	reply.APIL1Validator = apiVdr
	reply.ChainID = l1Validator.ChainID
	reply.Height = avajson.Uint64(height)
	return reply, nil
}

func (s *Service) convertL1ValidatorToAPI(vdr state.L1Validator) (platformapitypes.APIL1Validator, error) {
	remBalOwner, err := txs.UnmarshalOwner(vdr.RemainingBalanceOwner)
	if err != nil {
		return platformapitypes.APIL1Validator{}, fmt.Errorf("failed unmarshalling remaining balance owner: %w", err)
	}
	remainingBalanceOwner := message.PChainOwner{Threshold: remBalOwner.Threshold, Addresses: remBalOwner.Addrs}
	remainingBalanceAPIOwner, err := s.getAPIOwner(&secp256k1fx.OutputOwners{
		Threshold: remainingBalanceOwner.Threshold,
		Addrs:     remainingBalanceOwner.Addresses,
	})
	if err != nil {
		return platformapitypes.APIL1Validator{}, fmt.Errorf("failed formatting remaining balance owner: %w", err)
	}

	deacOwner, err := txs.UnmarshalOwner(vdr.DeactivationOwner)
	if err != nil {
		return platformapitypes.APIL1Validator{}, fmt.Errorf("failed unmarshalling deactivation owner: %w", err)
	}
	deactivationOwner := message.PChainOwner{Threshold: deacOwner.Threshold, Addresses: deacOwner.Addrs}
	deactivationAPIOwner, err := s.getAPIOwner(&secp256k1fx.OutputOwners{
		Threshold: deactivationOwner.Threshold,
		Addrs:     deactivationOwner.Addresses,
	})
	if err != nil {
		return platformapitypes.APIL1Validator{}, fmt.Errorf("failed formatting deactivation owner: %w", err)
	}

	pubKey := types.JSONByteSlice(bls.PublicKeyToCompressedBytes(
		bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey),
	))
	minNonce := avajson.Uint64(vdr.MinNonce)

	apiVdr := platformapitypes.APIL1Validator{
		NodeID:    vdr.NodeID,
		StartTime: avajson.Uint64(vdr.StartTime),
		Weight:    avajson.Uint64(vdr.Weight),
		BaseL1Validator: platformapitypes.BaseL1Validator{
			ValidationID:          &vdr.ValidationID,
			PublicKey:             &pubKey,
			RemainingBalanceOwner: remainingBalanceAPIOwner,
			DeactivationOwner:     deactivationAPIOwner,
			MinNonce:              &minNonce,
		},
	}
	zero := avajson.Uint64(0)
	apiVdr.Balance = &zero
	if vdr.EndAccumulatedFee != 0 {
		accruedFees := s.vm.state.GetAccruedFees()
		balance := avajson.Uint64(vdr.EndAccumulatedFee - accruedFees)
		apiVdr.Balance = &balance
	}
	return apiVdr, nil
}

// GetCurrentSupplyArgs are the arguments for calling GetCurrentSupply
type GetCurrentSupplyArgs struct {
	ChainID ids.ID `json:"netID"`
}

// GetCurrentSupplyReply are the results from calling GetCurrentSupply
type GetCurrentSupplyReply struct {
	Supply avajson.Uint64 `json:"supply"`
	Height avajson.Uint64 `json:"height"`
}

// getCurrentSupply returns an upper bound on the supply of LUX on a net, and
// the height it was read at.
func (s *Service) getCurrentSupply(ctx context.Context, args *GetCurrentSupplyArgs) (*GetCurrentSupplyReply, error) {
	reply := &GetCurrentSupplyReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getCurrentSupply",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	supply, err := s.vm.state.GetCurrentSupply(args.ChainID)
	if err != nil {
		return nil, fmt.Errorf("fetching current supply failed: %w", err)
	}
	reply.Supply = avajson.Uint64(supply)

	height, err := s.vm.GetCurrentHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching current height failed: %w", err)
	}
	reply.Height = avajson.Uint64(height)

	return reply, nil
}

// GetBlockchainStatusArgs is the arguments for calling GetBlockchainStatus
// [BlockchainID] is the ID of or an alias of the blockchain to get the status of.
type GetBlockchainStatusArgs struct {
	BlockchainID string `json:"blockchainID"`
}

// GetBlockchainStatusReply is the reply from calling GetBlockchainStatus
// [Status] is the blockchain's status.
type GetBlockchainStatusReply struct {
	Status status.BlockchainStatus `json:"status"`
}

// getBlockchainStatus returns how far along a blockchain is: whether this node
// validates it, whether it has been created, and whether it is preferred.
func (s *Service) getBlockchainStatus(ctx context.Context, args *GetBlockchainStatusArgs) (*GetBlockchainStatusReply, error) {
	reply := &GetBlockchainStatusReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getBlockchainStatus",
	)

	if args.BlockchainID == "" {
		return nil, errMissingBlockchainID
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	// if its aliased then vm created this chain.
	if aliasedID, err := s.vm.Chains.Lookup(args.BlockchainID); err == nil {
		if s.nodeValidates(aliasedID) {
			reply.Status = status.Validating
			return reply, nil
		}

		reply.Status = status.Syncing
		return reply, nil
	}

	blockchainID, err := ids.FromString(args.BlockchainID)
	if err != nil {
		return nil, fmt.Errorf("problem parsing blockchainID %q: %w", args.BlockchainID, err)
	}

	lastAcceptedID, err := s.vm.LastAccepted(ctx)
	if err != nil {
		return nil, fmt.Errorf("problem loading last accepted ID: %w", err)
	}

	exists, err := s.chainExists(ctx, lastAcceptedID, blockchainID)
	if err != nil {
		return nil, fmt.Errorf("problem looking up blockchain: %w", err)
	}
	if exists {
		reply.Status = status.Created
		return reply, nil
	}

	preferredBlkID := s.vm.manager.Preferred()
	preferred, err := s.chainExists(ctx, preferredBlkID, blockchainID)
	if err != nil {
		return nil, fmt.Errorf("problem looking up blockchain: %w", err)
	}
	if preferred {
		reply.Status = status.Preferred
	} else {
		reply.Status = status.UnknownChain
	}
	return reply, nil
}

func (s *Service) nodeValidates(blockchainID ids.ID) bool {
	chainTx, _, err := s.vm.state.GetTx(blockchainID)
	if err != nil {
		return false
	}

	chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
	if !ok {
		return false
	}

	_, isValidator := s.vm.Validators.GetValidator(chain.ChainID(), s.vm.nodeID)
	return isValidator
}

func (s *Service) chainExists(ctx context.Context, blockID ids.ID, chainID ids.ID) (bool, error) {
	state, ok := s.vm.manager.GetState(blockID)
	if !ok {
		block, err := s.vm.GetBlock(ctx, blockID)
		if err != nil {
			return false, err
		}
		state, ok = s.vm.manager.GetState(block.Parent())
		if !ok {
			return false, errMissingDecisionBlock
		}
	}

	tx, _, err := state.GetTx(chainID)
	if err == database.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, ok = tx.Unsigned.(*txs.CreateChainTx)
	return ok, nil
}

// ValidatedByArgs is the arguments for calling ValidatedBy
type ValidatedByArgs struct {
	// ValidatedBy returns the ID of the Net validating the blockchain with this ID
	BlockchainID ids.ID `json:"blockchainID"`
}

// ValidatedByResponse is the reply from calling ValidatedBy
type ValidatedByResponse struct {
	// ID of the Net validating the specified blockchain
	ChainID ids.ID `json:"netID"`
}

// validatedBy returns the net that validates a blockchain.
func (s *Service) validatedBy(ctx context.Context, args *ValidatedByArgs) (*ValidatedByResponse, error) {
	response := &ValidatedByResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "validatedBy",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	// GetChainID is not available in the current validators.Manager interface
	// Return primary network for now
	response.ChainID = constants.PrimaryNetworkID
	return response, nil
}

// ValidatesArgs are the arguments to Validates
type ValidatesArgs struct {
	ChainID ids.ID `json:"netID"`
}

// ValidatesResponse is the response from calling Validates
type ValidatesResponse struct {
	BlockchainIDs []ids.ID `json:"blockchainIDs"`
}

// validates returns the blockchains a net validates.
func (s *Service) validates(ctx context.Context, args *ValidatesArgs) (*ValidatesResponse, error) {
	response := &ValidatesResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "validates",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	if args.ChainID != constants.PrimaryNetworkID {
		netTx, _, err := s.vm.state.GetTx(args.ChainID)
		if err != nil {
			return nil, fmt.Errorf(
				"problem retrieving net %q: %w",
				args.ChainID,
				err,
			)
		}
		_, ok := netTx.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return nil, fmt.Errorf("%q is not a net", args.ChainID)
		}
	}

	// Get the chains that exist
	chains, err := s.vm.state.GetChains(args.ChainID)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving chains for net %q: %w", args.ChainID, err)
	}

	response.BlockchainIDs = make([]ids.ID, len(chains))
	for i, chain := range chains {
		response.BlockchainIDs[i] = chain.ID()
	}
	return response, nil
}

// APIBlockchain is the representation of a blockchain used in API calls
type APIBlockchain struct {
	// Blockchain's ID
	ID ids.ID `json:"id"`

	// Blockchain's (non-unique) human-readable name
	Name string `json:"name"`

	// Net that validates the blockchain
	ChainID ids.ID `json:"netID"`

	// Virtual Machine the blockchain runs
	VMID ids.ID `json:"vmID"`
}

// GetBlockchainsResponse is the response from a call to GetBlockchains
type GetBlockchainsResponse struct {
	// blockchains that exist
	Blockchains []APIBlockchain `json:"blockchains"`
}

// getBlockchains returns every blockchain that exists, with the net that
// validates it and the VM it runs.
func (s *Service) getBlockchains(ctx context.Context, _ *struct{}) (*GetBlockchainsResponse, error) {
	response := &GetBlockchainsResponse{}
	s.vm.log.Debug("deprecated API called",
		"service", "platform",
		"method", "getBlockchains",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	netIDs, err := s.vm.state.GetChainIDs()
	if err != nil {
		return nil, fmt.Errorf("couldn't retrieve nets: %w", err)
	}

	response.Blockchains = []APIBlockchain{}
	for _, netID := range netIDs {
		chains, err := s.vm.state.GetChains(netID)
		if err != nil {
			return nil, fmt.Errorf(
				"couldn't retrieve chains for net %q: %w",
				netID,
				err,
			)
		}

		for _, chainTx := range chains {
			chainID := chainTx.ID()
			chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
			if !ok {
				return nil, fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chainTx.Unsigned)
			}
			response.Blockchains = append(response.Blockchains, APIBlockchain{
				ID:      chainID,
				Name:    chain.BlockchainName(),
				ChainID: netID,
				VMID:    chain.VMID(),
			})
		}
	}

	chains, err := s.vm.state.GetChains(constants.PrimaryNetworkID)
	if err != nil {
		return nil, fmt.Errorf("couldn't retrieve nets: %w", err)
	}
	for _, chainTx := range chains {
		chainID := chainTx.ID()
		chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return nil, fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chainTx.Unsigned)
		}
		response.Blockchains = append(response.Blockchains, APIBlockchain{
			ID:      chainID,
			Name:    chain.BlockchainName(),
			ChainID: constants.PrimaryNetworkID,
			VMID:    chain.VMID(),
		})
	}

	return response, nil
}

// issueTx hands already-signed transaction bytes to consensus and returns the
// transaction's id.
//
// The node checks no signature here and holds no key that could have made one:
// the bytes carry their own authority, which is why this address answers anyone
// (see [server.Relay]).
func (s *Service) issueTx(ctx context.Context, args *apitypes.FormattedTx) (*apitypes.JSONTxID, error) {
	response := &apitypes.JSONTxID{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "issueTx",
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return nil, fmt.Errorf("problem decoding transaction: %w", err)
	}
	tx, err := txs.Parse(txBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse tx: %w", err)
	}

	if err := s.vm.issueTxFromRPC(tx); err != nil {
		return nil, fmt.Errorf("couldn't issue tx: %w", err)
	}

	response.TxID = tx.ID()
	return response, nil
}

// getTx returns a transaction by its id.
//
// The encoding chooses the shape of the answer: hex returns the signed bytes as
// one string, and json returns the transaction as an object.
func (s *Service) getTx(ctx context.Context, args *apitypes.GetTxArgs) (*apitypes.GetTxReply, error) {
	response := &apitypes.GetTxReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getTx",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	tx, _, err := s.vm.state.GetTx(args.TxID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get tx: %w", err)
	}
	response.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		tx.Unsigned.InitRuntime(s.vm.rt)
		result = tx
	} else {
		result, err = formatting.Encode(args.Encoding, tx.Bytes())
		if err != nil {
			return nil, fmt.Errorf("couldn't encode tx as %s: %w", args.Encoding, err)
		}
	}

	response.Tx, err = json.Marshal(result, jsonv1.FormatByteArrayAsArray(true))
	return nil, err
}

type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
}

type GetTxStatusResponse struct {
	Status status.Status `json:"status"`
	// Reason this tx was dropped.
	// Only non-empty if Status is dropped
	Reason string `json:"reason,omitempty"`
}

// getTxStatus returns where a transaction stands: accepted, still processing,
// dropped with a reason, or unknown to this node.
func (s *Service) getTxStatus(ctx context.Context, args *GetTxStatusArgs) (*GetTxStatusResponse, error) {
	response := &GetTxStatusResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getTxStatus",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	_, txStatus, err := s.vm.state.GetTx(args.TxID)
	if err == nil { // Found the status. Report it.
		response.Status = txStatus
		return response, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	// The status of this transaction is not in the database - check if the tx
	// is in the preferred block's db. If so, return that it's processing.
	preferredID := s.vm.manager.Preferred()
	onAccept, ok := s.vm.manager.GetState(preferredID)
	if !ok {
		// Preferred state may not be cached after recent block acceptance.
		// Fall back to last accepted state.
		lastAccepted := s.vm.manager.LastAccepted()
		onAccept, ok = s.vm.manager.GetState(lastAccepted)
		if !ok {
			return nil, fmt.Errorf("could not retrieve state for block %s", preferredID)
		}
	}

	_, _, err = onAccept.GetTx(args.TxID)
	if err == nil {
		// Found the status in the preferred block's db. Report tx is processing.
		response.Status = status.Processing
		return response, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	if _, ok := s.vm.Builder.Get(args.TxID); ok {
		// Found the tx in the mempool. Report tx is processing.
		response.Status = status.Processing
		return response, nil
	}

	// Note: we check if tx is dropped only after having looked for it
	// in the database and the mempool, because dropped txs may be re-issued.
	reason := s.vm.Builder.GetDropReason(args.TxID)
	if reason == nil {
		// The tx isn't being tracked by the node.
		response.Status = status.Unknown
		return response, nil
	}

	// The tx was recently dropped because it was invalid.
	response.Status = status.Dropped
	response.Reason = reason.Error()
	return response, nil
}

type GetStakeArgs struct {
	apitypes.JSONAddresses
	ValidatorsOnly bool                `json:"validatorsOnly"`
	Encoding       formatting.Encoding `json:"encoding"`
}

// GetStakeReply is the response from calling GetStake.
type GetStakeReply struct {
	Staked  avajson.Uint64 `json:"staked"`
	Stakeds Amounts        `json:"stakeds"`
	// String representation of staked outputs
	// Each is of type lux.TransferableOutput
	Outputs []string `json:"stakedOutputs"`
	// Encoding of [Outputs]
	Encoding formatting.Encoding `json:"encoding"`
}

// getStake returns what a set of addresses has staked on the primary network,
// and the outputs that stake is locked in.
//
// Deprecated: read the stake off the validators with getCurrentValidators, or
// off the staking transaction with getTx. This walks every current and pending
// staker on every call.
func (s *Service) getStake(ctx context.Context, args *GetStakeArgs) (*GetStakeReply, error) {
	response := &GetStakeReply{}
	s.vm.log.Debug("deprecated API called",
		"service", "platform",
		"method", "getStake",
	)

	if len(args.Addresses) > maxGetStakeAddrs {
		return nil, fmt.Errorf("%d addresses provided but this method can take at most %d", len(args.Addresses), maxGetStakeAddrs)
	}

	addrs, err := lux.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return nil, err
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	currentStakerIterator, err := s.vm.state.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}
	defer currentStakerIterator.Release()

	var (
		totalAmountStaked = make(map[ids.ID]uint64)
		stakedOuts        []lux.TransferableOutput
	)
	for currentStakerIterator.Next() { // Iterates over current stakers
		staker := currentStakerIterator.Value()

		if args.ValidatorsOnly && !staker.Priority.IsValidator() {
			continue
		}

		tx, _, err := s.vm.state.GetTx(staker.TxID)
		if err != nil {
			return nil, err
		}

		stakedOuts = append(stakedOuts, getStakeHelper(tx, addrs, totalAmountStaked)...)
	}

	pendingStakerIterator, err := s.vm.state.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}
	defer pendingStakerIterator.Release()

	for pendingStakerIterator.Next() { // Iterates over pending stakers
		staker := pendingStakerIterator.Value()

		if args.ValidatorsOnly && !staker.Priority.IsValidator() {
			continue
		}

		tx, _, err := s.vm.state.GetTx(staker.TxID)
		if err != nil {
			return nil, err
		}

		stakedOuts = append(stakedOuts, getStakeHelper(tx, addrs, totalAmountStaked)...)
	}

	response.Stakeds = newAmounts(totalAmountStaked)
	response.Staked = avajson.Uint64(totalAmountStaked[s.vm.utxoAssetID])
	response.Outputs = make([]string, len(stakedOuts))
	for i, output := range stakedOuts {
		// Surface each staked output as native UTXO wire bytes — the one
		// canonical standalone-output encoding (no codec). The deprecated
		// GetStake client treats each entry as an opaque byte string.
		utxo := &lux.UTXO{
			UTXOID: lux.UTXOID{TxID: ids.Empty, OutputIndex: uint32(i)},
			Asset:  output.Asset,
			Out:    output.Out,
		}
		bytes, err := utxo.WireBytes()
		if err != nil {
			return nil, fmt.Errorf("couldn't serialize output %s: %w", output.ID, err)
		}
		response.Outputs[i], err = formatting.Encode(args.Encoding, bytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't encode output %s as %s: %w", output.ID, args.Encoding, err)
		}
	}
	response.Encoding = args.Encoding

	return response, nil
}

// GetMinStakeArgs are the arguments for calling GetMinStake.
type GetMinStakeArgs struct {
	ChainID ids.ID `json:"netID"`
}

// GetMinStakeReply is the response from calling GetMinStake.
type GetMinStakeReply struct {
	//  The minimum amount of tokens one must bond to be a validator
	MinValidatorStake avajson.Uint64 `json:"minValidatorStake"`
	// Minimum stake, in µLUX, that can be delegated on the primary network
	MinDelegatorStake avajson.Uint64 `json:"minDelegatorStake"`
}

// getMinStake returns the least that can be staked on a net, as a validator
// and as a delegator.
func (s *Service) getMinStake(ctx context.Context, args *GetMinStakeArgs) (*GetMinStakeReply, error) {
	reply := &GetMinStakeReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getMinStake",
	)

	if args.ChainID == constants.PrimaryNetworkID {
		reply.MinValidatorStake = avajson.Uint64(s.vm.MinValidatorStake)
		reply.MinDelegatorStake = avajson.Uint64(s.vm.MinDelegatorStake)
		return reply, nil
	}

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	transformNetIntf, err := s.vm.state.GetNetTransformation(args.ChainID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed fetching net transformation for %s: %w",
			args.ChainID,
			err,
		)
	}
	transformNet, ok := transformNetIntf.Unsigned.(*txs.TransformChainTx)
	if !ok {
		return nil, fmt.Errorf(
			"unexpected net transformation tx type fetched %T",
			transformNetIntf.Unsigned,
		)
	}

	reply.MinValidatorStake = avajson.Uint64(transformNet.MinValidatorStake())
	reply.MinDelegatorStake = avajson.Uint64(transformNet.MinDelegatorStake())

	return reply, nil
}

// GetTotalStakeArgs are the arguments for calling GetTotalStake
type GetTotalStakeArgs struct {
	// Net we're getting the total stake
	// If omitted returns Primary network weight
	ChainID ids.ID `json:"netID"`
}

// GetTotalStakeReply is the response from calling GetTotalStake.
type GetTotalStakeReply struct {
	// Deprecated: Use Weight instead.
	Stake avajson.Uint64 `json:"stake"`

	Weight avajson.Uint64 `json:"weight"`
}

// getTotalStake returns the total weight of a net's validator set.
func (s *Service) getTotalStake(ctx context.Context, args *GetTotalStakeArgs) (*GetTotalStakeReply, error) {
	reply := &GetTotalStakeReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getTotalStake",
	)

	totalWeight, err := s.vm.Validators.TotalWeight(args.ChainID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get total weight: %w", err)
	}
	weight := avajson.Uint64(totalWeight)
	reply.Weight = weight
	reply.Stake = weight
	return reply, nil
}

// GetRewardUTXOsReply defines the GetRewardUTXOs replies returned from the API
type GetRewardUTXOsReply struct {
	// Number of UTXOs returned
	NumFetched avajson.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []string `json:"utxos"`
	// Encoding specifies the encoding format the UTXOs are returned in
	Encoding formatting.Encoding `json:"encoding"`
}

// getRewardUTXOs returns the UTXOs paid out when a staking transaction's period
// ended.
func (s *Service) getRewardUTXOs(ctx context.Context, args *apitypes.GetTxArgs) (*GetRewardUTXOsReply, error) {
	reply := &GetRewardUTXOsReply{}
	s.vm.log.Debug("deprecated API called",
		"service", "platform",
		"method", "getRewardUTXOs",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	utxos, err := s.vm.state.GetRewardUTXOs(args.TxID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get reward UTXOs: %w", err)
	}

	reply.NumFetched = avajson.Uint64(len(utxos))
	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		utxoBytes, err := utxo.WireBytes()
		if err != nil {
			return nil, fmt.Errorf("couldn't encode UTXO to bytes: %w", err)
		}

		utxoStr, err := formatting.Encode(args.Encoding, utxoBytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't encode utxo as %s: %w", args.Encoding, err)
		}
		reply.UTXOs[i] = utxoStr
	}
	reply.Encoding = args.Encoding
	return reply, nil
}

// GetTimestampReply is the response from GetTimestamp
type GetTimestampReply struct {
	// Current timestamp
	Timestamp avajson.Time `json:"timestamp"`
}

// getTimestamp returns the chain's current time.
func (s *Service) getTimestamp(ctx context.Context, _ *struct{}) (*GetTimestampReply, error) {
	reply := &GetTimestampReply{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getTimestamp",
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	reply.Timestamp = avajson.NewTime(s.vm.state.GetTimestamp())
	return reply, nil
}

// GetValidatorsAtArgs is the response from GetValidatorsAt
type GetValidatorsAtArgs struct {
	Height  platformapitypes.Height `json:"height"`
	ChainID ids.ID                  `json:"netID"`
}

// The two validator-set replies build their own object rather than letting the
// codec walk a struct, and they do it under jsonv1.DefaultOptionsV1 because v1
// is the spelling mainnet answers in. Two v1/v2 differences are visible in
// exactly these bytes: a nil []byte is
// null under v1 and "" under v2, and map entries are ordered under v1 and are
// not under v2. Both show in what mainnet answers today — see testdata/ — so
// the semantics are part of the wire, not a preference.
type jsonGetValidatorOutput struct {
	PublicKey *string        `json:"publicKey"`
	Weight    avajson.Uint64 `json:"weight"`
}

func (v *GetValidatorsAtReply) MarshalJSON() ([]byte, error) {
	m := make(map[ids.NodeID]*jsonGetValidatorOutput, len(v.Validators))
	for _, vdr := range v.Validators {
		vdrJSON := &jsonGetValidatorOutput{
			Weight: vdr.Weight,
		}

		if vdr.PublicKey != nil {
			pk, err := formatting.Encode(formatting.HexNC, vdr.PublicKey)
			if err != nil {
				return nil, err
			}
			vdrJSON.PublicKey = &pk
		}

		m[vdr.NodeID] = vdrJSON
	}
	return json.Marshal(m, jsonv1.DefaultOptionsV1())
}

func (v *GetValidatorsAtReply) UnmarshalJSON(b []byte) error {
	var m map[ids.NodeID]*jsonGetValidatorOutput
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	if m == nil {
		v.Validators = nil
		return nil
	}

	v.Validators = make(ValidatorSet, 0, len(m))
	for nodeID, vdrJSON := range m {
		vdr := Validator{
			NodeID: nodeID,
			Weight: vdrJSON.Weight,
		}

		if vdrJSON.PublicKey != nil {
			pkBytes, err := formatting.Decode(formatting.HexNC, *vdrJSON.PublicKey)
			if err != nil {
				return err
			}
			vdr.PublicKey = pkBytes
		}

		v.Validators = append(v.Validators, vdr)
	}
	slices.SortFunc(v.Validators, func(x, y Validator) int { return x.NodeID.Compare(y.NodeID) })
	return nil
}

// GetValidatorsAtReply is the response from GetValidatorsAt.
//
// The wire carries a node id, a public key and a weight per validator, and that
// is what this holds. The read behind it answers with more — a corona key, a
// light weight, the tx that added the validator — and none of it has ever been
// on the wire, so a reply that held it would state a contract the answer does
// not keep.
type GetValidatorsAtReply struct {
	Validators ValidatorSet
}

// getValidatorsAt returns a net's validator set as it stood at a height: each
// node, its BLS key where it has one, and its weight.
//
// The height "proposed" asks for the height the next proposal will be built at.
func (s *Service) getValidatorsAt(ctx context.Context, args *GetValidatorsAtArgs) (*GetValidatorsAtReply, error) {
	reply := &GetValidatorsAtReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getValidatorsAt"),
		log.Uint64("height", uint64(args.Height)),
		log.Bool("isProposed", args.Height.IsProposed()),
		log.Stringer("netID", args.ChainID),
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	height := uint64(args.Height)
	if args.Height.IsProposed() {
		// Get the proposed height from the last accepted block
		lastAcceptedID := s.vm.state.GetLastAccepted()
		lastAcceptedBlock, err := s.vm.manager.GetStatelessBlock(lastAcceptedID)
		if err != nil {
			return nil, fmt.Errorf("failed to get last accepted block: %w", err)
		}
		height = lastAcceptedBlock.Height()
	}

	set, err := s.vm.GetValidatorSet(ctx, height, args.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator set: %w", err)
	}
	reply.Validators = newValidatorSet(set)
	return reply, nil
}

// GetAllValidatorsAtArgs are the arguments to GetAllValidatorsAt
type GetAllValidatorsAtArgs struct {
	Height platformapitypes.Height `json:"height"`
}

// GetAllValidatorsAtReply is the response from GetAllValidatorsAt.
//
// One entry per chain, each carrying its own set. The wire is an object keyed
// by chain id whose values are objects keyed by node id, and MarshalJSON writes
// exactly that.
type GetAllValidatorsAtReply struct {
	ValidatorSets []ChainValidatorSet `json:"validatorSets"`
}

func (v GetAllValidatorsAtReply) MarshalJSON() ([]byte, error) {
	if v.ValidatorSets == nil {
		return []byte(`{"validatorSets":` + avajson.Null + `}`), nil
	}
	m := make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput, len(v.ValidatorSets))
	for _, set := range v.ValidatorSets {
		inner := make(map[ids.NodeID]*validators.GetValidatorOutput, len(set.Validators))
		for _, vdr := range set.Validators {
			inner[vdr.NodeID] = vdr
		}
		m[set.ChainID] = inner
	}
	return json.Marshal(struct {
		ValidatorSets map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput `json:"validatorSets"`
	}{m}, jsonv1.DefaultOptionsV1())
}

func (v *GetAllValidatorsAtReply) UnmarshalJSON(b []byte) error {
	var wire struct {
		ValidatorSets map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput `json:"validatorSets"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	if wire.ValidatorSets == nil {
		v.ValidatorSets = nil
		return nil
	}
	v.ValidatorSets = make([]ChainValidatorSet, 0, len(wire.ValidatorSets))
	for chainID, set := range wire.ValidatorSets {
		v.ValidatorSets = append(v.ValidatorSets, newChainValidatorSet(chainID, set))
	}
	slices.SortFunc(v.ValidatorSets, func(x, y ChainValidatorSet) int {
		return x.ChainID.Compare(y.ChainID)
	})
	return nil
}

// getAllValidatorsAt returns every net's validator set as it stood at a height,
// the primary network included.
//
// The height "proposed" asks for the height the next proposal will be built at.
func (s *Service) getAllValidatorsAt(ctx context.Context, args *GetAllValidatorsAtArgs) (*GetAllValidatorsAtReply, error) {
	reply := &GetAllValidatorsAtReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getAllValidatorsAt"),
		log.Uint64("height", uint64(args.Height)),
		log.Bool("isProposed", args.Height.IsProposed()),
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	height := uint64(args.Height)
	if args.Height.IsProposed() {
		// Get the proposed height from the last accepted block
		lastAcceptedID := s.vm.state.GetLastAccepted()
		lastAcceptedBlock, err := s.vm.manager.GetStatelessBlock(lastAcceptedID)
		if err != nil {
			return nil, fmt.Errorf("failed to get last accepted block: %w", err)
		}
		height = lastAcceptedBlock.Height()
	}

	// Get all net IDs
	netIDs, err := s.vm.state.GetChainIDs()
	if err != nil {
		return nil, fmt.Errorf("failed to get net IDs: %w", err)
	}

	reply.ValidatorSets = make([]ChainValidatorSet, 0, len(netIDs)+1)

	// Add primary network first
	primaryValidators, err := s.vm.GetValidatorSet(ctx, height, constants.PrimaryNetworkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get primary network validator set: %w", err)
	}
	reply.ValidatorSets = append(reply.ValidatorSets, newChainValidatorSet(constants.PrimaryNetworkID, primaryValidators))

	// Add all nets
	for _, netID := range netIDs {
		netValidators, err := s.vm.GetValidatorSet(ctx, height, netID)
		if err != nil {
			return nil, fmt.Errorf("failed to get validator set for net %s: %w", netID, err)
		}
		reply.ValidatorSets = append(reply.ValidatorSets, newChainValidatorSet(netID, netValidators))
	}
	slices.SortFunc(reply.ValidatorSets, func(x, y ChainValidatorSet) int {
		return x.ChainID.Compare(y.ChainID)
	})

	return reply, nil
}

// getBlock returns a block by its id.
//
// The encoding chooses the shape of the answer: hex returns the block's bytes as
// one string, and json returns the block as an object.
func (s *Service) getBlock(ctx context.Context, args *apitypes.GetBlockArgs) (*apitypes.GetBlockResponse, error) {
	response := &apitypes.GetBlockResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getBlock",
		"blkID", args.BlockID,
		"encoding", args.Encoding,
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	block, err := s.vm.manager.GetStatelessBlock(args.BlockID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get block with id %s: %w", args.BlockID, err)
	}
	response.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		// block.InitRuntime(s.vm.rt)
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return nil, fmt.Errorf("couldn't encode block %s as %s: %w", args.BlockID, args.Encoding, err)
		}
	}

	response.Block, err = json.Marshal(result, jsonv1.FormatByteArrayAsArray(true))
	return nil, err
}

// getBlockByHeight returns the accepted block at a height.
//
// The encoding chooses the shape of the answer: hex returns the block's bytes as
// one string, and json returns the block as an object.
func (s *Service) getBlockByHeight(ctx context.Context, args *apitypes.GetBlockByHeightArgs) (*apitypes.GetBlockResponse, error) {
	response := &apitypes.GetBlockResponse{}
	s.vm.log.Debug("API called",
		"service", "platform",
		"method", "getBlockByHeight",
		"height", uint64(args.Height),
		"encoding", args.Encoding,
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	blockID, err := s.vm.state.GetBlockIDAtHeight(uint64(args.Height))
	if err != nil {
		return nil, fmt.Errorf("couldn't get block at height %d: %w", args.Height, err)
	}

	block, err := s.vm.manager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.log.Error("couldn't get accepted block",
			"blkID", blockID,
			"error", err,
		)
		return nil, fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}
	response.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		// block.InitRuntime(s.vm.rt)
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return nil, fmt.Errorf("couldn't encode block %s as %s: %w", blockID, args.Encoding, err)
		}
	}

	response.Block, err = json.Marshal(result, jsonv1.FormatByteArrayAsArray(true))
	return nil, err
}

// getFeeConfig returns the chain's dynamic-fee configuration: what gas each
// dimension of a transaction costs, the capacity and target per second, and the
// constants the price curve is drawn from.
func (s *Service) getFeeConfig(ctx context.Context, _ *struct{}) (*GetFeeConfigReply, error) {
	reply := &GetFeeConfigReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getFeeConfig"),
	)

	*reply = newFeeConfig(s.vm.DynamicFeeConfig)
	return reply, nil
}

type GetFeeStateReply struct {
	gas.State
	Price gas.Price    `json:"price"`
	Time  avajson.Time `json:"timestamp"`
}

// getFeeState returns the chain's current fee state: the gas consumed, the
// excess it is priced off, the price that excess implies, and the time it was
// read at.
func (s *Service) getFeeState(ctx context.Context, _ *struct{}) (*GetFeeStateReply, error) {
	reply := &GetFeeStateReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getFeeState"),
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	reply.State = s.vm.state.GetFeeState()
	reply.Price = gas.CalculatePrice(
		s.vm.DynamicFeeConfig.MinPrice,
		reply.State.Excess,
		s.vm.DynamicFeeConfig.ExcessConversionConstant,
	)
	reply.Time = avajson.NewTime(s.vm.state.GetTimestamp())
	return reply, nil
}

// getValidatorFeeConfig returns the configuration the L1 validator fee is
// calculated from.
func (s *Service) getValidatorFeeConfig(ctx context.Context, _ *struct{}) (*fee.Config, error) {
	reply := &fee.Config{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getValidatorFeeConfig"),
	)

	*reply = s.vm.ValidatorFeeConfig
	return reply, nil
}

type GetValidatorFeeStateReply struct {
	Excess gas.Gas      `json:"excess"`
	Price  gas.Price    `json:"price"`
	Time   avajson.Time `json:"timestamp"`
}

// getValidatorFeeState returns what an L1 validator is currently charged per
// second to keep validating, and the time it was read at.
func (s *Service) getValidatorFeeState(ctx context.Context, _ *struct{}) (*GetValidatorFeeStateReply, error) {
	reply := &GetValidatorFeeStateReply{}
	s.vm.log.Debug("API called",
		log.String("service", "platform"),
		log.String("method", "getValidatorFeeState"),
	)

	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	reply.Excess = s.vm.state.GetL1ValidatorExcess()
	reply.Price = gas.CalculatePrice(
		s.vm.ValidatorFeeConfig.MinPrice,
		reply.Excess,
		s.vm.ValidatorFeeConfig.ExcessConversionConstant,
	)
	reply.Time = avajson.NewTime(s.vm.state.GetTimestamp())
	return reply, nil
}

func (s *Service) getAPIOwner(owner *secp256k1fx.OutputOwners) (*platformapitypes.Owner, error) {
	apiOwner := &platformapitypes.Owner{
		Locktime:  avajson.Uint64(owner.Locktime),
		Threshold: avajson.Uint32(owner.Threshold),
		Addresses: make([]string, 0, len(owner.Addrs)),
	}
	for _, addr := range owner.Addrs {
		addrStr, err := s.addrManager.FormatLocalAddress(addr)
		if err != nil {
			return nil, err
		}
		apiOwner.Addresses = append(apiOwner.Addresses, addrStr)
	}
	return apiOwner, nil
}

// Takes in a staker and a set of addresses
// Returns:
// 1) The total amount staked by addresses in [addrs]
// 2) The staked outputs
func getStakeHelper(tx *txs.Tx, addrs set.Set[ids.ShortID], totalAmountStaked map[ids.ID]uint64) []lux.TransferableOutput {
	staker, ok := tx.Unsigned.(txs.PermissionlessStaker)
	if !ok {
		return nil
	}

	stake := staker.Stake()
	stakedOuts := make([]lux.TransferableOutput, 0, len(stake))
	// Go through all of the staked outputs
	for _, output := range stake {
		out := output.Out
		if lockedOut, ok := out.(*stakeable.LockOut); ok {
			// This output can only be used for staking until [stakeOnlyUntil]
			out = lockedOut.TransferableOut
		}
		secpOut, ok := out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}

		// Check whether this output is owned by one of the given addresses
		contains := slices.ContainsFunc(secpOut.Addrs, addrs.Contains)
		if !contains {
			// This output isn't owned by one of the given addresses. Ignore.
			continue
		}

		assetID := output.AssetID()
		newAmount, err := safemath.Add(totalAmountStaked[assetID], secpOut.Amt)
		if err != nil {
			newAmount = math.MaxUint64
		}
		totalAmountStaked[assetID] = newAmount

		stakedOuts = append(
			stakedOuts,
			*output,
		)
	}
	return stakedOuts
}

func toPlatformStaker(staker *state.Staker) platformapitypes.Staker {
	return platformapitypes.Staker{
		TxID:      staker.TxID,
		StartTime: avajson.Uint64(staker.StartTime.Unix()),
		EndTime:   avajson.Uint64(staker.EndTime.Unix()),
		Weight:    avajson.Uint64(staker.Weight),
		NodeID:    staker.NodeID,
	}
}
