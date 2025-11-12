// (c) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luxfi/geth/common/hexutil"
	"github.com/luxfi/geth/eth"
	"github.com/luxfi/geth/params"
)

// TestEVMRPCEndpoints tests that all EVM RPC endpoints are properly registered and functional
func TestEVMRPCEndpoints(t *testing.T) {
	// Create a test VM with backend
	vm := &VM{
		backend: &MinimalEthBackend{
			chainConfig: &params.ChainConfig{
				ChainID: hexutil.MustDecodeBig("0x1869F"), // 99999 in hex
			},
		},
		ethConfig: eth.Config{
			NetworkId: 99999,
		},
	}
	
	// Initialize logger
	vm.ensureLogger()
	
	// Create handlers
	handlers, err := vm.CreateHandlers(context.Background())
	if err != nil {
		t.Fatalf("Failed to create handlers: %v", err)
	}
	
	// Test NewHTTPHandler returns the same handlers
	httpHandler, err := vm.NewHTTPHandler(context.Background())
	if err != nil {
		t.Fatalf("Failed to create HTTP handler: %v", err)
	}
	
	if httpHandler == nil {
		t.Fatal("NewHTTPHandler returned nil")
	}
	
	// Convert to map[string]http.Handler
	handlersMap, ok := httpHandler.(map[string]http.Handler)
	if !ok {
		t.Fatalf("NewHTTPHandler did not return map[string]http.Handler, got %T", httpHandler)
	}
	
	// Check that we have the expected handlers
	if len(handlersMap) != len(handlers) {
		t.Errorf("Handler count mismatch: NewHTTPHandler returned %d, CreateHandlers returned %d", 
			len(handlersMap), len(handlers))
	}
	
	// Test RPC handler exists
	rpcHandler, exists := handlersMap["/rpc"]
	if !exists {
		t.Fatal("RPC handler not found at /rpc")
	}
	
	// Test root handler exists
	rootHandler, exists := handlersMap["/"]
	if !exists {
		t.Fatal("RPC handler not found at /")
	}
	
	// Both should be the same handler
	if rpcHandler != rootHandler {
		t.Error("RPC handler at /rpc and / are different")
	}
	
	// Test specific RPC methods
	testCases := []struct {
		method string
		params []interface{}
		checkResult func(result json.RawMessage) error
	}{
		{
			method: "eth_chainId",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var chainId string
				if err := json.Unmarshal(result, &chainId); err != nil {
					return err
				}
				if chainId != "0x1869f" { // 99999 in hex
					t.Errorf("Expected chainId 0x1869f, got %s", chainId)
				}
				return nil
			},
		},
		{
			method: "web3_clientVersion",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var version string
				if err := json.Unmarshal(result, &version); err != nil {
					return err
				}
				if version != "Lux/v1.0.0" {
					t.Errorf("Expected version Lux/v1.0.0, got %s", version)
				}
				return nil
			},
		},
		{
			method: "net_version",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var netVersion string
				if err := json.Unmarshal(result, &netVersion); err != nil {
					return err
				}
				if netVersion != "99999" {
					t.Errorf("Expected netVersion 99999, got %s", netVersion)
				}
				return nil
			},
		},
		{
			method: "net_listening",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var listening bool
				if err := json.Unmarshal(result, &listening); err != nil {
					return err
				}
				if listening != false {
					t.Errorf("Expected listening false, got %v", listening)
				}
				return nil
			},
		},
		{
			method: "net_peerCount",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var peerCount string
				if err := json.Unmarshal(result, &peerCount); err != nil {
					return err
				}
				if peerCount != "0x0" {
					t.Errorf("Expected peerCount 0x0, got %s", peerCount)
				}
				return nil
			},
		},
		{
			method: "txpool_status",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var status map[string]interface{}
				if err := json.Unmarshal(result, &status); err != nil {
					return err
				}
				if status["pending"] != "0x0" || status["queued"] != "0x0" {
					t.Errorf("Expected empty txpool status, got %v", status)
				}
				return nil
			},
		},
		{
			method: "eth_mining",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var mining bool
				if err := json.Unmarshal(result, &mining); err != nil {
					return err
				}
				if mining != false {
					t.Errorf("Expected mining false, got %v", mining)
				}
				return nil
			},
		},
		{
			method: "eth_hashrate",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var hashrate string
				if err := json.Unmarshal(result, &hashrate); err != nil {
					return err
				}
				if hashrate != "0x0" {
					t.Errorf("Expected hashrate 0x0, got %s", hashrate)
				}
				return nil
			},
		},
		{
			method: "eth_syncing",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var syncing bool
				if err := json.Unmarshal(result, &syncing); err != nil {
					return err
				}
				if syncing != false {
					t.Errorf("Expected syncing false, got %v", syncing)
				}
				return nil
			},
		},
		{
			method: "eth_accounts",
			params: []interface{}{},
			checkResult: func(result json.RawMessage) error {
				var accounts []interface{}
				if err := json.Unmarshal(result, &accounts); err != nil {
					return err
				}
				if len(accounts) != 0 {
					t.Errorf("Expected empty accounts, got %v", accounts)
				}
				return nil
			},
		},
	}
	
	// Create test server with RPC handler
	server := httptest.NewServer(rpcHandler)
	defer server.Close()
	
	// Test each RPC method
	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			// Create RPC request
			reqBody := map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  tc.method,
				"params":  tc.params,
				"id":      1,
			}
			
			bodyBytes, err := json.Marshal(reqBody)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}
			
			// Send request
			resp, err := http.Post(server.URL, "application/json", bytes.NewReader(bodyBytes))
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()
			
			// Check response
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d", resp.StatusCode)
			}
			
			// Parse response
			var rpcResp struct {
				Jsonrpc string          `json:"jsonrpc"`
				Result  json.RawMessage `json:"result"`
				Error   *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				Id int `json:"id"`
			}
			
			if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}
			
			// Check for RPC error
			if rpcResp.Error != nil {
				t.Fatalf("RPC error: %s (code: %d)", rpcResp.Error.Message, rpcResp.Error.Code)
			}
			
			// Check result
			if tc.checkResult != nil {
				if err := tc.checkResult(rpcResp.Result); err != nil {
					t.Errorf("Result check failed: %v", err)
				}
			}
		})
	}
	
	t.Log("✅ All EVM RPC endpoints are properly registered and functional")
}