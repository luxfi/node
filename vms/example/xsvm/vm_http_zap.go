// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"net/http"
)

// NewHTTPHandler returns the VM's HTTP handler. The example XSVM does
// not expose any HTTP endpoints in the canonical ZAP build.
func (vm *VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	return http.NewServeMux(), nil
}
