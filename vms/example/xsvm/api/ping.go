// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/luxfi/node/connectproto/pb/xsvm"
	"github.com/luxfi/node/connectproto/pb/xsvm/xsvmconnect"
	luxlog "github.com/luxfi/log"
)

var _ xsvmconnect.PingHandler = (*PingService)(nil)

type PingService struct {
	Log luxlog.Logger
}

func (p *PingService) Ping(_ context.Context, request *connect.Request[xsvm.PingRequest]) (*connect.Response[xsvm.PingReply], error) {
	p.Log.Debug("ping", zap.String("message", request.Msg.Message))
	return connect.NewResponse[xsvm.PingReply](
		&xsvm.PingReply{
			Message: request.Msg.Message,
		},
	), nil
}

func (p *PingService) StreamPing(_ context.Context, server *connect.BidiStream[xsvm.StreamPingRequest, xsvm.StreamPingReply]) error {
	for {
		request, err := server.Receive()
		if errors.Is(err, io.EOF) {
			// Client closed the send stream
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to receive message: %w", err)
		}

		p.Log.Debug("stream ping", zap.String("message", request.Message))
		err = server.Send(&xsvm.StreamPingReply{
			Message: request.Message,
		})
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}
}
