// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/math"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/platformvm/warp/payload"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/chains/atomic"
)

// RegisterL1ValidatorTxExpiryWindow bounds the maximum number of tracked
// expiries. The window is 1 day, which limits expiry set size.
const (
	second                            = 1
	minute                            = 60 * second
	hour                              = 60 * minute
	day                               = 24 * hour
	RegisterL1ValidatorTxExpiryWindow = day
)

var (
	_ txs.Visitor = (*standardTxExecutor)(nil)

	errEmptyNodeID                     = errors.New("validator nodeID cannot be empty")
	errTransformChainTxNotPermitted    = errors.New("TransformChainTx is not permitted")
	errMaxNumActiveValidators          = errors.New("already at the max number of active validators")
	errCouldNotLoadChainToL1Conversion = errors.New("could not load chain conversion")
	errWrongWarpMessageSourceChainID   = errors.New("wrong warp message source chain ID")
	errWrongWarpMessageSourceAddress   = errors.New("wrong warp message source address")
	errWarpMessageExpired              = errors.New("warp message expired")
	errWarpMessageNotYetAllowed        = errors.New("warp message not yet allowed")
	errWarpMessageAlreadyIssued        = errors.New("warp message already issued")
	errCouldNotLoadL1Validator         = errors.New("could not load L1 validator")
	errWarpMessageContainsStaleNonce   = errors.New("warp message contains stale nonce")
	errRemovingLastValidator           = errors.New("attempting to remove the last L1 validator from a converted chain")
	errStateCorruption                 = errors.New("state corruption")
)

// StandardTx executes the standard transaction [tx].
//
// [state] is modified to represent the state of the chain after the execution
// of [tx].
//
// Returns:
//   - The IDs of any import UTXOs consumed.
//   - The, potentially nil, atomic requests that should be performed against
//     shared memory when this transaction is accepted.
//   - A, potentially nil, function that should be called when this transaction
//     is accepted.
func StandardTx(
	backend *Backend,
	feeCalculator fee.Calculator,
	tx *txs.Tx,
	state state.Diff,
) (set.Set[ids.ID], map[ids.ID]*atomic.Requests, func(), error) {
	standardExecutor := standardTxExecutor{
		backend:       backend,
		feeCalculator: feeCalculator,
		tx:            tx,
		state:         state,
	}
	if err := tx.Unsigned.Visit(&standardExecutor); err != nil {
		txID := tx.ID()
		return nil, nil, nil, fmt.Errorf("standard tx %s failed execution: %w", txID, err)
	}
	return standardExecutor.inputs, standardExecutor.atomicRequests, standardExecutor.onAccept, nil
}

type standardTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	backend       *Backend
	state         state.Diff // state is expected to be modified
	feeCalculator fee.Calculator
	tx            *txs.Tx

	// outputs of visitor execution
	onAccept       func() // may be nil
	inputs         set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*standardTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (e *standardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if tx.Validator().NodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}

	if _, err := verifyAddValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())

	return nil
}

func (e *standardTxExecutor) AddChainValidatorTx(tx *txs.AddChainValidatorTx) error {
	if err := verifyAddChainValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

func (e *standardTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	if _, err := verifyAddDelegatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

func (e *standardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// Verify chain name uniqueness (case-insensitive)
	if tx.BlockchainName() != "" && e.state.IsChainNameTaken(tx.BlockchainName()) {
		return fmt.Errorf("chain name %q is already taken", tx.BlockchainName())
	}

	baseTxCreds, err := verifyPoAChainAuthorization(e.backend.Fx, e.state, e.tx, tx.ChainID(), tx.ChainAuth())
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		baseTxCreds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	// Add the new chain to the database
	e.state.AddChain(e.tx)

	// If this proposal is committed and this node is a member of the chain
	// that validates the blockchain, create the blockchain
	e.onAccept = func() {
		e.backend.Config.CreateChain(txID, tx)
	}
	return nil
}

func (e *standardTxExecutor) CreateNetworkTx(tx *txs.CreateNetworkTx) error {
	// Make sure this transaction is well formed.
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	// Add the new network to the database
	e.state.AddNet(txID)
	e.state.SetNetOwner(txID, tx.Owner())

	// A sovereign network runs its OWN validator set: seed that set and record
	// its manager authority now. The new network's id IS this tx's id, so the
	// L1 validators are keyed under txID. ManagerChainID names the chain hosting
	// a Contract-governed staking contract (ids.Empty ⇒ P-Chain-governed, owner
	// is the authority). Chains themselves are added by CreateChainTx.
	if tx.Security().Sovereign() {
		if err := registerOwnSet(e, txID, tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()); err != nil {
			return err
		}
	}
	return nil
}

func (e *standardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	e.inputs = set.NewSet[ids.ID](len(tx.ImportedInputs()))
	utxoIDs := make([][]byte, len(tx.ImportedInputs()))
	for i, in := range tx.ImportedInputs() {
		utxoID := in.UTXOID.InputID()

		e.inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	// Skip verification of the shared memory inputs if the other primary
	// network chains are not guaranteed to be up-to-date.
	var allUTXOBytes [][]byte
	if e.backend.Bootstrapped.Get() {
		if err := verify.SameChain(context.TODO(), e.backend.Runtime, tx.SourceChain()); err != nil {
			return err
		}

		if e.backend.Runtime.SharedMemory != nil {
			if sm, ok := e.backend.Runtime.SharedMemory.(atomic.SharedMemory); ok {
				var err error
				allUTXOBytes, err = sm.Get(tx.SourceChain(), utxoIDs)
				if err != nil {
					return fmt.Errorf("failed to get shared memory: %w", err)
				}
			}
		}

		utxos := make([]*lux.UTXO, len(tx.Inputs())+len(tx.ImportedInputs()))
		for index, input := range tx.Inputs() {
			utxo, err := e.state.GetUTXO(input.InputID())
			if err != nil {
				return fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
			}
			utxos[index] = utxo
		}
		for i, utxoBytes := range allUTXOBytes {
			utxo, err := lux.ParseUTXO(utxoBytes)
			if err != nil {
				return fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxos[i+len(tx.Inputs())] = utxo
		}

		ins := make([]*lux.TransferableInput, len(tx.Inputs())+len(tx.ImportedInputs()))
		copy(ins, tx.Inputs())
		copy(ins[len(tx.Inputs()):], tx.ImportedInputs())

		// Verify the flowcheck
		fee, err := e.feeCalculator.CalculateFee(tx)
		if err != nil {
			return err
		}
		if err := e.backend.FlowChecker.VerifySpendUTXOs(
			tx,
			utxos,
			ins,
			tx.Outputs(),
			e.tx.Creds,
			map[ids.ID]uint64{
				e.backend.Runtime.UTXOAssetID: fee,
			},
		); err != nil {
			return err
		}
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	e.atomicRequests = map[ids.ID]*atomic.Requests{
		tx.SourceChain(): {
			RemoveRequests: utxoIDs,
		},
	}
	return nil
}

func (e *standardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	outs := make([]*lux.TransferableOutput, len(tx.Outputs())+len(tx.ExportedOutputs()))
	copy(outs, tx.Outputs())
	copy(outs[len(tx.Outputs()):], tx.ExportedOutputs())

	if e.backend.Bootstrapped.Get() {
		if err := verify.SameChain(context.TODO(), e.backend.Runtime, tx.DestinationChain()); err != nil {
			return err
		}
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		outs,
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("failed verifySpend: %w", err)
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	elems := make([]*atomic.Element, len(tx.ExportedOutputs()))
	for i, out := range tx.ExportedOutputs() {
		utxo := &lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outputs()) + i),
			},
			Asset: lux.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := utxo.WireBytes()
		if err != nil {
			return fmt.Errorf("failed to marshal UTXO: %w", err)
		}
		utxoID := utxo.InputID()
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(lux.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	e.atomicRequests = map[ids.ID]*atomic.Requests{
		tx.DestinationChain(): {
			PutRequests: elems,
		},
	}
	return nil
}

// Verifies a [*txs.RemoveChainValidatorTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyRemoveChainValidatorTx]. This
// transaction will result in [tx.NodeID] being removed as a validator of
// [tx.ChainID()].
// Note: [tx.NodeID] may be either a current or pending validator.
func (e *standardTxExecutor) RemoveChainValidatorTx(tx *txs.RemoveChainValidatorTx) error {
	staker, isCurrentValidator, err := verifyRemoveChainValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	)
	if err != nil {
		return err
	}

	if isCurrentValidator {
		e.state.DeleteCurrentValidator(staker)
	} else {
		e.state.DeletePendingValidator(staker)
	}

	// Invariant: There are no permissioned net delegators to remove.

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())

	return nil
}

func (e *standardTxExecutor) TransformChainTx(*txs.TransformChainTx) error {
	// TransformChainTx is permanently rejected: it has no role under
	// activate-all-implicitly. Any historical TransformChainTx in genesis is
	// already applied; live submissions are always refused.
	return errTransformChainTxNotPermitted
}

func (e *standardTxExecutor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if err := verifyAddPermissionlessValidatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())

	return nil
}

func (e *standardTxExecutor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if err := verifyAddPermissionlessDelegatorTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

// Verifies a [*txs.TransferChainOwnershipTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyTransferChainOwnershipTx].
// This transaction will result in the ownership of [tx.Chain()] being transferred
// to [tx.Owner()].
func (e *standardTxExecutor) TransferChainOwnershipTx(tx *txs.TransferChainOwnershipTx) error {
	err := verifyTransferChainOwnershipTx(
		e.backend,
		e.feeCalculator,
		e.state,
		e.tx,
		tx,
	)
	if err != nil {
		return err
	}

	e.state.SetNetOwner(tx.Chain(), tx.Owner())

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Inputs())
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

func (e *standardTxExecutor) BaseTx(tx *txs.BaseTx) error {
	// Verify the tx is well-formed
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()
	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

// registerOwnSet folds the establishment of a network's OWN validator set: it
// seeds every genesis L1 validator into state (keyed under the network id [id],
// which is each validator's ChainID) and records the set's on-chain manager
// authority. It is the single shared primitive behind both the ∅→Network
// constructor (CreateNetworkTx) and the Network→Network promote endomorphism
// (ConvertNetworkTx), so a sovereign set is established exactly one way.
//
// [managerChainID]+[managerAddress] name the validator-manager contract (empty
// for a P-Chain-governed set). A non-zero validator Balance activates the
// validator and prepays its continuously-charged EndAccumulatedFee; the LUX
// backing that balance is spent by the CALLER's flow check (registerOwnSet only
// mutates state — it does not charge inputs), preserving the legacy accounting.
func registerOwnSet(
	e *standardTxExecutor,
	id ids.ID,
	vdrs []*txs.NetworkValidator,
	sec security.Mode,
	managerChainID ids.ID,
	managerAddress []byte,
) error {
	// A contract-governed set must name its manager; SyntacticVerify already
	// enforces this, but registerOwnSet stays self-contained.
	if sec.Manager == security.Contract && (managerChainID == ids.Empty || len(managerAddress) == 0) {
		return txs.ErrContractManagerNeedsAddress
	}

	startTime := uint64(e.state.GetTimestamp().Unix())
	currentFees := e.state.GetAccruedFees()
	conversionData := message.ChainToL1ConversionData{
		ChainID:        id,
		ManagerChainID: managerChainID,
		ManagerAddress: managerAddress,
		Validators:     make([]message.ChainToL1ConversionValidatorData, len(vdrs)),
	}
	for i, vdr := range vdrs {
		nodeID, err := ids.ToNodeID(vdr.NodeID)
		if err != nil {
			return err
		}

		// [vdrs] is a FRESH decode of the tx buffer, so Key() — which Verify()
		// populates — is nil here. This is the call Verify() makes to fill it,
		// on the same bytes, so it yields the same key; what it leaves out is
		// the possession pairing. Both callers run SyntacticVerify on this
		// unsigned tx first, and that already proves possession for every
		// validator against the buffer this decode reads, so pairing again
		// would just pay it twice on every node executing the block —
		// ~17x this call, and nothing caps the validator count below 2MiB.
		//
		// Still fail-closed: a key that does not parse is refused. A validator
		// stored without a usable key carries its weight in the quorum
		// denominator but can never sign a vote toward it, so a set seeded
		// keyless cannot reach finality.
		publicKey, err := bls.PublicKeyFromCompressedBytes(vdr.Signer.PublicKey[:])
		if err != nil {
			return err
		}

		// Owner blobs are the native standalone-owner encoding (txs.MarshalOwner
		// / txs.UnmarshalOwner) — the same bytes the P-Chain state DB stores; no
		// codec. message.PChainOwner maps directly onto secp256k1fx.OutputOwners.
		remainingBalanceOwner, err := txs.MarshalOwner(&secp256k1fx.OutputOwners{
			Threshold: vdr.RemainingBalanceOwner.Threshold,
			Addrs:     vdr.RemainingBalanceOwner.Addresses,
		})
		if err != nil {
			return err
		}
		deactivationOwner, err := txs.MarshalOwner(&secp256k1fx.OutputOwners{
			Threshold: vdr.DeactivationOwner.Threshold,
			Addrs:     vdr.DeactivationOwner.Addresses,
		})
		if err != nil {
			return err
		}

		l1Validator := state.L1Validator{
			ValidationID:          id.Append(uint32(i)),
			ChainID:               id,
			NodeID:                nodeID,
			PublicKey:             bls.PublicKeyToUncompressedBytes(publicKey),
			RemainingBalanceOwner: remainingBalanceOwner,
			DeactivationOwner:     deactivationOwner,
			StartTime:             startTime,
			Weight:                vdr.Weight,
			MinNonce:              0,
			EndAccumulatedFee:     0, // If Balance is 0, the validator stays inactive.
		}
		if vdr.Balance != 0 {
			// Activating a validator consumes active-set capacity and prepays
			// its fee out of the accrued-fee clock.
			if gas.Gas(e.state.NumActiveL1Validators()) >= e.backend.Config.ValidatorFeeConfig.Capacity {
				return errMaxNumActiveValidators
			}
			endAccumulatedFee, err := math.Add(vdr.Balance, currentFees)
			if err != nil {
				return err
			}
			l1Validator.EndAccumulatedFee = endAccumulatedFee
		}

		if err := e.state.PutL1Validator(l1Validator); err != nil {
			return err
		}

		conversionData.Validators[i] = message.ChainToL1ConversionValidatorData{
			NodeID:       vdr.NodeID,
			BLSPublicKey: vdr.Signer.PublicKey,
			Weight:       vdr.Weight,
		}
	}

	// Record the set's manager authority (the warp-message source authorized to
	// mutate the set), keyed by the network id. Byte-for-byte the legacy
	// ConvertNetworkToL1Tx tail.
	conversionID, err := message.ChainToL1ConversionID(conversionData)
	if err != nil {
		return err
	}
	e.state.SetNetToL1Conversion(id, state.NetToL1Conversion{
		ConversionID: conversionID,
		ChainID:      managerChainID,
		Addr:         managerAddress,
	})
	return nil
}

// ConvertNetworkTx PROMOTES an existing network to sovereign: it establishes
// the network's OWN validator set + manager authority (and LP-77 prepays each
// activated validator's balance). This is the fold of the former
// ConvertNetworkToL1Tx onto the decomplected security.Mode model — the
// Network→Network endomorphism paired with CreateNetworkTx's ∅→Network
// constructor. Behavior is byte-for-byte the legacy convert: the promotion is
// authorized by the existing network owner, every validator + the manager are
// registered via the shared registerOwnSet primitive, and the tx spends the
// base fee plus every activated validator's prepaid balance.
func (e *standardTxExecutor) ConvertNetworkTx(tx *txs.ConvertNetworkTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// The existing network owner must authorize the promotion.
	baseTxCreds, err := verifyPoAChainAuthorization(e.backend.Fx, e.state, e.tx, tx.Network(), tx.Auth())
	if err != nil {
		return err
	}

	// Verify the flowcheck. Each activated validator's balance is charged on top
	// of the base fee — it funds that validator's continuously-charged
	// EndAccumulatedFee, so the backing LUX must be spent here (mirrors the
	// legacy convert; without it, LUX would be minted).
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	for _, vdr := range tx.Validators() {
		if vdr.Balance != 0 {
			fee, err = math.Add(fee, vdr.Balance)
			if err != nil {
				return err
			}
		}
	}

	// Establish the own validator set + record the manager authority.
	if err := registerOwnSet(e, tx.Network(), tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()); err != nil {
		return err
	}

	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		baseTxCreds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

func (e *standardTxExecutor) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	currentTimestamp := e.state.GetTimestamp()

	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}
	fee, err = math.Add(fee, tx.Balance())
	if err != nil {
		return err
	}

	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	// Parse the warp message.
	warpMessage, err := warp.ParseMessage(tx.Message())
	if err != nil {
		return err
	}
	addressedCall, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}
	msg, err := message.ParseRegisterL1Validator(addressedCall.Payload)
	if err != nil {
		return err
	}
	if err := msg.Verify(); err != nil {
		return err
	}

	// Verify that the warp message was sent from the expected chain and
	// address.
	if err := verifyL1Conversion(e.state, msg.ChainID, warpMessage.SourceChainID, addressedCall.SourceAddress); err != nil {
		return err
	}

	// Verify that the message contains a valid expiry time.
	currentTimestampUnix := uint64(currentTimestamp.Unix())
	if msg.Expiry <= currentTimestampUnix {
		return fmt.Errorf("%w at %d and it is currently %d", errWarpMessageExpired, msg.Expiry, currentTimestampUnix)
	}
	if secondsUntilExpiry := msg.Expiry - currentTimestampUnix; secondsUntilExpiry > RegisterL1ValidatorTxExpiryWindow {
		return fmt.Errorf("%w because time is %d seconds in the future but the limit is %d", errWarpMessageNotYetAllowed, secondsUntilExpiry, RegisterL1ValidatorTxExpiryWindow)
	}

	// Verify that this warp message isn't being replayed.
	validationID := msg.ValidationID()
	expiry := state.ExpiryEntry{
		Timestamp:    msg.Expiry,
		ValidationID: validationID,
	}
	isDuplicate, err := e.state.HasExpiry(expiry)
	if err != nil {
		return err
	}
	if isDuplicate {
		return fmt.Errorf("%w for validationID %s", errWarpMessageAlreadyIssued, validationID)
	}

	// Verify proof of possession provided by the transaction against the public
	// key provided by the warp message.
	pop := signer.ProofOfPossession{
		PublicKey:         msg.BLSPublicKey,
		ProofOfPossession: tx.ProofOfPossession(),
	}
	if err := pop.Verify(); err != nil {
		return err
	}

	// Create the L1 validator.
	nodeID, err := ids.ToNodeID(msg.NodeID)
	if err != nil {
		return err
	}
	remainingBalanceOwner, err := txs.MarshalOwner(&secp256k1fx.OutputOwners{
		Threshold: msg.RemainingBalanceOwner.Threshold,
		Addrs:     msg.RemainingBalanceOwner.Addresses,
	})
	if err != nil {
		return err
	}
	deactivationOwner, err := txs.MarshalOwner(&secp256k1fx.OutputOwners{
		Threshold: msg.DisableOwner.Threshold,
		Addrs:     msg.DisableOwner.Addresses,
	})
	if err != nil {
		return err
	}
	l1Validator := state.L1Validator{
		ValidationID:          validationID,
		ChainID:               msg.ChainID,
		NodeID:                nodeID,
		PublicKey:             bls.PublicKeyToUncompressedBytes(pop.Key()),
		RemainingBalanceOwner: remainingBalanceOwner,
		DeactivationOwner:     deactivationOwner,
		StartTime:             currentTimestampUnix,
		Weight:                msg.Weight,
		MinNonce:              0,
		EndAccumulatedFee:     0, // If Balance is 0, this is will remain 0
	}

	// If the balance is non-zero, this validator should be initially active.
	if tx.Balance() != 0 {
		// Verify that there is space for an active validator.
		if gas.Gas(e.state.NumActiveL1Validators()) >= e.backend.Config.ValidatorFeeConfig.Capacity {
			return errMaxNumActiveValidators
		}

		// Mark the validator as active.
		currentFees := e.state.GetAccruedFees()
		l1Validator.EndAccumulatedFee, err = math.Add(tx.Balance(), currentFees)
		if err != nil {
			return err
		}
	}

	if err := e.state.PutL1Validator(l1Validator); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	// Prevent this warp message from being replayed
	e.state.PutExpiry(expiry)
	return nil
}

func (e *standardTxExecutor) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	// Parse the warp message.
	warpMessage, err := warp.ParseMessage(tx.Message())
	if err != nil {
		return err
	}
	addressedCall, err := payload.ParseAddressedCall(warpMessage.Payload)
	if err != nil {
		return err
	}
	msg, err := message.ParseL1ValidatorWeight(addressedCall.Payload)
	if err != nil {
		return err
	}
	if err := msg.Verify(); err != nil {
		return err
	}

	// Verify that the message contains a valid nonce for a current validator.
	l1Validator, err := e.state.GetL1Validator(msg.ValidationID)
	if err != nil {
		return fmt.Errorf("%w: %w", errCouldNotLoadL1Validator, err)
	}
	if msg.Nonce < l1Validator.MinNonce {
		return fmt.Errorf("%w %d must be at least %d", errWarpMessageContainsStaleNonce, msg.Nonce, l1Validator.MinNonce)
	}

	// Verify that the warp message was sent from the expected chain and
	// address.
	if err := verifyL1Conversion(e.state, l1Validator.ChainID, warpMessage.SourceChainID, addressedCall.SourceAddress); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Check if we are removing the validator.
	if msg.Weight == 0 {
		// Verify that we are not removing the last validator.
		weight, err := e.state.WeightOfL1Validators(l1Validator.ChainID)
		if err != nil {
			return fmt.Errorf("could not load L1 validator weights: %w", err)
		}
		if weight == l1Validator.Weight {
			return errRemovingLastValidator
		}

		// If the validator is currently active, we need to refund the remaining
		// balance.
		if l1Validator.EndAccumulatedFee != 0 {
			remOwner, err := txs.UnmarshalOwner(l1Validator.RemainingBalanceOwner)
			if err != nil {
				return fmt.Errorf("%w: remaining balance owner is malformed", errStateCorruption)
			}
			remainingBalanceOwner := message.PChainOwner{Threshold: remOwner.Threshold, Addresses: remOwner.Addrs}

			accruedFees := e.state.GetAccruedFees()
			if l1Validator.EndAccumulatedFee <= accruedFees {
				// This check should be unreachable. However, it prevents LUX
				// from being minted due to state corruption. This also prevents
				// invalid UTXOs from being created (with 0 value).
				return fmt.Errorf("%w: validator should have already been disabled", errStateCorruption)
			}
			remainingBalance := l1Validator.EndAccumulatedFee - accruedFees

			utxo := &lux.UTXO{
				UTXOID: lux.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(tx.Outputs())),
				},
				Asset: lux.Asset{
					ID: e.backend.Runtime.UTXOAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: remainingBalance,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: remainingBalanceOwner.Threshold,
						Addrs:     remainingBalanceOwner.Addresses,
					},
				},
			}
			e.state.AddUTXO(utxo)
		}
	}

	// If the weight is being set to 0, it is possible for the nonce increment
	// to overflow. However, the validator is being removed and the nonce
	// doesn't matter. If weight is not 0, [msg.Nonce] is enforced by
	// [msg.Verify()] to be less than MaxUInt64 and can therefore be incremented
	// without overflow.
	l1Validator.MinNonce = msg.Nonce + 1
	l1Validator.Weight = msg.Weight
	if err := e.state.PutL1Validator(l1Validator); err != nil {
		return err
	}

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

func (e *standardTxExecutor) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	fee, err = math.Add(fee, tx.Balance())
	if err != nil {
		return err
	}

	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		e.tx.Creds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	l1Validator, err := e.state.GetL1Validator(tx.ValidationID())
	if err != nil {
		return err
	}

	// If the validator is currently inactive, we are activating it.
	if l1Validator.EndAccumulatedFee == 0 {
		if gas.Gas(e.state.NumActiveL1Validators()) >= e.backend.Config.ValidatorFeeConfig.Capacity {
			return errMaxNumActiveValidators
		}

		l1Validator.EndAccumulatedFee = e.state.GetAccruedFees()
	}
	l1Validator.EndAccumulatedFee, err = math.Add(l1Validator.EndAccumulatedFee, tx.Balance())
	if err != nil {
		return err
	}

	if err := e.state.PutL1Validator(l1Validator); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())
	return nil
}

func (e *standardTxExecutor) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo(), true); err != nil {
		return err
	}

	l1Validator, err := e.state.GetL1Validator(tx.ValidationID())
	if err != nil {
		return fmt.Errorf("%w: %w", errCouldNotLoadL1Validator, err)
	}

	disOwner, err := txs.UnmarshalOwner(l1Validator.DeactivationOwner)
	if err != nil {
		return err
	}
	disableOwner := message.PChainOwner{Threshold: disOwner.Threshold, Addresses: disOwner.Addrs}

	baseTxCreds, err := verifyAuthorization(
		e.backend.Fx,
		e.tx,
		&secp256k1fx.OutputOwners{
			Threshold: disableOwner.Threshold,
			Addrs:     disableOwner.Addresses,
		},
		tx.DisableAuth(),
	)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	fee, err := e.feeCalculator.CalculateFee(tx)
	if err != nil {
		return err
	}

	if err := e.backend.FlowChecker.VerifySpend(
		tx,
		e.state,
		tx.Inputs(),
		tx.Outputs(),
		baseTxCreds,
		map[ids.ID]uint64{
			e.backend.Runtime.UTXOAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.tx.ID()

	// Consume the UTXOS
	lux.Consume(e.state, tx.Inputs())
	// Produce the UTXOS
	lux.Produce(e.state, txID, tx.Outputs())

	// If the validator is already disabled, there is nothing to do.
	if l1Validator.EndAccumulatedFee == 0 {
		return nil
	}

	remOwner, err := txs.UnmarshalOwner(l1Validator.RemainingBalanceOwner)
	if err != nil {
		return err
	}
	remainingBalanceOwner := message.PChainOwner{Threshold: remOwner.Threshold, Addresses: remOwner.Addrs}

	accruedFees := e.state.GetAccruedFees()
	if l1Validator.EndAccumulatedFee <= accruedFees {
		// This check should be unreachable. However, including it ensures
		// that LUX can't get minted out of thin air due to state
		// corruption.
		return fmt.Errorf("%w: validator should have already been disabled", errStateCorruption)
	}
	remainingBalance := l1Validator.EndAccumulatedFee - accruedFees

	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{
			TxID:        txID,
			OutputIndex: uint32(len(tx.Outputs())),
		},
		Asset: lux.Asset{
			ID: e.backend.Runtime.UTXOAssetID,
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: remainingBalance,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: remainingBalanceOwner.Threshold,
				Addrs:     remainingBalanceOwner.Addresses,
			},
		},
	}
	e.state.AddUTXO(utxo)

	// Disable the validator
	l1Validator.EndAccumulatedFee = 0
	return e.state.PutL1Validator(l1Validator)
}

// Creates the staker as defined in [stakerTx] and adds it to [e.State].
func (e *standardTxExecutor) putStaker(stakerTx txs.Staker) error {
	var (
		chainTime = e.state.GetTimestamp()
		txID      = e.tx.ID()
		staker    *state.Staker
		err       error
	)

	// Only calculate the potentialReward for permissionless stakers.
	// Recall that we only need to check if this is a permissioned
	// validator as there are no permissioned delegators
	var potentialReward uint64
	if !stakerTx.CurrentPriority().IsPermissionedValidator() {
		chainID := stakerTx.ChainID()
		currentSupply, err := e.state.GetCurrentSupply(chainID)
		if err != nil {
			return err
		}

		rewards, err := GetRewardsCalculator(e.backend, e.state, chainID)
		if err != nil {
			return err
		}

		// Stakers are immediately added to the current staker set. Their
		// [StartTime] is the current chain time.
		stakeDuration := stakerTx.EndTime().Sub(chainTime)
		potentialReward = rewards.Calculate(
			stakeDuration,
			stakerTx.Weight(),
			currentSupply,
		)

		e.state.SetCurrentSupply(chainID, currentSupply+potentialReward)
	}

	staker, err = state.NewCurrentStaker(txID, stakerTx, chainTime, potentialReward)
	if err != nil {
		return err
	}

	switch priority := staker.Priority; {
	case priority.IsCurrentValidator():
		if err := e.state.PutCurrentValidator(staker); err != nil {
			return err
		}
	case priority.IsCurrentDelegator():
		e.state.PutCurrentDelegator(staker)
	case priority.IsPendingValidator():
		if err := e.state.PutPendingValidator(staker); err != nil {
			return err
		}
	case priority.IsPendingDelegator():
		e.state.PutPendingDelegator(staker)
	default:
		return fmt.Errorf("staker %s, unexpected priority %d", staker.TxID, priority)
	}
	return nil
}

// verifyL1Conversion verifies that the L1 conversion of [chainID] references
// the [expectedChainID] and [expectedAddress].
func verifyL1Conversion(
	state state.Chain,
	chainID ids.ID,
	expectedChainID ids.ID,
	expectedAddress []byte,
) error {
	chainToL1Conversion, err := state.GetNetToL1Conversion(chainID)
	if err != nil {
		return fmt.Errorf("%w for %s with: %w", errCouldNotLoadChainToL1Conversion, chainID, err)
	}
	if expectedChainID != chainToL1Conversion.ChainID {
		return fmt.Errorf("%w expected %s but had %s", errWrongWarpMessageSourceChainID, chainToL1Conversion.ChainID, expectedChainID)
	}
	if !bytes.Equal(expectedAddress, chainToL1Conversion.Addr) {
		return fmt.Errorf("%w expected 0x%x but got 0x%x", errWrongWarpMessageSourceAddress, chainToL1Conversion.Addr, expectedAddress)
	}
	return nil
}
