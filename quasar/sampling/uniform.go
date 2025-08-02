// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import (
	"errors"
	"math"
	"math/rand"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/set"
)

var (
	ErrInsufficientElements = errors.New("insufficient elements")
	ErrTooManyElements      = errors.New("too many elements requested")
)

// Sampler samples elements from a set.
type Sampler interface {
	// Sample returns a sample of the given size from the set.
	// If there are less than [size] elements in the set, an error is returned.
	Sample(size int) ([]ids.NodeID, error)

	// CanSample returns whether a sample of the given size can be taken from the set.
	CanSample(size int) bool

	// Clear removes all elements from the sampler.
	Clear()

	// Add adds an element to the set.
	Add(ids.NodeID)

	// Len returns the number of elements in the set.
	Len() int

	// Weighted returns whether the sampler samples by weight.
	Weighted() bool
}

// Uniform samples uniformly from a set.
type Uniform struct {
	elements []ids.NodeID
	set      set.Set[ids.NodeID]
}

// NewUniform returns a new sampler that samples uniformly from the set.
func NewUniform() Sampler {
	return &Uniform{
		set: set.Set[ids.NodeID]{},
	}
}

// Sample returns a sample of the given size from the set.
func (u *Uniform) Sample(size int) ([]ids.NodeID, error) {
	if size > len(u.elements) {
		return nil, ErrInsufficientElements
	}

	if size <= 0 {
		return nil, nil
	}

	// Create a copy to avoid modifying the original
	indices := make([]int, len(u.elements))
	for i := range indices {
		indices[i] = i
	}

	// Fisher-Yates shuffle
	for i := 0; i < size; i++ {
		j := rand.Intn(len(indices)-i) + i
		indices[i], indices[j] = indices[j], indices[i]
	}

	// Select the first 'size' elements
	sample := make([]ids.NodeID, size)
	for i := 0; i < size; i++ {
		sample[i] = u.elements[indices[i]]
	}

	return sample, nil
}

// CanSample returns whether a sample of the given size can be taken from the set.
func (u *Uniform) CanSample(size int) bool {
	return size <= len(u.elements)
}

// Clear removes all elements from the sampler.
func (u *Uniform) Clear() {
	u.elements = u.elements[:0]
	u.set.Clear()
}

// Add adds an element to the set.
func (u *Uniform) Add(nodeID ids.NodeID) {
	if u.set.Contains(nodeID) {
		return
	}
	u.set.Add(nodeID)
	u.elements = append(u.elements, nodeID)
}

// Len returns the number of elements in the set.
func (u *Uniform) Len() int {
	return len(u.elements)
}

// Weighted returns false for uniform sampling.
func (u *Uniform) Weighted() bool {
	return false
}

// Weighted samples from a set with weights.
type Weighted struct {
	elements    []ids.NodeID
	weights     []uint64
	totalWeight uint64
	set         set.Set[ids.NodeID]
}

// NewWeighted returns a new sampler that samples by weight.
func NewWeighted() WeightedSampler {
	return &Weighted{
		set: set.Set[ids.NodeID]{},
	}
}

// WeightedSampler samples elements from a set with weights.
type WeightedSampler interface {
	Sampler

	// AddWeighted adds an element with the given weight to the set.
	AddWeighted(nodeID ids.NodeID, weight uint64)
}

// Sample returns a sample of the given size from the set.
func (w *Weighted) Sample(size int) ([]ids.NodeID, error) {
	if size > len(w.elements) {
		return nil, ErrInsufficientElements
	}

	if size <= 0 {
		return nil, nil
	}

	// Weighted sampling without replacement
	sample := make([]ids.NodeID, 0, size)
	used := set.Set[ids.NodeID]{}

	for len(sample) < size {
		// Calculate remaining weight
		remainingWeight := w.totalWeight
		for i, nodeID := range w.elements {
			if used.Contains(nodeID) {
				remainingWeight -= w.weights[i]
			}
		}

		if remainingWeight == 0 {
			return nil, ErrInsufficientElements
		}

		// Sample based on weight
		target := uint64(rand.Int63n(int64(remainingWeight)))
		cumulative := uint64(0)

		for i, nodeID := range w.elements {
			if used.Contains(nodeID) {
				continue
			}
			cumulative += w.weights[i]
			if cumulative > target {
				sample = append(sample, nodeID)
				used.Add(nodeID)
				break
			}
		}
	}

	return sample, nil
}

// CanSample returns whether a sample of the given size can be taken from the set.
func (w *Weighted) CanSample(size int) bool {
	return size <= len(w.elements)
}

// Clear removes all elements from the sampler.
func (w *Weighted) Clear() {
	w.elements = w.elements[:0]
	w.weights = w.weights[:0]
	w.totalWeight = 0
	w.set.Clear()
}

// Add adds an element with weight 1 to the set.
func (w *Weighted) Add(nodeID ids.NodeID) {
	w.AddWeighted(nodeID, 1)
}

// AddWeighted adds an element with the given weight to the set.
func (w *Weighted) AddWeighted(nodeID ids.NodeID, weight uint64) {
	if w.set.Contains(nodeID) || weight == 0 {
		return
	}
	w.set.Add(nodeID)
	w.elements = append(w.elements, nodeID)
	w.weights = append(w.weights, weight)
	w.totalWeight += weight
}

// Len returns the number of elements in the set.
func (w *Weighted) Len() int {
	return len(w.elements)
}

// Weighted returns true for weighted sampling.
func (w *Weighted) Weighted() bool {
	return true
}

// NewBest returns a new sampler that samples the K nodes with the best scores.
func NewBest(k int) BestSampler {
	return &bestSampler{
		k:      k,
		scores: make(map[ids.NodeID]uint64),
	}
}

// BestSampler samples the K elements with the best scores.
type BestSampler interface {
	// Sample returns the K elements with the best scores.
	Sample() []ids.NodeID

	// SetScore sets the score of the given element.
	SetScore(nodeID ids.NodeID, score uint64)

	// Clear removes all elements.
	Clear()
}

type bestSampler struct {
	k      int
	scores map[ids.NodeID]uint64
}

func (b *bestSampler) Sample() []ids.NodeID {
	// Get all elements and their scores
	type scored struct {
		nodeID ids.NodeID
		score  uint64
	}

	elements := make([]scored, 0, len(b.scores))
	for nodeID, score := range b.scores {
		elements = append(elements, scored{nodeID, score})
	}

	// Sort by score (highest first)
	for i := 0; i < len(elements); i++ {
		for j := i + 1; j < len(elements); j++ {
			if elements[j].score > elements[i].score {
				elements[i], elements[j] = elements[j], elements[i]
			}
		}
	}

	// Return top K
	k := b.k
	if k > len(elements) {
		k = len(elements)
	}

	result := make([]ids.NodeID, k)
	for i := 0; i < k; i++ {
		result[i] = elements[i].nodeID
	}

	return result
}

func (b *bestSampler) SetScore(nodeID ids.NodeID, score uint64) {
	b.scores[nodeID] = score
}

func (b *bestSampler) Clear() {
	// Create new map instead of clearing to avoid map growth issues
	b.scores = make(map[ids.NodeID]uint64)
}

// NewDeterministicUniform returns a new sampler that samples uniformly with a deterministic seed.
func NewDeterministicUniform(seed int64) Sampler {
	return &deterministicUniform{
		uniform: &Uniform{
			set: set.Set[ids.NodeID]{},
		},
		rng: rand.New(rand.NewSource(seed)),
	}
}

type deterministicUniform struct {
	uniform *Uniform
	rng     *rand.Rand
}

func (d *deterministicUniform) Sample(size int) ([]ids.NodeID, error) {
	if size > len(d.uniform.elements) {
		return nil, ErrInsufficientElements
	}

	if size <= 0 {
		return nil, nil
	}

	// Create a copy to avoid modifying the original
	indices := make([]int, len(d.uniform.elements))
	for i := range indices {
		indices[i] = i
	}

	// Fisher-Yates shuffle with deterministic RNG
	for i := 0; i < size; i++ {
		j := d.rng.Intn(len(indices)-i) + i
		indices[i], indices[j] = indices[j], indices[i]
	}

	// Select the first 'size' elements
	sample := make([]ids.NodeID, size)
	for i := 0; i < size; i++ {
		sample[i] = d.uniform.elements[indices[i]]
	}

	return sample, nil
}

func (d *deterministicUniform) CanSample(size int) bool {
	return d.uniform.CanSample(size)
}

func (d *deterministicUniform) Clear() {
	d.uniform.Clear()
}

func (d *deterministicUniform) Add(nodeID ids.NodeID) {
	d.uniform.Add(nodeID)
}

func (d *deterministicUniform) Len() int {
	return d.uniform.Len()
}

func (d *deterministicUniform) Weighted() bool {
	return false
}

// Type enumerates the different samplers that can be used.
type Type byte

const (
	// UniformSamplerType samples uniformly from the set.
	UniformSamplerType Type = iota
	// WeightedSamplerType samples from the set with weights.
	WeightedSamplerType
	// BestSamplerType samples the K elements with the best scores.
	BestSamplerType

	// MaxSamplerType is the largest valid sampler type.
	MaxSamplerType = math.MaxUint8
)

func (t Type) String() string {
	switch t {
	case UniformSamplerType:
		return "uniform"
	case WeightedSamplerType:
		return "weighted"
	case BestSamplerType:
		return "best"
	default:
		return "unknown"
	}
}