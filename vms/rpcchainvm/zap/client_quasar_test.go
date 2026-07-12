// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	luxruntime "github.com/luxfi/runtime"
	"github.com/luxfi/version"
	rpc "github.com/luxfi/vm/rpc"
)

// These tests exercise the REAL cross-process export path that the live deploy
// uses — node rpcchainvm *Client  →  ZAP wire  →  luxfi/vm/rpc server  →  the
// wrapped VM — which is where the export tier had been silently dormant (the
// node held the *Client, whose method set did not carry the Quasar calls, so the
// chain manager's capability probe failed and the observer never fired). The
// fake VM here stands in for the C-Chain EVM's concrete *VM: the server asserts
// the SAME structural capability interface against it that it asserts against
// *evm.VM, so this proves the plumbing end to end without the full EVM stack.

// baseFakeVM is a minimal block.ChainVM with a NON-nil GetBlock (so the server's
// handleInitialize succeeds) and NO export capability.
type baseFakeVM struct{}

func (baseFakeVM) Initialize(context.Context, block.Init) error            { return nil }
func (baseFakeVM) BuildBlock(context.Context) (block.Block, error)         { return &fakeBlock{}, nil }
func (baseFakeVM) ParseBlock(context.Context, []byte) (block.Block, error) { return &fakeBlock{}, nil }
func (baseFakeVM) GetBlock(context.Context, ids.ID) (block.Block, error)   { return &fakeBlock{}, nil }
func (baseFakeVM) Shutdown(context.Context) error                          { return nil }
func (baseFakeVM) NewHTTPHandler(context.Context) (http.Handler, error)    { return nil, nil }
func (baseFakeVM) SetState(context.Context, uint32) error                  { return nil }
func (baseFakeVM) Version(context.Context) (string, error)                 { return "fake", nil }
func (baseFakeVM) Connected(context.Context, ids.NodeID, *version.Application) error {
	return nil
}
func (baseFakeVM) Disconnected(context.Context, ids.NodeID) error { return nil }
func (baseFakeVM) HealthCheck(context.Context) (block.HealthCheckResult, error) {
	var r block.HealthCheckResult
	return r, nil
}
func (baseFakeVM) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	return ids.Empty, nil
}
func (baseFakeVM) SetPreference(context.Context, ids.ID) error  { return nil }
func (baseFakeVM) LastAccepted(context.Context) (ids.ID, error) { return ids.Empty, nil }
func (baseFakeVM) WaitForEvent(context.Context) (block.Message, error) {
	var m block.Message
	return m, nil
}

var _ block.ChainVM = baseFakeVM{}

// exportFakeVM adds the OPTIONAL export capability — the shape of the C-Chain EVM.
type exportFakeVM struct {
	baseFakeVM
	mu     sync.Mutex
	height uint64
}

func (v *exportFakeVM) SetLastQuasarFinalized(h uint64) {
	v.mu.Lock()
	v.height = h
	v.mu.Unlock()
}

func (v *exportFakeVM) LastQuasarHeight() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.height
}

type fakeBlock struct{}

func (*fakeBlock) ID() ids.ID                   { return ids.Empty }
func (*fakeBlock) Parent() ids.ID               { return ids.Empty }
func (*fakeBlock) ParentID() ids.ID             { return ids.Empty }
func (*fakeBlock) Height() uint64               { return 0 }
func (*fakeBlock) Timestamp() time.Time         { return time.Unix(0, 0) }
func (*fakeBlock) Status() uint8                { return 0 }
func (*fakeBlock) Bytes() []byte                { return []byte{} }
func (*fakeBlock) Verify(context.Context) error { return nil }
func (*fakeBlock) Accept(context.Context) error { return nil }
func (*fakeBlock) Reject(context.Context) error { return nil }

var _ block.Block = (*fakeBlock)(nil)

// newQuasarClient stands up the REAL luxfi/vm/rpc server (via NewZAPHandler)
// wrapping vm, connects a REAL node *Client over an in-process ZAP link, and runs
// the real Initialize handshake (which is where the capability is advertised).
func newQuasarClient(t *testing.T, vm block.ChainVM) *Client {
	t.Helper()
	addr, stop := startTestServer(t, rpc.NewZAPHandler(vm, log.NewNoOpLogger()))
	t.Cleanup(stop)

	conn, err := zapwire.Dial(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	c := NewClient(conn, log.NewNoOpLogger())
	rt := &luxruntime.Runtime{NetworkID: 1337, ChainDataDir: t.TempDir()}
	if err := c.Initialize(context.Background(), block.Init{Runtime: rt, Genesis: []byte("{}")}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return c
}

// TestQuasarExport_CrossProcess_ReachesVM is the integration guard for the
// deploy-blocking gap: with an export-capable VM behind the plugin boundary, the
// node *Client (a) captures the capability from the Initialize handshake, (b)
// forwards a pushed export height ACROSS the wire into the VM, and (c) reads the
// VM's height back across the wire. Before the fix the *Client carried none of
// this and the whole export tier stuck at genesis in the real plugin deploy.
func TestQuasarExport_CrossProcess_ReachesVM(t *testing.T) {
	vm := &exportFakeVM{}
	c := newQuasarClient(t, vm)

	if !c.SupportsQuasarExport() {
		t.Fatal("client did not capture CapQuasarExport from the Initialize handshake")
	}

	// Push crosses the wire and reaches the concrete VM (what the chain manager's
	// QuasarObserver does on every ⅔-stake export-frontier advance).
	c.SetLastQuasarFinalized(42)
	if got := vm.LastQuasarHeight(); got != 42 {
		t.Fatalf("SetLastQuasarFinalized did not reach the VM across the boundary: got %d want 42", got)
	}

	// Read-back crosses the wire (what the chain manager's boot re-seed does).
	if got := c.LastQuasarHeight(); got != 42 {
		t.Fatalf("LastQuasarHeight did not round-trip the VM's height: got %d want 42", got)
	}
}

// TestQuasarExport_CrossProcess_NonCapableGraceful proves the OTHER half of the
// capability gate: a generic plugin (no export methods) advertises nothing, so
// the *Client reports not-capable and the export calls are graceful no-ops — the
// chain manager therefore leaves the observer unwired and the chain runs
// Nova-only, with no per-finalization cross-process traffic.
func TestQuasarExport_CrossProcess_NonCapableGraceful(t *testing.T) {
	c := newQuasarClient(t, baseFakeVM{})

	if c.SupportsQuasarExport() {
		t.Fatal("generic VM must not advertise CapQuasarExport")
	}
	// No panic, no RPC, sentinel height.
	c.SetLastQuasarFinalized(42)
	if got := c.LastQuasarHeight(); got != 0 {
		t.Fatalf("non-capable client must report 0 (empty frontier), got %d", got)
	}
}
