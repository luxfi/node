// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

<<<<<<< HEAD
<<<<<<< HEAD
var (
	_ Set = (*vdrSet)(nil)
=======
var (
	_ Set = (*set)(nil)
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))

	errZeroWeight         = errors.New("weight must be non-zero")
	errDuplicateValidator = errors.New("duplicate validator")
	errMissingValidator   = errors.New("missing validator")
)
<<<<<<< HEAD
=======
var _ Set = (*set)(nil)
>>>>>>> 1437bfe45 (Remove validators.Set#Set from the interface (#2275))
=======
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))

// Set of validators that can be sampled
type Set interface {
	formatting.PrefixedStringer

<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	// Add a new staker to the set.
	// Returns an error if:
	// - [weight] is 0
	// - [nodeID] is already in the validator set
	// - the total weight of the validator set would overflow uint64
	// If an error is returned, the set will be unmodified.
<<<<<<< HEAD
	Add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error
=======
	Add(nodeID ids.NodeID, weight uint64) error
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))

	// AddWeight to an existing staker.
	// Returns an error if:
	// - [weight] is 0
	// - [nodeID] is not already in the validator set
	// - the total weight of the validator set would overflow uint64
	// If an error is returned, the set will be unmodified.
	AddWeight(nodeID ids.NodeID, weight uint64) error
<<<<<<< HEAD
=======
	// AddWeight to a staker.
	AddWeight(ids.NodeID, uint64) error
>>>>>>> 1437bfe45 (Remove validators.Set#Set from the interface (#2275))
=======
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))

	// GetWeight retrieves the validator weight from the set.
	GetWeight(ids.NodeID) uint64

	// Get returns the validator tied to the specified ID.
<<<<<<< HEAD
	Get(ids.NodeID) (*Validator, bool)

	// SubsetWeight returns the sum of the weights of the validators.
	SubsetWeight(set.Set[ids.NodeID]) uint64
=======
	Get(ids.NodeID) (Validator, bool)

	// SubsetWeight returns the sum of the weights of the validators.
	SubsetWeight(ids.NodeIDSet) uint64
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))

	// RemoveWeight from a staker. If the staker's weight becomes 0, the staker
	// will be removed from the validator set.
	// Returns an error if:
	// - [weight] is 0
	// - [nodeID] is not already in the validator set
	// - the weight of the validator would become negative
	// If an error is returned, the set will be unmodified.
	RemoveWeight(nodeID ids.NodeID, weight uint64) error

	// Contains returns true if there is a validator with the specified ID
	// currently in the set.
	Contains(ids.NodeID) bool

	// Len returns the number of validators currently in the set.
	Len() int

	// List all the validators in this group
	List() []*Validator

	// Weight returns the cumulative weight of all validators in the set.
	Weight() uint64

	// Sample returns a collection of validatorIDs, potentially with duplicates.
	// If sampling the requested size isn't possible, an error will be returned.
	Sample(size int) ([]ids.NodeID, error)

	// When a validator's weight changes, or a validator is added/removed,
	// this listener is called.
	RegisterCallbackListener(SetCallbackListener)
}

type SetCallbackListener interface {
	OnValidatorAdded(validatorID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64)
	OnValidatorRemoved(validatorID ids.NodeID, weight uint64)
	OnValidatorWeightChanged(validatorID ids.NodeID, oldWeight, newWeight uint64)
}

// NewSet returns a new, empty set of validators.
func NewSet() Set {
	return &vdrSet{
		vdrs:    make(map[ids.NodeID]*Validator),
		sampler: sampler.NewWeightedWithoutReplacement(),
	}
}

// NewBestSet returns a new, empty set of validators.
func NewBestSet(expectedSampleSize int) Set {
	return &vdrSet{
		vdrs:    make(map[ids.NodeID]*Validator),
		sampler: sampler.NewBestWeightedWithoutReplacement(expectedSampleSize),
	}
}

<<<<<<< HEAD
type vdrSet struct {
=======
type set struct {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	lock        sync.RWMutex
	vdrs        map[ids.NodeID]*Validator
	vdrSlice    []*Validator
	weights     []uint64
	totalWeight uint64

	samplerInitialized bool
	sampler            sampler.WeightedWithoutReplacement

	callbackListeners []SetCallbackListener
}

<<<<<<< HEAD
<<<<<<< HEAD
func (s *vdrSet) Add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
=======
func (s *set) AddWeight(vdrID ids.NodeID, weight uint64) error {
>>>>>>> 1437bfe45 (Remove validators.Set#Set from the interface (#2275))
=======
func (s *set) Add(nodeID ids.NodeID, weight uint64) error {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	if weight == 0 {
		return errZeroWeight
	}

	s.lock.Lock()
	defer s.lock.Unlock()

<<<<<<< HEAD
	return s.add(nodeID, pk, txID, weight)
}

func (s *vdrSet) add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
=======
	return s.add(nodeID, weight)
}

func (s *set) add(nodeID ids.NodeID, weight uint64) error {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	_, nodeExists := s.vdrs[nodeID]
	if nodeExists {
		return errDuplicateValidator
	}

	// We first calculate the new total weight of the set, as this guarantees
	// that none of the following operations can overflow.
	newTotalWeight, err := math.Add64(s.totalWeight, weight)
	if err != nil {
		return err
	}

<<<<<<< HEAD
	vdr := &Validator{
		NodeID:    nodeID,
		PublicKey: pk,
		TxID:      txID,
		Weight:    weight,
		index:     len(s.vdrSlice),
=======
	vdr := &validator{
		nodeID: nodeID,
		weight: weight,
		index:  len(s.vdrSlice),
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	}
	s.vdrs[nodeID] = vdr
	s.vdrSlice = append(s.vdrSlice, vdr)
	s.weights = append(s.weights, weight)
	s.totalWeight = newTotalWeight
	s.samplerInitialized = false

<<<<<<< HEAD
	s.callValidatorAddedCallbacks(nodeID, pk, txID, weight)
	return nil
}

func (s *vdrSet) AddWeight(nodeID ids.NodeID, weight uint64) error {
=======
	s.callValidatorAddedCallbacks(nodeID, weight)
	return nil
}

func (s *set) AddWeight(nodeID ids.NodeID, weight uint64) error {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	if weight == 0 {
		return errZeroWeight
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	return s.addWeight(nodeID, weight)
}

<<<<<<< HEAD
func (s *vdrSet) addWeight(nodeID ids.NodeID, weight uint64) error {
=======
func (s *set) addWeight(nodeID ids.NodeID, weight uint64) error {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	vdr, nodeExists := s.vdrs[nodeID]
	if !nodeExists {
		return errMissingValidator
	}

	// We first calculate the new total weight of the set, as this guarantees
	// that none of the following operations can overflow.
	newTotalWeight, err := math.Add64(s.totalWeight, weight)
	if err != nil {
		return err
	}

<<<<<<< HEAD
	oldWeight := vdr.Weight
	vdr.Weight += weight
=======
	oldWeight := vdr.weight
	vdr.weight += weight
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	s.weights[vdr.index] += weight
	s.totalWeight = newTotalWeight
	s.samplerInitialized = false

<<<<<<< HEAD
	s.callWeightChangeCallbacks(nodeID, oldWeight, vdr.Weight)
	return nil
}

func (s *vdrSet) GetWeight(nodeID ids.NodeID) uint64 {
=======
	s.callWeightChangeCallbacks(nodeID, oldWeight, vdr.weight)
	return nil
}

func (s *set) GetWeight(nodeID ids.NodeID) uint64 {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getWeight(nodeID)
}

<<<<<<< HEAD
func (s *vdrSet) getWeight(nodeID ids.NodeID) uint64 {
	if vdr, ok := s.vdrs[nodeID]; ok {
		return vdr.Weight
=======
func (s *set) getWeight(nodeID ids.NodeID) uint64 {
	if vdr, ok := s.vdrs[nodeID]; ok {
		return vdr.weight
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	}
	return 0
}

<<<<<<< HEAD
func (s *vdrSet) SubsetWeight(subset set.Set[ids.NodeID]) uint64 {
=======
func (s *set) SubsetWeight(subset ids.NodeIDSet) uint64 {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.subsetWeight(subset)
}

<<<<<<< HEAD
func (s *vdrSet) subsetWeight(subset set.Set[ids.NodeID]) uint64 {
=======
func (s *set) subsetWeight(subset ids.NodeIDSet) uint64 {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	var totalWeight uint64
	for nodeID := range subset {
		// Because [totalWeight] will be <= [s.totalWeight], we are guaranteed
		// this will not overflow.
		totalWeight += s.getWeight(nodeID)
	}
	return totalWeight
}

<<<<<<< HEAD
func (s *vdrSet) RemoveWeight(nodeID ids.NodeID, weight uint64) error {
=======
func (s *set) RemoveWeight(nodeID ids.NodeID, weight uint64) error {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	if weight == 0 {
		return errZeroWeight
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	return s.removeWeight(nodeID, weight)
}

<<<<<<< HEAD
func (s *vdrSet) removeWeight(nodeID ids.NodeID, weight uint64) error {
=======
func (s *set) removeWeight(nodeID ids.NodeID, weight uint64) error {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	vdr, ok := s.vdrs[nodeID]
	if !ok {
		return errMissingValidator
	}

<<<<<<< HEAD
	oldWeight := vdr.Weight
=======
	oldWeight := vdr.weight
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	// We first calculate the new weight of the validator, as this guarantees
	// that none of the following operations can underflow.
	newWeight, err := math.Sub(oldWeight, weight)
	if err != nil {
		return err
	}

	if newWeight == 0 {
		// Get the last element
		lastIndex := len(s.vdrSlice) - 1
		vdrToSwap := s.vdrSlice[lastIndex]

		// Move element at last index --> index of removed validator
		vdrToSwap.index = vdr.index
		s.vdrSlice[vdr.index] = vdrToSwap
<<<<<<< HEAD
		s.weights[vdr.index] = vdrToSwap.Weight
=======
		s.weights[vdr.index] = vdrToSwap.weight
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))

		// Remove validator
		delete(s.vdrs, nodeID)
		s.vdrSlice[lastIndex] = nil
		s.vdrSlice = s.vdrSlice[:lastIndex]
		s.weights = s.weights[:lastIndex]

		s.callValidatorRemovedCallbacks(nodeID, oldWeight)
	} else {
<<<<<<< HEAD
		vdr.Weight = newWeight
=======
		vdr.weight = newWeight
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
		s.weights[vdr.index] = newWeight

		s.callWeightChangeCallbacks(nodeID, oldWeight, newWeight)
	}
	s.totalWeight -= weight
	s.samplerInitialized = false
	return nil
}

<<<<<<< HEAD
func (s *vdrSet) Get(nodeID ids.NodeID) (*Validator, bool) {
=======
func (s *set) Get(nodeID ids.NodeID) (Validator, bool) {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.get(nodeID)
}

<<<<<<< HEAD
func (s *vdrSet) get(nodeID ids.NodeID) (*Validator, bool) {
=======
func (s *set) get(nodeID ids.NodeID) (Validator, bool) {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	vdr, ok := s.vdrs[nodeID]
	if !ok {
		return nil, false
	}
	copiedVdr := *vdr
	return &copiedVdr, true
}

<<<<<<< HEAD
func (s *vdrSet) Contains(nodeID ids.NodeID) bool {
=======
func (s *set) Contains(nodeID ids.NodeID) bool {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.contains(nodeID)
}

<<<<<<< HEAD
func (s *vdrSet) contains(nodeID ids.NodeID) bool {
=======
func (s *set) contains(nodeID ids.NodeID) bool {
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
	_, contains := s.vdrs[nodeID]
	return contains
}

func (s *vdrSet) Len() int {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.len()
}

<<<<<<< HEAD
func (s *vdrSet) len() int {
=======
func (s *set) len() int {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
	return len(s.vdrSlice)
}

func (s *vdrSet) List() []*Validator {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.list()
}

func (s *vdrSet) list() []*Validator {
	list := make([]*Validator, len(s.vdrSlice))
	for i, vdr := range s.vdrSlice {
		copiedVdr := *vdr
		list[i] = &copiedVdr
	}
	return list
}

<<<<<<< HEAD
func (s *vdrSet) Sample(size int) ([]ids.NodeID, error) {
=======
func (s *set) Sample(size int) ([]ids.NodeID, error) {
>>>>>>> 98ebbad72 (Simplify validators.Set#Sample return signature (#2292))
	if size == 0 {
		return nil, nil
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	return s.sample(size)
}

<<<<<<< HEAD
func (s *vdrSet) sample(size int) ([]ids.NodeID, error) {
=======
func (s *set) sample(size int) ([]ids.NodeID, error) {
>>>>>>> 98ebbad72 (Simplify validators.Set#Sample return signature (#2292))
	if !s.samplerInitialized {
		if err := s.sampler.Initialize(s.weights); err != nil {
			return nil, err
		}
		s.samplerInitialized = true
	}

	indices, err := s.sampler.Sample(size)
	if err != nil {
		return nil, err
	}

	list := make([]ids.NodeID, size)
	for i, index := range indices {
<<<<<<< HEAD
<<<<<<< HEAD
		list[i] = s.vdrSlice[index].NodeID
=======
		copiedVdr := *s.vdrSlice[index]
		list[i] = &copiedVdr
>>>>>>> 749a0d8e9 (Add validators.Set#Add function and report errors (#2276))
=======
		list[i] = s.vdrSlice[index].nodeID
>>>>>>> 98ebbad72 (Simplify validators.Set#Sample return signature (#2292))
	}
	return list, nil
}

func (s *vdrSet) Weight() uint64 {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.totalWeight
}

func (s *vdrSet) String() string {
	return s.PrefixedString("")
}

func (s *vdrSet) PrefixedString(prefix string) string {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.prefixedString(prefix)
}

func (s *vdrSet) prefixedString(prefix string) string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Validator Set: (Size = %d, Weight = %d)",
		len(s.vdrSlice),
		s.totalWeight,
	))
	format := fmt.Sprintf("\n%s    Validator[%s]: %%33s, %%d", prefix, formatting.IntFormat(len(s.vdrSlice)-1))
	for i, vdr := range s.vdrSlice {
		sb.WriteString(fmt.Sprintf(
			format,
			i,
			vdr.NodeID,
			vdr.Weight,
		))
	}

	return sb.String()
}

func (s *vdrSet) RegisterCallbackListener(callbackListener SetCallbackListener) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.callbackListeners = append(s.callbackListeners, callbackListener)
	for _, vdr := range s.vdrSlice {
		callbackListener.OnValidatorAdded(vdr.NodeID, vdr.PublicKey, vdr.TxID, vdr.Weight)
	}
}

// Assumes [s.lock] is held
func (s *vdrSet) callWeightChangeCallbacks(node ids.NodeID, oldWeight, newWeight uint64) {
	for _, callbackListener := range s.callbackListeners {
		callbackListener.OnValidatorWeightChanged(node, oldWeight, newWeight)
	}
}

// Assumes [s.lock] is held
func (s *vdrSet) callValidatorAddedCallbacks(node ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	for _, callbackListener := range s.callbackListeners {
		callbackListener.OnValidatorAdded(node, pk, txID, weight)
	}
}

// Assumes [s.lock] is held
func (s *vdrSet) callValidatorRemovedCallbacks(node ids.NodeID, weight uint64) {
	for _, callbackListener := range s.callbackListeners {
		callbackListener.OnValidatorRemoved(node, weight)
	}
}
