// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/zap-proto/zip"

	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/utils/json"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

type service struct {
	index *index
}

// ops is what one chain's index answers. Every operation is a GET: an index
// reads what consensus already accepted and changes nothing, so there is
// nothing to authorize and anyone may ask.
//
// A container is addressed three ways because a caller holds one of three
// things — its id, its position, or nothing but the wish for the newest — and
// each is a different question with the same answer shape.
func (s *service) ops(logger log.Logger, name string) *zip.App {
	app := zip.New(zip.Config{
		AppName:               "index-" + name,
		Logger:                logger,
		DisableStartupMessage: true,
	})

	zip.Get(app, "/container", s.getContainer)
	zip.Get(app, "/container/:index", s.getContainerByIndex)
	zip.Get(app, "/container/latest", s.getContainerLatest)
	zip.Get(app, "/containers", s.getContainers)
	zip.Get(app, "/index", s.getIndex)
	zip.Get(app, "/accepted", s.getAccepted)

	return app
}

// mount serves this index's operations under its own endpoint.
func (s *service) mount(logger log.Logger, name string) (*zip.App, http.Handler, error) {
	app := s.ops(logger, name)
	handler, err := server.Mount(app)
	return app, handler, err
}

type FormattedContainer struct {
	// ID is the container's own id.
	ID ids.ID `json:"id"`
	// Bytes is the container, written in Encoding.
	Bytes string `json:"bytes"`
	// Timestamp is when this node accepted the container.
	Timestamp json.Time `json:"timestamp"`
	// Encoding is how Bytes is written.
	Encoding formatting.Encoding `json:"encoding"`
	// Index is the container's position in the accepted order, from zero.
	Index json.Uint64 `json:"index"`
}

func newFormattedContainer(c Container, index uint64, enc formatting.Encoding) (FormattedContainer, error) {
	fc := FormattedContainer{
		Encoding: enc,
		ID:       c.ID,
		Index:    json.Uint64(index),
	}
	bytesStr, err := formatting.Encode(enc, c.Bytes)
	if err != nil {
		return fc, err
	}
	fc.Bytes = bytesStr
	fc.Timestamp = json.NewTime(time.Unix(0, c.Timestamp))
	return fc, nil
}

type GetLastAcceptedArgs struct {
	// Encoding is how to write the container's bytes.
	Encoding formatting.Encoding `json:"encoding"`
}

// getContainerLatest returns the container this node accepted most recently.
//
// Example: {"encoding":"hex"}
func (s *service) getContainerLatest(_ context.Context, in *GetLastAcceptedArgs) (*FormattedContainer, error) {
	container, err := s.index.GetLastAccepted()
	if err != nil {
		return nil, err
	}
	index, err := s.index.GetIndex(container.ID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get index: %w", err)
	}
	reply, err := newFormattedContainer(container, index, in.Encoding)
	return &reply, err
}

type GetContainerByIndexArgs struct {
	// Index is the position to read, from zero.
	Index json.Uint64 `json:"index"`
	// Encoding is how to write the container's bytes.
	Encoding formatting.Encoding `json:"encoding"`
}

// getContainerByIndex returns the container accepted at the given position.
//
// Example: {"index":"0","encoding":"hex"}
func (s *service) getContainerByIndex(_ context.Context, in *GetContainerByIndexArgs) (*FormattedContainer, error) {
	container, err := s.index.GetContainerByIndex(uint64(in.Index))
	if err != nil {
		return nil, err
	}
	index, err := s.index.GetIndex(container.ID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get index: %w", err)
	}
	reply, err := newFormattedContainer(container, index, in.Encoding)
	return &reply, err
}

type GetContainerRangeArgs struct {
	// StartIndex is the first position to read, from zero.
	StartIndex json.Uint64 `json:"startIndex"`
	// NumToFetch is how many to read from there.
	NumToFetch json.Uint64 `json:"numToFetch"`
	// Encoding is how to write each container's bytes.
	Encoding formatting.Encoding `json:"encoding"`
}

type GetContainerRangeResponse struct {
	// Containers are those read, in accepted order.
	Containers []FormattedContainer `json:"containers"`
}

// getContainers returns the containers accepted at startIndex and after it.
//
// It returns what it finds: asking past the last accepted one yields the
// containers before it and no error. numToFetch of zero yields none.
//
// Example: {"startIndex":"0","numToFetch":"2","encoding":"hex"}
func (s *service) getContainers(_ context.Context, in *GetContainerRangeArgs) (*GetContainerRangeResponse, error) {
	containers, err := s.index.GetContainerRange(uint64(in.StartIndex), uint64(in.NumToFetch))
	if err != nil {
		return nil, err
	}

	reply := &GetContainerRangeResponse{Containers: make([]FormattedContainer, len(containers))}
	for i, container := range containers {
		index, err := s.index.GetIndex(container.ID)
		if err != nil {
			return nil, fmt.Errorf("couldn't get index: %w", err)
		}
		reply.Containers[i], err = newFormattedContainer(container, index, in.Encoding)
		if err != nil {
			return nil, err
		}
	}
	return reply, nil
}

type GetIndexArgs struct {
	// ID is the container to locate.
	ID ids.ID `json:"id"`
}

type GetIndexResponse struct {
	// Index is the container's position in the accepted order, from zero.
	Index json.Uint64 `json:"index"`
}

// getIndex returns a container's position in the accepted order.
//
// Example: {"id":"11111111111111111111111111111111LpoYY"}
func (s *service) getIndex(_ context.Context, in *GetIndexArgs) (*GetIndexResponse, error) {
	index, err := s.index.GetIndex(in.ID)
	return &GetIndexResponse{Index: json.Uint64(index)}, err
}

type IsAcceptedArgs struct {
	// ID is the container to ask about.
	ID ids.ID `json:"id"`
}

type IsAcceptedResponse struct {
	// IsAccepted is whether this node has accepted and indexed the container.
	IsAccepted bool `json:"isAccepted"`
}

// getAccepted returns whether this node has accepted and indexed a container.
//
// Example: {"id":"11111111111111111111111111111111LpoYY"}
func (s *service) getAccepted(_ context.Context, in *IsAcceptedArgs) (*IsAcceptedResponse, error) {
	_, err := s.index.GetIndex(in.ID)
	if err == nil {
		return &IsAcceptedResponse{IsAccepted: true}, nil
	}
	if err == database.ErrNotFound {
		return &IsAcceptedResponse{IsAccepted: false}, nil
	}
	return nil, err
}

type GetContainerByIDArgs struct {
	// ID is the container to read.
	ID ids.ID `json:"id"`
	// Encoding is how to write the container's bytes.
	Encoding formatting.Encoding `json:"encoding"`
}

// getContainer returns the container with the given id.
//
// Example: {"id":"11111111111111111111111111111111LpoYY","encoding":"hex"}
func (s *service) getContainer(_ context.Context, in *GetContainerByIDArgs) (*FormattedContainer, error) {
	container, err := s.index.GetContainerByID(in.ID)
	if err != nil {
		return nil, err
	}
	index, err := s.index.GetIndex(container.ID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get index: %w", err)
	}
	reply, err := newFormattedContainer(container, index, in.Encoding)
	return &reply, err
}
