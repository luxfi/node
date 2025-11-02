// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"context"
	"net/http"
)

// DelegateHandlers delegates the CreateHandlers call to the underlying VM
func DelegateHandlers(ctx context.Context, vm interface{}) (map[string]http.Handler, error) {
	if handlerCreator, ok := vm.(interface {
		CreateHandlers(context.Context) (map[string]http.Handler, error)
	}); ok {
		return handlerCreator.CreateHandlers(ctx)
	}
	return nil, nil
}