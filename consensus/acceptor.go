// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensus

import (
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// Acceptor is something that can accept a piece of data
type Acceptor interface {
	Accept(ctx *Context, containerID ids.ID, container []byte) error
}

// AcceptorGroup manages multiple acceptors
type AcceptorGroup interface {
	RegisterAcceptor(chainID ids.ID, name string, acceptor Acceptor, persist bool) error
	DeregisterAcceptor(chainID ids.ID, name string) error
	Accept(ctx *Context, containerID ids.ID, container []byte) error
}

type acceptorGroup struct {
	log       log.Logger
	lock      sync.RWMutex
	acceptors map[ids.ID]map[string]Acceptor
}

// NewAcceptorGroup creates a new acceptor group
func NewAcceptorGroup(log log.Logger) AcceptorGroup {
	return &acceptorGroup{
		log:       log,
		acceptors: make(map[ids.ID]map[string]Acceptor),
	}
}

func (ag *acceptorGroup) RegisterAcceptor(chainID ids.ID, name string, acceptor Acceptor, persist bool) error {
	ag.lock.Lock()
	defer ag.lock.Unlock()

	if ag.acceptors[chainID] == nil {
		ag.acceptors[chainID] = make(map[string]Acceptor)
	}
	ag.acceptors[chainID][name] = acceptor
	ag.log.Debug("registered acceptor", "chainID", chainID, "name", name)
	return nil
}

func (ag *acceptorGroup) DeregisterAcceptor(chainID ids.ID, name string) error {
	ag.lock.Lock()
	defer ag.lock.Unlock()

	if chainAcceptors, exists := ag.acceptors[chainID]; exists {
		delete(chainAcceptors, name)
		if len(chainAcceptors) == 0 {
			delete(ag.acceptors, chainID)
		}
		ag.log.Debug("deregistered acceptor", "chainID", chainID, "name", name)
	}
	return nil
}

func (ag *acceptorGroup) Accept(ctx *Context, containerID ids.ID, container []byte) error {
	ag.lock.RLock()
	chainAcceptors := ag.acceptors[ctx.ChainID]
	ag.lock.RUnlock()

	if chainAcceptors == nil {
		return nil
	}

	for name, acceptor := range chainAcceptors {
		if err := acceptor.Accept(ctx, containerID, container); err != nil {
			ag.log.Error("acceptor failed", "chainID", ctx.ChainID, "name", name, "error", err)
			// Continue with other acceptors even if one fails
		}
	}
	return nil
}