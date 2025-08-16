// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensus

import (
	"context"
	"fmt"
	"sync"

	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

type acceptorWrapper struct {
	acceptor   consensus.Acceptor
	dieOnError bool
}

type acceptorGroup struct {
	log log.Logger

	lock      sync.RWMutex
	acceptors map[ids.ID]map[string]acceptorWrapper
}

// NewAcceptorGroup creates a new AcceptorGroup
func NewAcceptorGroup(log log.Logger) *acceptorGroup {
	return &acceptorGroup{
		log:       log,
		acceptors: make(map[ids.ID]map[string]acceptorWrapper),
	}
}

func (a *acceptorGroup) RegisterAcceptor(chainID ids.ID, acceptorName string, acceptor consensus.Acceptor, dieOnError bool) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	chainAcceptors, ok := a.acceptors[chainID]
	if !ok {
		chainAcceptors = make(map[string]acceptorWrapper)
		a.acceptors[chainID] = chainAcceptors
	}

	if _, ok := chainAcceptors[acceptorName]; ok {
		return fmt.Errorf("acceptor %s already registered for chain %s", acceptorName, chainID)
	}

	chainAcceptors[acceptorName] = acceptorWrapper{
		acceptor:   acceptor,
		dieOnError: dieOnError,
	}
	return nil
}

func (a *acceptorGroup) DeregisterAcceptor(chainID ids.ID, acceptorName string) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	chainAcceptors, ok := a.acceptors[chainID]
	if !ok {
		return nil
	}

	delete(chainAcceptors, acceptorName)
	if len(chainAcceptors) == 0 {
		delete(a.acceptors, chainID)
	}
	return nil
}

func (a *acceptorGroup) Accept(ctx context.Context, chainID ids.ID, containerID ids.ID, container []byte) error {
	a.lock.RLock()
	chainAcceptors := a.acceptors[chainID]
	a.lock.RUnlock()

	for name, wrapper := range chainAcceptors {
		if err := wrapper.acceptor.Accept(ctx, containerID, container); err != nil {
			a.log.Error("acceptor failed",
				"chain", chainID,
				"acceptor", name,
				"containerID", containerID,
				"error", err,
			)
			if wrapper.dieOnError {
				return err
			}
		}
	}
	return nil
}