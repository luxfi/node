// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contracts

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// TODO: This is a stub file. The actual contract bindings need to be generated
// once solc is available. Run ./generate_abi_bindings.sh to generate the real bindings.

// EVMLoadSimulatorMetaData contains all meta data concerning the EVMLoadSimulator contract.
var EVMLoadSimulatorMetaData = &bind.MetaData{
	ABI: "[]", // TODO: Add actual ABI
}

// EVMLoadSimulator is an auto generated Go binding around an Ethereum contract.
type EVMLoadSimulator struct {
	EVMLoadSimulatorCaller     // Read-only binding to the contract
	EVMLoadSimulatorTransactor // Write-only binding to the contract
	EVMLoadSimulatorFilterer   // Log filterer for contract events
}

// EVMLoadSimulatorCaller is an auto generated read-only Go binding around an Ethereum contract.
type EVMLoadSimulatorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EVMLoadSimulatorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EVMLoadSimulatorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EVMLoadSimulatorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EVMLoadSimulatorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EVMLoadSimulatorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EVMLoadSimulatorSession struct {
	Contract     *EVMLoadSimulator // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EVMLoadSimulatorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EVMLoadSimulatorCallerSession struct {
	Contract *EVMLoadSimulatorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// EVMLoadSimulatorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EVMLoadSimulatorTransactorSession struct {
	Contract     *EVMLoadSimulatorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// EVMLoadSimulatorRaw is an auto generated low-level Go binding around an Ethereum contract.
type EVMLoadSimulatorRaw struct {
	Contract *EVMLoadSimulator // Generic contract binding to access the raw methods on
}

// EVMLoadSimulatorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EVMLoadSimulatorCallerRaw struct {
	Contract *EVMLoadSimulatorCaller // Generic read-only contract binding to access the raw methods on
}

// EVMLoadSimulatorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EVMLoadSimulatorTransactorRaw struct {
	Contract *EVMLoadSimulatorTransactor // Generic write-only contract binding to access the raw methods on
}

// DeployEVMLoadSimulator deploys a new Ethereum contract, binding an instance of EVMLoadSimulator to it.
func DeployEVMLoadSimulator(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *EVMLoadSimulator, error) {
	// TODO: Implement actual deployment
	return common.Address{}, nil, nil, errors.New("contract deployment not implemented - run generate_abi_bindings.sh")
}

// Stub methods for the contract
func (e *EVMLoadSimulator) SimulateReads(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateRandomWrite(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateModification(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateHashing(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateMemory(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateCallDepth(opts *bind.TransactOpts, count *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateContractCreation(opts *bind.TransactOpts) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulatePureCompute(opts *bind.TransactOpts, numIterations *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateLargeEvent(opts *bind.TransactOpts, numEvents *big.Int) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}

func (e *EVMLoadSimulator) SimulateExternalCall(opts *bind.TransactOpts) (*types.Transaction, error) {
	return nil, errors.New("contract method not implemented - run generate_abi_bindings.sh")
}