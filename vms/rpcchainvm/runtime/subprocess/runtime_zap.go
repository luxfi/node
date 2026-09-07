// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
)

// Bootstrap starts a VM as a subprocess after initialization completes
// and pipes the IO to the appropriate writers. Handshake is the ZAP
// binary framed exchange ([len][protocol version][vm addr]).
//
// The subprocess is expected to be stopped by the caller if a non-nil
// error is returned. If piping the IO fails then the subprocess will
// be stopped.
func Bootstrap(
	ctx context.Context,
	listener net.Listener,
	cmd *exec.Cmd,
	config *Config,
) (*Status, runtime.Stopper, error) {
	defer listener.Close()

	switch {
	case cmd == nil:
		return nil, nil, fmt.Errorf("%w: cmd required", runtime.ErrInvalidConfig)
	case config.Log.IsZero():
		return nil, nil, fmt.Errorf("%w: logger required", runtime.ErrInvalidConfig)
	case config.Stderr == nil, config.Stdout == nil:
		return nil, nil, fmt.Errorf("%w: stderr and stdout required", runtime.ErrInvalidConfig)
	}

	serverAddr := listener.Addr()
	// CRITICAL: If cmd.Env is already set (e.g., by tests), preserve those values
	// and only add our required environment variable. If cmd.Env is nil,
	// copy the parent's environment first.
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", runtime.EngineAddressKey, serverAddr.String()))
	// Compat shim: also set the pre-rename env key so plugin binaries built
	// against luxfi/node <= v1.26.x (which had EngineAddressKey =
	// "LUX_VM_RUNTIME_ENGINE_ADDR") can still dial back. Plugins built from
	// luxfi/node >= the rename pick up the new key; old plugins keep working.
	// Remove this line once every plugin in /data/plugins is rebuilt against
	// a luxfi/node version that has the rename.
	if runtime.EngineAddressKey != runtime.LegacyEngineAddressKey {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", runtime.LegacyEngineAddressKey, serverAddr.String()))
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// start subprocess
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start process: %w", err)
	}

	log := config.Log
	stopper := NewStopper(log, cmd)

	// start stdout collector
	go func() {
		_, err := io.Copy(config.Stdout, stdoutPipe)
		if err != nil {
			log.Error("stdout collector failed", "error", err)
		}
		stopper.Stop(context.TODO())
		log.Info("stdout collector shutdown")
	}()

	// start stderr collector
	go func() {
		_, err := io.Copy(config.Stderr, stderrPipe)
		if err != nil {
			log.Error("stderr collector failed", "error", err)
		}
		stopper.Stop(context.TODO())
		log.Info("stderr collector shutdown")
	}()

	// Accept connection and wait for handshake
	resultCh := make(chan handshakeResult, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			resultCh <- handshakeResult{err: fmt.Errorf("failed to accept connection: %w", err)}
			return
		}
		defer conn.Close()

		// Read handshake: [4-byte len][4-byte protocol version][vm addr string]
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			resultCh <- handshakeResult{err: fmt.Errorf("failed to read handshake header: %w", err)}
			return
		}

		msgLen := binary.BigEndian.Uint32(header[:4])
		protocolVersion := binary.BigEndian.Uint32(header[4:8])

		addrLen := msgLen - 4 // subtract protocol version size
		addrBytes := make([]byte, addrLen)
		if _, err := io.ReadFull(conn, addrBytes); err != nil {
			resultCh <- handshakeResult{err: fmt.Errorf("failed to read vm address: %w", err)}
			return
		}
		vmAddr := string(addrBytes)

		// Verify protocol version
		if version.RPCChainVMProtocol != uint(protocolVersion) {
			resultCh <- handshakeResult{
				err: fmt.Errorf("%w. Lux Node version %s implements RPCChainVM protocol version %d. The VM implements protocol version %d",
					runtime.ErrProtocolVersionMismatch,
					version.Current,
					version.RPCChainVMProtocol,
					protocolVersion,
				),
			}
			return
		}

		// Send ACK: [1-byte OK]
		if _, err := conn.Write([]byte{1}); err != nil {
			resultCh <- handshakeResult{err: fmt.Errorf("failed to send handshake ack: %w", err)}
			return
		}

		resultCh <- handshakeResult{vmAddr: vmAddr}
	}()

	// wait for handshake success
	timeout := time.NewTimer(config.HandshakeTimeout)
	defer timeout.Stop()

	select {
	case result := <-resultCh:
		if result.err != nil {
			stopper.Stop(ctx)
			return nil, nil, fmt.Errorf("%w: %w", runtime.ErrHandshakeFailed, result.err)
		}
		log.Info("plugin handshake succeeded via ZAP", "addr", result.vmAddr)
		status := &Status{
			Pid:  cmd.Process.Pid,
			Addr: result.vmAddr,
		}
		return status, stopper, nil

	case <-timeout.C:
		stopper.Stop(ctx)
		return nil, nil, fmt.Errorf("%w: %w", runtime.ErrHandshakeFailed, runtime.ErrProcessNotFound)
	}
}

type handshakeResult struct {
	vmAddr string
	err    error
}
