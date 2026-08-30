// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"net/http"

	"github.com/luxfi/log"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// Service answers for the proposer wrapper around a chain's own VM.
type Service struct {
	vm *VM
}

// GetProposedHeightArgs are the arguments for getProposedHeight
type GetProposedHeightArgs struct{}

// GetProposedHeightReply is the answer from getProposedHeight
type GetProposedHeightReply struct {
	// ProposedHeight is the P-Chain height that would be proposed for the next
	// block built on the current preferred block.
	ProposedHeight uint64 `json:"proposedHeight"`
}

// getProposedHeight returns the P-Chain height this node would propose for the
// next block built on its preferred one.
//
// Response: {"proposedHeight":42}
func (s *Service) getProposedHeight(ctx context.Context, _ *GetProposedHeightArgs) (*GetProposedHeightReply, error) {
	s.vm.lock.Lock()
	defer s.vm.lock.Unlock()

	// Get the current preferred block
	preferredBlock, err := s.vm.getBlock(ctx, s.vm.preferred)
	if err != nil {
		return nil, err
	}

	// Get the P-Chain height that would be proposed for a child of this block
	proposedHeight, err := preferredBlock.selectChildPChainHeight(ctx)
	if err != nil {
		return nil, err
	}

	return &GetProposedHeightReply{ProposedHeight: proposedHeight}, nil
}

// ops is what the proposer wrapper answers. It reads the height it would
// propose next and changes nothing, so it is a GET and anyone may ask.
func (s *Service) ops(logger log.Logger) *zip.App {
	app := zip.New(zip.Config{
		AppName:               "proposervm",
		Logger:                logger,
		DisableStartupMessage: true,
	})

	zip.Get(app, "/proposed/height", s.getProposedHeight)

	return app
}

// NewHTTPHandler serves the proposer wrapper's operations.
func NewHTTPHandler(vm *VM) (*zip.App, http.Handler, error) {
	app := (&Service{vm: vm}).ops(vm.logger)
	handler, err := server.Mount(app)
	return app, handler, err
}
