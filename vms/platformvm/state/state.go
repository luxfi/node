// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/btree"
	"github.com/luxfi/metric"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/runtime"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/constants"
	"github.com/luxfi/container/maybe"
	"github.com/luxfi/database"
	"github.com/luxfi/database/linkeddb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/cache/metercacher"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"

	safemath "github.com/luxfi/math"
)

const (
	defaultTreeDegree             = 2
	indexIterationLimit           = 4096
	indexIterationSleepMultiplier = 5
	indexIterationSleepCap        = 10 * time.Second
	indexLogFrequency             = 30 * time.Second
)

var (
	_ State = (*state)(nil)

	errValidatorSetAlreadyPopulated   = errors.New("validator set already populated")
	errIsNotNet                       = errors.New("is not a chain")
	errMissingPrimaryNetworkValidator = errors.New("missing primary network validator")

	BlockIDPrefix                 = []byte("blockID")
	BlockPrefix                   = []byte("block")
	ValidatorsPrefix              = []byte("validators")
	CurrentPrefix                 = []byte("current")
	PendingPrefix                 = []byte("pending")
	ValidatorPrefix               = []byte("validator")
	DelegatorPrefix               = []byte("delegator")
	NetValidatorPrefix            = []byte("chainValidator")
	NetDelegatorPrefix            = []byte("chainDelegator")
	ValidatorWeightDiffsPrefix    = []byte("flatValidatorDiffs")
	ValidatorPublicKeyDiffsPrefix = []byte("flatPublicKeyDiffs")
	TxPrefix                      = []byte("tx")
	RewardUTXOsPrefix             = []byte("rewardUTXOs")
	UTXOPrefix                    = []byte("utxo")
	NetPrefix                     = []byte("chain")
	NetOwnerPrefix                = []byte("chainOwner")
	NetToL1ConversionPrefix       = []byte("chainToL1Conversion")
	TransformedNetPrefix          = []byte("transformedNet")
	SupplyPrefix                  = []byte("supply")
	ChainPrefix                   = []byte("chain")
	ChainNamePrefix               = []byte("chainName") // maps lowercase chain name -> chainID
	ExpiryReplayProtectionPrefix  = []byte("expiryReplayProtection")
	L1Prefix                      = []byte("l1")
	WeightsPrefix                 = []byte("weights")
	ChainIDNodeIDPrefix           = []byte("chainIDNodeID")
	ActivePrefix                  = []byte("active")
	InactivePrefix                = []byte("inactive")
	SingletonPrefix               = []byte("singleton")

	TimestampKey         = []byte("timestamp")
	FeeStateKey          = []byte("fee state")
	L1ValidatorExcessKey = []byte("l1Validator excess")
	AccruedFeesKey       = []byte("accrued fees")
	CurrentSupplyKey     = []byte("current supply")
	LastAcceptedKey      = []byte("last accepted")
	HeightsIndexedKey    = []byte("heights indexed")
	InitializedKey       = []byte("initialized")
	BlocksReindexedKey   = []byte("blocks reindexed.3")

	emptyL1ValidatorCache = &cache.Empty[ids.ID, maybe.Maybe[L1Validator]]{}
)

// Chain collects all methods to manage the state of the chain for block
// execution.
type Chain interface {
	Expiry
	L1Validators
	Stakers
	L1Validators
	lux.UTXOAdder
	lux.UTXOGetter
	lux.UTXODeleter

	GetTimestamp() time.Time
	SetTimestamp(tm time.Time)

	GetFeeState() gas.State
	SetFeeState(f gas.State)

	GetL1ValidatorExcess() gas.Gas
	SetL1ValidatorExcess(e gas.Gas)

	GetAccruedFees() uint64
	SetAccruedFees(f uint64)

	GetCurrentSupply(chainID ids.ID) (uint64, error)
	SetCurrentSupply(chainID ids.ID, cs uint64)

	AddRewardUTXO(txID ids.ID, utxo *lux.UTXO)

	AddNet(netID ids.ID)

	GetNetOwner(netID ids.ID) (fx.Owner, error)
	SetNetOwner(netID ids.ID, owner fx.Owner)

	GetNetToL1Conversion(chainID ids.ID) (NetToL1Conversion, error)
	SetNetToL1Conversion(chainID ids.ID, c NetToL1Conversion)

	GetNetTransformation(chainID ids.ID) (*txs.Tx, error)
	AddNetTransformation(transformNetTx *txs.Tx)

	AddChain(createChainTx *txs.Tx)

	// Chain name uniqueness - for network-wide chain name resolution
	GetChainIDByName(name string) (ids.ID, error)
	IsChainNameTaken(name string) bool

	GetTx(txID ids.ID) (*txs.Tx, status.Status, error)
	AddTx(tx *txs.Tx, status status.Status)

	// L1 Validator support - most methods inherited from L1Validators interface
	// Only PutL1Validator is Chain-specific
	PutL1Validator(validator L1Validator) error
}

type State interface {
	Chain
	uptime.State
	lux.UTXOReader

	GetLastAccepted() ids.ID
	SetLastAccepted(blkID ids.ID)

	GetStatelessBlock(blockID ids.ID) (block.Block, error)

	// Invariant: [block] is an accepted block.
	AddStatelessBlock(block block.Block)

	GetBlockIDAtHeight(height uint64) (ids.ID, error)

	GetRewardUTXOs(txID ids.ID) ([]*lux.UTXO, error)
	GetChainIDs() ([]ids.ID, error)
	GetChains(netID ids.ID) ([]*txs.Tx, error)

	// ApplyValidatorWeightDiffs iterates from [startHeight] towards the genesis
	// block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// will typically be greater than or equal to [endHeight]. If [startHeight]
	// is less than [endHeight], no diffs will be applied.
	ApplyValidatorWeightDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
		netID ids.ID,
	) error

	// ApplyValidatorPublicKeyDiffs iterates from [startHeight] towards the
	// genesis block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// will typically be greater than or equal to [endHeight]. If [startHeight]
	// is less than [endHeight], no diffs will be applied.
	ApplyValidatorPublicKeyDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
		chainID ids.ID,
	) error

	SetHeight(height uint64)

	// GetCurrentValidators returns chain and L1 validators for the given
	// chainID along with the current P-chain height.
	// This method works for both chains and L1s. Depending of the requested
	// chain/L1 validator schema, the return values can include only chain
	// validator, only L1 validators or both if there are initial stakers in the
	// L1 conversion.
	GetCurrentValidators(ctx context.Context, chainID ids.ID) ([]*Staker, []L1Validator, uint64, error)

	// Discard uncommitted changes to the database.
	Abort()

	// ReindexBlocks converts any block indices using the legacy storage format
	// to the new format. If this database has already updated the indices,
	// this function will return immediately, without iterating over the
	// database.
	//
	// ReindexBlocks converts legacy block storage indices to the current format.
	// Retained for backward compatibility with pre-v1.14.x databases.
	ReindexBlocks(lock sync.Locker, log log.Logger) error

	// Commit changes to the base database.
	Commit() error

	// Returns a batch of unwritten changes that, when written, will commit all
	// pending changes to the base database.
	CommitBatch() (database.Batch, error)

	Checksum() ids.ID

	Close() error
}

// Prior to https://github.com/luxfi/node/pull/1719, blocks were
// stored as a map from blkID to stateBlk. Nodes synced prior to this PR may
// still have blocks partially stored using this legacy format.
//
// stateBlk is the legacy block storage format from before PR #1719.
// Retained for backward compatibility with pre-v1.14.x databases.
type stateBlk struct {
	Bytes  []byte `serialize:"true"`
	Status uint32 `serialize:"true"`
}

// RegisterStateBlockType registers the stateBlk type with the given codec.
// This is needed for backward compatibility with old block storage format.
func RegisterStateBlockType(targetCodec codec.Registry) error {
	return targetCodec.RegisterType(&stateBlk{})
}

// Initialize the stateBlk type registration with the block codecs.
// This must be called before using parseStoredBlock for backward compatibility.
func init() {
	// We need to register stateBlk with block.GenesisCodec so that
	// parseStoredBlock can deserialize legacy block storage format.
	// Since state imports block (no import cycle), we can register directly.
	if err := block.RegisterGenesisType(&stateBlk{}); err != nil {
		panic(err)
	}
}

/*
 * VMDB
 * |-. validators
 * | |-. current
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> uptime + potential reward + potential delegatee reward
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> potential reward
 * | | |-. chainValidator
 * | | | '-. list
 * | | |   '-- txID -> uptime + potential reward + potential delegatee reward
 * | | '-. chainDelegator
 * | |   '-. list
 * | |     '-- txID -> potential reward
 * | |-. pending
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | |-. chainValidator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | '-. chainDelegator
 * | |   '-. list
 * | |     '-- txID -> nil
 * | |-. l1
 * | | |-. weights
 * | | | '-- chainID -> weight
 * | | |-. chainIDNodeID
 * | | | '-- chainID+nodeID -> validationID
 * | | |-. active
 * | | | '-- validationID -> l1Validator
 * | | '-. inactive
 * | |   '-- validationID -> l1Validator
 * | |-. weight diffs
 * | | '-- chain+height+nodeID -> weightChange
 * | '-. pub key diffs
 * |   '-- chain+height+nodeID -> uncompressed public key or nil
 * |-. blockIDs
 * | '-- height -> blockID
 * |-. blocks
 * | '-- blockID -> block bytes
 * |-. txs
 * | '-- txID -> tx bytes + tx status
 * |- rewardUTXOs
 * | '-. txID
 * |   '-. list
 * |     '-- utxoID -> utxo bytes
 * |- utxos
 * | '-- utxoDB
 * |-. chains
 * | '-. list
 * |   '-- txID -> nil
 * |-. chainOwners
 * | '-- chainID -> owner
 * |-. chainToL1Conversions
 * | '-- chainID -> conversionID + chainID + addr
 * |-. chains
 * | '-. netID
 * |   '-. list
 * |     '-- txID -> nil
 * |-. expiryReplayProtection
 * | '-- timestamp + validationID -> nil
 * '-. singletons
 *   |-- initializedKey -> nil
 *   |-- blocksReindexedKey -> nil
 *   |-- timestampKey -> timestamp
 *   |-- feeStateKey -> feeState
 *   |-- l1ValidatorExcessKey -> l1ValidatorExcess
 *   |-- accruedFeesKey -> accruedFees
 *   |-- currentSupplyKey -> currentSupply
 *   |-- lastAcceptedKey -> lastAccepted
 *   '-- heightsIndexKey -> startIndexHeight + endIndexHeight
 */
type state struct {
	validatorState

	validators validators.Manager
	rt         *runtime.Runtime
	upgrades   upgrade.Config
	metrics    metrics.Metrics
	rewards    reward.Calculator

	baseDB *versiondb.Database

	expiry     *btree.BTreeG[ExpiryEntry]
	expiryDiff *expiryDiff
	expiryDB   database.Database

	activeL1Validators   *activeL1Validators
	l1ValidatorsDiff     *l1ValidatorsDiff
	l1ValidatorsDiffLock sync.RWMutex // Protects concurrent access to l1ValidatorsDiff
	l1ValidatorsDB       database.Database
	weightsCache         cache.Cacher[ids.ID, uint64] // chainID -> total L1 validator weight
	weightsDB            database.Database
	chainIDNodeIDCache   cache.Cacher[chainIDNodeID, bool] // chainID+nodeID -> is validator
	chainIDNodeIDDB      database.Database
	activeDB             database.Database
	inactiveCache        cache.Cacher[ids.ID, maybe.Maybe[L1Validator]] // validationID -> L1Validator
	inactiveDB           database.Database

	currentStakers *baseStakers
	pendingStakers *baseStakers

	currentHeight uint64

	// L1 validators storage (temporary in-memory implementation)
	l1Validators map[ids.ID]L1Validator

	addedBlockIDs map[uint64]ids.ID            // map of height -> blockID
	blockIDCache  cache.Cacher[uint64, ids.ID] // cache of height -> blockID; if the entry is ids.Empty, it is not in the database
	blockIDDB     database.Database

	addedBlocks map[ids.ID]block.Block            // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, block.Block] // cache of blockID -> Block; if the entry is nil, it is not in the database
	blockDB     database.Database

	validatorsDB              database.Database
	currentValidatorsDB       database.Database
	currentValidatorBaseDB    database.Database
	currentValidatorList      linkeddb.LinkedDB
	currentDelegatorBaseDB    database.Database
	currentDelegatorList      linkeddb.LinkedDB
	currentNetValidatorBaseDB database.Database
	currentNetValidatorList   linkeddb.LinkedDB
	currentNetDelegatorBaseDB database.Database
	currentNetDelegatorList   linkeddb.LinkedDB
	pendingValidatorsDB       database.Database
	pendingValidatorBaseDB    database.Database
	pendingValidatorList      linkeddb.LinkedDB
	pendingDelegatorBaseDB    database.Database
	pendingDelegatorList      linkeddb.LinkedDB
	pendingNetValidatorBaseDB database.Database
	pendingNetValidatorList   linkeddb.LinkedDB
	pendingNetDelegatorBaseDB database.Database
	pendingNetDelegatorList   linkeddb.LinkedDB

	validatorWeightDiffsDB    database.Database
	validatorPublicKeyDiffsDB database.Database

	addedTxs map[ids.ID]*txAndStatus            // map of txID -> {*txs.Tx, Status}
	txCache  cache.Cacher[ids.ID, *txAndStatus] // txID -> {*txs.Tx, Status}; if the entry is nil, it is not in the database
	txDB     database.Database

	addedRewardUTXOs map[ids.ID][]*lux.UTXO            // map of txID -> []*UTXO
	rewardUTXOsCache cache.Cacher[ids.ID, []*lux.UTXO] // txID -> []*UTXO
	rewardUTXODB     database.Database

	modifiedUTXOs map[ids.ID]*lux.UTXO // map of modified UTXOID -> *UTXO; if the UTXO is nil, it has been removed
	utxoDB        database.Database
	utxoState     lux.UTXOState

	cachedChainIDs []ids.ID // nil if the chains haven't been loaded
	addedChainIDs  []ids.ID
	chainBaseDB    database.Database
	chainDB        linkeddb.LinkedDB

	chainOwners     map[ids.ID]fx.Owner                  // map of netID -> owner
	chainOwnerCache cache.Cacher[ids.ID, fxOwnerAndSize] // cache of netID -> owner; if the entry is nil, it is not in the database
	chainOwnerDB    database.Database

	chainToL1Conversions     map[ids.ID]NetToL1Conversion            // map of chainID -> conversion of the chain
	chainToL1ConversionCache cache.Cacher[ids.ID, NetToL1Conversion] // cache of chainID -> conversion
	chainToL1ConversionDB    database.Database

	transformedNets     map[ids.ID]*txs.Tx            // map of chainID -> transformNetTx
	transformedNetCache cache.Cacher[ids.ID, *txs.Tx] // cache of chainID -> transformNetTx; if the entry is nil, it is not in the database
	transformedNetDB    database.Database

	modifiedSupplies map[ids.ID]uint64             // map of netID -> current supply
	supplyCache      cache.Cacher[ids.ID, *uint64] // cache of netID -> current supply; if the entry is nil, it is not in the database
	supplyDB         database.Database

	addedChains  map[ids.ID][]*txs.Tx                    // maps netID -> the newly added chains to the chain
	chainCache   cache.Cacher[ids.ID, []*txs.Tx]         // cache of netID -> the chains after all local modifications []*txs.Tx
	chainDBCache cache.Cacher[ids.ID, linkeddb.LinkedDB] // cache of netID -> linkedDB
	chainsDB     database.Database                       // base database for net-specific chains

	// Chain name uniqueness - maps lowercase chain name to chain ID
	addedChainNames map[string]ids.ID            // newly added chain names (lowercase) -> chainID
	chainNameCache  cache.Cacher[string, ids.ID] // cache of lowercase chain name -> chainID
	chainNameDB     database.Database

	// The persisted fields represent the current database value
	timestamp, persistedTimestamp                 time.Time
	feeState, persistedFeeState                   gas.State
	l1ValidatorExcess, persistedL1ValidatorExcess gas.Gas
	accruedFees, persistedAccruedFees             uint64
	currentSupply, persistedCurrentSupply         uint64
	// [lastAccepted] is the most recently accepted block.
	lastAccepted, persistedLastAccepted ids.ID
	lastAcceptedLock                    sync.RWMutex // Protects concurrent access to lastAccepted
	indexedHeights                      *heightRange
	singletonDB                         database.Database
}

// heightRange is used to track which heights are safe to use the native DB
// iterator for querying validator diffs.
//
// new indexing mechanism.
type heightRange struct {
	LowerBound uint64 `serialize:"true"`
	UpperBound uint64 `serialize:"true"`
}

type ValidatorWeightDiff struct {
	Decrease     bool   `serialize:"true"`
	Amount       uint64 `serialize:"true"`
	ValidationID ids.ID `serialize:"true"` // Added to preserve TxID during diff application
}

func (v *ValidatorWeightDiff) Add(amount uint64) error {
	return v.addOrSub(false, amount)
}

func (v *ValidatorWeightDiff) Sub(amount uint64) error {
	return v.addOrSub(true, amount)
}

func (v *ValidatorWeightDiff) addOrSub(sub bool, amount uint64) error {
	if v.Decrease == sub {
		var err error
		v.Amount, err = safemath.Add64(v.Amount, amount)
		return err
	}

	if v.Amount > amount {
		v.Amount -= amount
	} else {
		v.Amount = safemath.AbsDiff(v.Amount, amount)
		v.Decrease = sub
	}
	return nil
}

type txBytesAndStatus struct {
	Tx     []byte        `serialize:"true"`
	Status status.Status `serialize:"true"`
}

type txAndStatus struct {
	tx     *txs.Tx
	status status.Status
}

type fxOwnerAndSize struct {
	owner fx.Owner
	size  int
}

type NetToL1Conversion struct {
	ConversionID ids.ID `serialize:"true"`
	ChainID      ids.ID `serialize:"true"`
	Addr         []byte `serialize:"true"`
}

func txSize(_ ids.ID, tx *txs.Tx) int {
	if tx == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(tx.Bytes()) + constants.PointerOverhead
}

func txAndStatusSize(_ ids.ID, t *txAndStatus) int {
	if t == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(t.tx.Bytes()) + wrappers.IntLen + 2*constants.PointerOverhead
}

func blockSize(_ ids.ID, blk block.Block) int {
	if blk == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(blk.Bytes()) + constants.PointerOverhead
}

func New(
	db database.Database,
	genesisBytes []byte,
	metricsReg metric.Registerer,
	validators validators.Manager,
	upgrades upgrade.Config,
	execCfg *config.Config,
	rt *runtime.Runtime,
	metrics metrics.Metrics,
	rewards reward.Calculator,
) (State, error) {
	// Convert metric.Registerer to metric.Registry
	// metricsReg implements Registerer, cast it to metric.Registry
	var reg metric.Registry
	if r, ok := metricsReg.(metric.Registry); ok {
		reg = r
	} else {
		// If conversion fails, use nil (metercacher will handle it)
		reg = nil
	}

	blockIDCache, err := metercacher.New[uint64, ids.ID](
		"block_id_cache",
		reg,
		lru.NewCache[uint64, ids.ID](execCfg.BlockIDCacheSize),
	)
	if err != nil {
		return nil, err
	}

	blockCache, err := metercacher.New[ids.ID, block.Block](
		"block_cache",
		reg,
		lru.NewSizedCache(execCfg.BlockCacheSize, blockSize),
	)
	if err != nil {
		return nil, err
	}

	baseDB := versiondb.New(db)

	validatorsDB := prefixdb.New(ValidatorsPrefix, baseDB)

	currentValidatorsDB := prefixdb.New(CurrentPrefix, validatorsDB)
	currentValidatorBaseDB := prefixdb.New(ValidatorPrefix, currentValidatorsDB)
	currentDelegatorBaseDB := prefixdb.New(DelegatorPrefix, currentValidatorsDB)
	currentNetValidatorBaseDB := prefixdb.New(NetValidatorPrefix, currentValidatorsDB)
	currentNetDelegatorBaseDB := prefixdb.New(NetDelegatorPrefix, currentValidatorsDB)

	pendingValidatorsDB := prefixdb.New(PendingPrefix, validatorsDB)
	pendingValidatorBaseDB := prefixdb.New(ValidatorPrefix, pendingValidatorsDB)
	pendingDelegatorBaseDB := prefixdb.New(DelegatorPrefix, pendingValidatorsDB)
	pendingNetValidatorBaseDB := prefixdb.New(NetValidatorPrefix, pendingValidatorsDB)
	pendingNetDelegatorBaseDB := prefixdb.New(NetDelegatorPrefix, pendingValidatorsDB)

	l1ValidatorsDB := prefixdb.New(L1Prefix, validatorsDB)

	validatorWeightDiffsDB := prefixdb.New(ValidatorWeightDiffsPrefix, validatorsDB)
	validatorPublicKeyDiffsDB := prefixdb.New(ValidatorPublicKeyDiffsPrefix, validatorsDB)

	weightsCache, err := metercacher.New(
		"l1_validator_weights_cache",
		reg,
		lru.NewSizedCache(execCfg.L1WeightsCacheSize, func(ids.ID, uint64) int {
			return ids.IDLen + wrappers.LongLen
		}),
	)
	if err != nil {
		return nil, err
	}

	inactiveL1ValidatorsCache, err := metercacher.New(
		"l1_validator_inactive_cache",
		reg,
		lru.NewSizedCache(
			execCfg.L1InactiveValidatorsCacheSize,
			func(_ ids.ID, maybeL1Validator maybe.Maybe[L1Validator]) int {
				const (
					l1ValidatorOverhead      = ids.IDLen + ids.NodeIDLen + 4*wrappers.LongLen + 3*constants.PointerOverhead
					maybeL1ValidatorOverhead = wrappers.BoolLen + l1ValidatorOverhead
					entryOverhead            = ids.IDLen + maybeL1ValidatorOverhead
				)
				if maybeL1Validator.IsNothing() {
					return entryOverhead
				}

				l1Validator := maybeL1Validator.Value()
				return entryOverhead + len(l1Validator.PublicKey) + len(l1Validator.RemainingBalanceOwner) + len(l1Validator.DeactivationOwner)
			},
		),
	)
	if err != nil {
		return nil, err
	}

	chainIDNodeIDCache, err := metercacher.New(
		"l1_validator_chain_id_node_id_cache",
		reg,
		lru.NewSizedCache(execCfg.L1ChainIDNodeIDCacheSize, func(chainIDNodeID, bool) int {
			return ids.IDLen + ids.NodeIDLen + wrappers.BoolLen
		}),
	)
	if err != nil {
		return nil, err
	}

	txCache, err := metercacher.New(
		"tx_cache",
		reg,
		lru.NewSizedCache(execCfg.TxCacheSize, txAndStatusSize),
	)
	if err != nil {
		return nil, err
	}

	rewardUTXODB := prefixdb.New(RewardUTXOsPrefix, baseDB)
	rewardUTXOsCache, err := metercacher.New[ids.ID, []*lux.UTXO](
		"reward_utxos_cache",
		reg,
		lru.NewCache[ids.ID, []*lux.UTXO](execCfg.RewardUTXOsCacheSize),
	)
	if err != nil {
		return nil, err
	}

	utxoDB := prefixdb.New(UTXOPrefix, baseDB)
	utxoState, err := lux.NewMeteredUTXOState(utxoDB, txs.GenesisCodec, metricsReg, execCfg.ChecksumsEnabled)
	if err != nil {
		return nil, err
	}

	chainBaseDB := prefixdb.New(NetPrefix, baseDB)

	chainOwnerDB := prefixdb.New(NetOwnerPrefix, baseDB)
	chainOwnerCache, err := metercacher.New[ids.ID, fxOwnerAndSize](
		"chain_owner_cache",
		reg,
		lru.NewSizedCache(execCfg.FxOwnerCacheSize, func(_ ids.ID, f fxOwnerAndSize) int {
			return ids.IDLen + f.size
		}),
	)
	if err != nil {
		return nil, err
	}

	chainToL1ConversionDB := prefixdb.New(NetToL1ConversionPrefix, baseDB)
	chainToL1ConversionCache, err := metercacher.New[ids.ID, NetToL1Conversion](
		"chain_conversion_cache",
		reg,
		lru.NewSizedCache(execCfg.NetToL1ConversionCacheSize, func(_ ids.ID, c NetToL1Conversion) int {
			return 3*ids.IDLen + len(c.Addr)
		}),
	)
	if err != nil {
		return nil, err
	}

	transformedNetCache, err := metercacher.New(
		"transformed_chain_cache",
		reg,
		lru.NewSizedCache(execCfg.TransformedNetTxCacheSize, txSize),
	)
	if err != nil {
		return nil, err
	}

	supplyCache, err := metercacher.New[ids.ID, *uint64](
		"supply_cache",
		reg,
		lru.NewCache[ids.ID, *uint64](execCfg.ChainCacheSize),
	)
	if err != nil {
		return nil, err
	}

	chainCache, err := metercacher.New[ids.ID, []*txs.Tx](
		"chain_cache",
		reg,
		lru.NewCache[ids.ID, []*txs.Tx](execCfg.ChainCacheSize),
	)
	if err != nil {
		return nil, err
	}

	chainDBCache, err := metercacher.New[ids.ID, linkeddb.LinkedDB](
		"chain_db_cache",
		reg,
		lru.NewCache[ids.ID, linkeddb.LinkedDB](execCfg.ChainDBCacheSize),
	)
	if err != nil {
		return nil, err
	}

	chainNameCache, err := metercacher.New[string, ids.ID](
		"chain_name_cache",
		reg,
		lru.NewCache[string, ids.ID](execCfg.ChainCacheSize),
	)
	if err != nil {
		return nil, err
	}

	s := &state{
		validatorState: newValidatorState(),

		validators: validators,
		rt:         rt,
		upgrades:   upgrades,
		metrics:    metrics,
		rewards:    rewards,
		baseDB:     baseDB,

		addedBlockIDs: make(map[uint64]ids.ID),
		blockIDCache:  blockIDCache,
		blockIDDB:     prefixdb.New(BlockIDPrefix, baseDB),

		addedBlocks: make(map[ids.ID]block.Block),
		blockCache:  blockCache,
		blockDB:     prefixdb.New(BlockPrefix, baseDB),

		expiry:     btree.NewG(defaultTreeDegree, ExpiryEntry.Less),
		expiryDiff: newExpiryDiff(),
		expiryDB:   prefixdb.New(ExpiryReplayProtectionPrefix, baseDB),

		activeL1Validators: newActiveL1Validators(),
		l1ValidatorsDiff:   newL1ValidatorsDiff(),
		l1ValidatorsDB:     l1ValidatorsDB,
		weightsCache:       weightsCache,
		weightsDB:          prefixdb.New(WeightsPrefix, l1ValidatorsDB),
		chainIDNodeIDCache: chainIDNodeIDCache,
		chainIDNodeIDDB:    prefixdb.New(ChainIDNodeIDPrefix, l1ValidatorsDB),
		activeDB:           prefixdb.New(ActivePrefix, l1ValidatorsDB),
		inactiveCache:      inactiveL1ValidatorsCache,
		inactiveDB:         prefixdb.New(InactivePrefix, l1ValidatorsDB),

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		l1Validators: make(map[ids.ID]L1Validator),

		validatorsDB:              validatorsDB,
		currentValidatorsDB:       currentValidatorsDB,
		currentValidatorBaseDB:    currentValidatorBaseDB,
		currentValidatorList:      linkeddb.NewDefault(currentValidatorBaseDB),
		currentDelegatorBaseDB:    currentDelegatorBaseDB,
		currentDelegatorList:      linkeddb.NewDefault(currentDelegatorBaseDB),
		currentNetValidatorBaseDB: currentNetValidatorBaseDB,
		currentNetValidatorList:   linkeddb.NewDefault(currentNetValidatorBaseDB),
		currentNetDelegatorBaseDB: currentNetDelegatorBaseDB,
		currentNetDelegatorList:   linkeddb.NewDefault(currentNetDelegatorBaseDB),
		pendingValidatorsDB:       pendingValidatorsDB,
		pendingValidatorBaseDB:    pendingValidatorBaseDB,
		pendingValidatorList:      linkeddb.NewDefault(pendingValidatorBaseDB),
		pendingDelegatorBaseDB:    pendingDelegatorBaseDB,
		pendingDelegatorList:      linkeddb.NewDefault(pendingDelegatorBaseDB),
		pendingNetValidatorBaseDB: pendingNetValidatorBaseDB,
		pendingNetValidatorList:   linkeddb.NewDefault(pendingNetValidatorBaseDB),
		pendingNetDelegatorBaseDB: pendingNetDelegatorBaseDB,
		pendingNetDelegatorList:   linkeddb.NewDefault(pendingNetDelegatorBaseDB),
		validatorWeightDiffsDB:    validatorWeightDiffsDB,
		validatorPublicKeyDiffsDB: validatorPublicKeyDiffsDB,

		addedTxs: make(map[ids.ID]*txAndStatus),
		txDB:     prefixdb.New(TxPrefix, baseDB),
		txCache:  txCache,

		addedRewardUTXOs: make(map[ids.ID][]*lux.UTXO),
		rewardUTXODB:     rewardUTXODB,
		rewardUTXOsCache: rewardUTXOsCache,

		modifiedUTXOs: make(map[ids.ID]*lux.UTXO),
		utxoDB:        utxoDB,
		utxoState:     utxoState,

		chainBaseDB: chainBaseDB,
		chainDB:     linkeddb.NewDefault(chainBaseDB),

		chainOwners:     make(map[ids.ID]fx.Owner),
		chainOwnerDB:    chainOwnerDB,
		chainOwnerCache: chainOwnerCache,

		chainToL1Conversions:     make(map[ids.ID]NetToL1Conversion),
		chainToL1ConversionDB:    chainToL1ConversionDB,
		chainToL1ConversionCache: chainToL1ConversionCache,

		transformedNets:     make(map[ids.ID]*txs.Tx),
		transformedNetCache: transformedNetCache,
		transformedNetDB:    prefixdb.New(TransformedNetPrefix, baseDB),

		modifiedSupplies: make(map[ids.ID]uint64),
		supplyCache:      supplyCache,
		supplyDB:         prefixdb.New(SupplyPrefix, baseDB),

		addedChains:  make(map[ids.ID][]*txs.Tx),
		chainsDB:     prefixdb.New(ChainPrefix, baseDB),
		chainCache:   chainCache,
		chainDBCache: chainDBCache,

		addedChainNames: make(map[string]ids.ID),
		chainNameCache:  chainNameCache,
		chainNameDB:     prefixdb.New(ChainNamePrefix, baseDB),

		singletonDB: prefixdb.New(SingletonPrefix, baseDB),
	}

	if err := s.sync(genesisBytes); err != nil {
		return nil, errors.Join(
			err,
			s.Close(),
		)
	}

	return s, nil
}
