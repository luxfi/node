// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"sync"

	apiwarp "github.com/luxfi/api/warp"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/warp"
)

// Service implements api/warp.Service.
type Service struct {
	log          log.Logger
	chainManager chains.Manager
	lock         sync.RWMutex
	ipcs         *warp.ChainIPCs
}

func New(log log.Logger, chainManager chains.Manager, ipcs *warp.ChainIPCs) *Service {
	return &Service{
		log:          log,
		chainManager: chainManager,
		ipcs:         ipcs,
	}
}

func (s *Service) PublishBlockchain(ctx context.Context, args *apiwarp.PublishBlockchainArgs) (*apiwarp.PublishBlockchainReply, error) {
	_ = ctx
	s.log.Warn("deprecated API called",
		log.UserString("service", "ipcs"),
		log.UserString("method", "publishBlockchain"),
		log.UserString("blockchainID", args.BlockchainID),
	)

	chainID, err := s.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		s.log.Error("chain lookup failed",
			log.UserString("blockchainID", args.BlockchainID),
			log.Reflect("error", err),
		)
		return nil, err
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	ipcs, err := s.ipcs.Publish(chainID)
	if err != nil {
		s.log.Error("couldn't publish chain",
			log.UserString("blockchainID", args.BlockchainID),
			log.Reflect("error", err),
		)
		return nil, err
	}

	return &apiwarp.PublishBlockchainReply{
		ConsensusURL: ipcs.ConsensusURL(),
		DecisionsURL: ipcs.DecisionsURL(),
	}, nil
}

func (s *Service) UnpublishBlockchain(ctx context.Context, args *apiwarp.UnpublishBlockchainArgs) (*apiwarp.EmptyReply, error) {
	_ = ctx
	s.log.Warn("deprecated API called",
		log.UserString("service", "ipcs"),
		log.UserString("method", "unpublishBlockchain"),
		log.UserString("blockchainID", args.BlockchainID),
	)

	chainID, err := s.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		s.log.Error("chain lookup failed",
			log.UserString("blockchainID", args.BlockchainID),
			log.Reflect("error", err),
		)
		return nil, err
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	ok, err := s.ipcs.Unpublish(chainID)
	if !ok {
		s.log.Error("couldn't publish chain",
			log.UserString("blockchainID", args.BlockchainID),
			log.Reflect("error", err),
		)
	}

	return &apiwarp.EmptyReply{}, err
}

func (s *Service) GetPublishedBlockchains(ctx context.Context) (*apiwarp.GetPublishedBlockchainsReply, error) {
	_ = ctx
	s.log.Warn("deprecated API called",
		log.UserString("service", "ipcs"),
		log.UserString("method", "getPublishedBlockchains"),
	)

	s.lock.RLock()
	defer s.lock.RUnlock()

	chains := s.ipcs.GetPublishedBlockchains()
	return &apiwarp.GetPublishedBlockchainsReply{Chains: chains}, nil
}

var _ apiwarp.Service = (*Service)(nil)
var _ ids.ID
