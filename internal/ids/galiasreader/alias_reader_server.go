// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/luxfi/ids"

	aliasreaderpb "github.com/luxfi/node/proto/pb/aliasreader"
)

var _ aliasreaderpb.AliasReaderServer = (*Server)(nil)

// Server enables alias lookups over RPC.
type Server struct {
	aliasreaderpb.UnsafeAliasReaderServer
	aliaser ids.AliaserReader
}

// NewServer returns an alias lookup connected to a remote alias lookup
func NewServer(aliaser ids.AliaserReader) *Server {
	return &Server{aliaser: aliaser}
}

func (s *Server) Lookup(
	_ context.Context,
	req *aliasreaderpb.Alias,
) (*aliasreaderpb.ID, error) {
	id, err := s.aliaser.Lookup(req.Alias)
	if err != nil {
		return nil, err
	}
	return &aliasreaderpb.ID{
		Id: id[:],
	}, nil
}

func (s *Server) PrimaryAlias(
	_ context.Context,
	req *aliasreaderpb.ID,
) (*aliasreaderpb.Alias, error) {
	// Debug logging
	debugFile, _ := os.OpenFile("/tmp/aliasreader_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if debugFile != nil {
		fmt.Fprintf(debugFile, "%s DEBUG aliasreader server: PrimaryAlias called, aliaser=%p\n", time.Now().Format("15:04:05.000"), s.aliaser)
		debugFile.Close()
	}

	if s.aliaser == nil {
		debugFile, _ := os.OpenFile("/tmp/aliasreader_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if debugFile != nil {
			fmt.Fprintf(debugFile, "%s DEBUG aliasreader server: ERROR aliaser is nil!\n", time.Now().Format("15:04:05.000"))
			debugFile.Close()
		}
		return nil, fmt.Errorf("aliaser is nil")
	}

	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	alias, err := s.aliaser.PrimaryAlias(id)

	debugFile, _ = os.OpenFile("/tmp/aliasreader_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if debugFile != nil {
		fmt.Fprintf(debugFile, "%s DEBUG aliasreader server: PrimaryAlias result alias=%s err=%v\n", time.Now().Format("15:04:05.000"), alias, err)
		debugFile.Close()
	}

	return &aliasreaderpb.Alias{
		Alias: alias,
	}, err
}

func (s *Server) Aliases(
	_ context.Context,
	req *aliasreaderpb.ID,
) (*aliasreaderpb.AliasList, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	aliases, err := s.aliaser.Aliases(id)
	return &aliasreaderpb.AliasList{
		Aliases: aliases,
	}, err
}
