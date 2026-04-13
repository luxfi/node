// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import "net"

// NewListener creates a new TCP listener on an available port.
// This is used for the ZAP handshake during VM subprocess bootstrap.
func NewListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}
