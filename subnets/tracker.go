// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"github.com/luxfi/ids"
)

// Tracker tracks information about subnets
type Tracker interface {
	// GetSubnet returns the subnet configuration for a given subnet ID
	GetSubnet(subnetID ids.ID) (Subnet, bool)

	// Tracked returns true if the subnet is being tracked
	Tracked(subnetID ids.ID) bool

	// OnFinishedBootstrapping returns a channel that is closed when
	// the subnet finishes bootstrapping
	OnFinishedBootstrapping(subnetID ids.ID) chan struct{}

	// AddSubnet adds a subnet to track
	AddSubnet(subnetID ids.ID, subnet Subnet) error

	// RemoveSubnet removes a subnet from tracking
	RemoveSubnet(subnetID ids.ID) error
}

// NewTracker creates a new subnet tracker
func NewTracker() Tracker {
	return &tracker{
		subnets: make(map[ids.ID]Subnet),
	}
}

type tracker struct {
	subnets map[ids.ID]Subnet
}

func (t *tracker) GetSubnet(subnetID ids.ID) (Subnet, bool) {
	subnet, ok := t.subnets[subnetID]
	return subnet, ok
}

func (t *tracker) Tracked(subnetID ids.ID) bool {
	_, ok := t.subnets[subnetID]
	return ok
}

func (t *tracker) OnFinishedBootstrapping(subnetID ids.ID) chan struct{} {
	subnet, ok := t.subnets[subnetID]
	if !ok {
		// Return a closed channel if subnet is not tracked
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	// Convert read-only channel to bidirectional channel
	ch := make(chan struct{})
	go func() {
		<-subnet.AllBootstrapped()
		close(ch)
	}()
	return ch
}

func (t *tracker) AddSubnet(subnetID ids.ID, subnet Subnet) error {
	t.subnets[subnetID] = subnet
	return nil
}

func (t *tracker) RemoveSubnet(subnetID ids.ID) error {
	delete(t.subnets, subnetID)
	return nil
}