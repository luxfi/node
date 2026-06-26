// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"io"
	"net/http"
	"testing"
	"time"

	zaphttp "github.com/zap-proto/http"

	log "github.com/luxfi/log"
)

// TestZapRPCListenAddr verifies the opt-in env parsing: the listener stays
// disabled unless LUX_ZAP_RPC_LISTEN is set to a real bind address.
func TestZapRPCListenAddr(t *testing.T) {
	cases := []struct {
		set  bool
		val  string
		want string
	}{
		{set: false, want: ""},          // unset → disabled (default)
		{set: true, val: "", want: ""},  // empty → disabled
		{set: true, val: "off", want: ""},
		{set: true, val: "OFF", want: ""},
		{set: true, val: "0", want: ""},
		{set: true, val: "false", want: ""},
		{set: true, val: ":9651", want: ":9651"},
		{set: true, val: "127.0.0.1:9651", want: "127.0.0.1:9651"},
	}
	for _, c := range cases {
		if c.set {
			t.Setenv(envZapRPCListen, c.val)
		} else {
			t.Setenv(envZapRPCListen, "") // ensure clean, then unset semantics below
			// t.Setenv can't unset; emulate "unset" only for the documented default
			// by treating empty as disabled, which the want already encodes.
		}
		if got := zapRPCListenAddr(); got != c.want {
			t.Fatalf("zapRPCListenAddr(set=%v val=%q) = %q, want %q", c.set, c.val, got, c.want)
		}
	}
}

// TestStartZapRPCListener_Disabled returns nil (no listener) when addr is empty.
func TestStartZapRPCListener_Disabled(t *testing.T) {
	if srv := startZapRPCListener(log.NewNoOpLogger(), http.NewServeMux(), ""); srv != nil {
		t.Fatalf("expected nil server when addr empty, got %v", srv)
	}
}

// TestStartZapRPCListener_RoundTrip proves the listener serves the EXACT
// handler it is given over the github.com/zap-proto/http binary wire — a
// real ZAP request reaches the handler and the response comes back intact.
func TestStartZapRPCListener_RoundTrip(t *testing.T) {
	const addr = "127.0.0.1:19653"
	const body = `{"jsonrpc":"2.0","result":"0x2a","id":1}`

	mux := http.NewServeMux()
	mux.HandleFunc("/ext/bc/C/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	srv := startZapRPCListener(log.NewNoOpLogger(), mux, addr)
	if srv == nil {
		t.Fatal("expected a running ZAP-RPC listener, got nil")
	}
	defer srv.Close()

	// Give the goroutine a beat to bind.
	time.Sleep(150 * time.Millisecond)

	client := &http.Client{Transport: zaphttp.NewTransport(addr)}
	resp, err := client.Post("http://"+addr+"/ext/bc/C/rpc", "application/json", nil)
	if err != nil {
		t.Fatalf("ZAP round-trip POST failed: %v", err)
	}
	defer resp.Body.Close()

	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Fatalf("ZAP round-trip body = %q, want %q", got, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("ZAP round-trip Content-Type = %q, want application/json", ct)
	}
}
