// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"github.com/luxfi/db"

	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
)

var (
	ErrEnumToError = map[rpcdbpb.Error]error{
		rpcdbpb.Error_ERROR_CLOSED:    database.ErrClosed,
		rpcdbpb.Error_ERROR_NOT_FOUND: database.ErrNotFound,
	}
	ErrorToErrEnum = map[error]rpcdbpb.Error{
		database.ErrClosed:   rpcdbpb.Error_ERROR_CLOSED,
		database.ErrNotFound: rpcdbpb.Error_ERROR_NOT_FOUND,
	}
)

func ErrorToRPCError(err error) error {
	if _, ok := ErrorToErrEnum[err]; ok {
		return nil
	}
	return err
}
