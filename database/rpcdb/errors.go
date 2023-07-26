// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"github.com/luxdefi/node/database"

	rpcdbpb "github.com/luxdefi/node/proto/pb/rpcdb"
)

var (
	errEnumToError = map[rpcdbpb.Error]error{
		rpcdbpb.Error_ERROR_CLOSED:    database.ErrClosed,
		rpcdbpb.Error_ERROR_NOT_FOUND: database.ErrNotFound,
	}
	errorToErrEnum = map[error]rpcdbpb.Error{
		database.ErrClosed:   rpcdbpb.Error_ERROR_CLOSED,
		database.ErrNotFound: rpcdbpb.Error_ERROR_NOT_FOUND,
	}
)

func errorToRPCError(err error) error {
	if _, ok := errorToErrEnum[err]; ok {
		return nil
	}
	return err
}
