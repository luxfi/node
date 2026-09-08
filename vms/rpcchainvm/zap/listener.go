// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// NewListener creates the listener a plugin VM dials back on during ZAP
// handshake bootstrap.
//
// The same-host fast path is a unix-domain socket — no TCP/UDP stack, no
// ephemeral-port exhaustion, filesystem-namespaced. The api/zap Dial infers
// "unix" from the socket-path addr, so a plugin rebuilt against that api
// connects back over the socket with zero code change on its side.
//
// The node and the plugins it runs are one build and ship together, so both
// sides move at once. Windows has no unix sockets and gets TCP; so does a host
// where the socket cannot be bound, because a chain that will not start is
// worse than one on a slower transport.
func NewListener() (net.Listener, error) {
	if runtime.GOOS != "windows" {
		if ln, err := newUnixListener(); err == nil {
			return ln, nil
		}
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

// newUnixListener binds a unix-domain socket under the OS temp dir. The socket
// dir is removed when the listener closes (unixListener.Close) so plugin churn
// does not leak socket inodes.
func newUnixListener() (net.Listener, error) {
	dir, err := os.MkdirTemp("", "luxd-vm-*")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "vm.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("unix listen: %w", err)
	}
	return &unixListener{Listener: ln, dir: dir}, nil
}

// unixListener wraps a unix net.Listener to remove its socket dir on Close.
type unixListener struct {
	net.Listener
	dir string
}

func (u *unixListener) Close() error {
	err := u.Listener.Close()
	_ = os.RemoveAll(u.dir)
	return err
}
