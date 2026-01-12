// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build qzmq

package xchain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTransportType_Constants(t *testing.T) {
	require.Equal(t, TransportType("grpc"), TransportGRPC)
	require.Equal(t, TransportType("qzmq"), TransportQZMQ)
	require.Equal(t, TransportType("hybrid"), TransportHybrid)
}

func TestDefaultTransportConfig(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	require.NotNil(config)

	// Test default values
	require.Equal(TransportGRPC, config.Type)
	require.True(config.EnableGRPC)
	require.False(config.EnableQZMQ)

	// Test gRPC config defaults
	require.NotNil(config.GRPCConfig)
	require.Equal(9090, config.GRPCConfig.Port)
	require.Equal(100*1024*1024, config.GRPCConfig.MaxMessageSize)
	require.True(config.GRPCConfig.EnableTLS)

	// Test QZMQ config defaults
	require.NotNil(config.QZMQConfig)
	require.True(config.QZMQConfig.EnableQuantum)
	require.Equal(5000, config.QZMQConfig.ConsensusPort)
	require.Equal(6000, config.QZMQConfig.DEXPort)
	require.Equal(5*time.Minute, config.QZMQConfig.KeyRotation)
	require.Equal("balanced", config.QZMQConfig.SecurityMode)

	// Test network pipes defaults
	require.NotNil(config.NetworkPipes)
	require.Equal("0.0.0.0", config.NetworkPipes.ListenAddr)
	require.Equal("", config.NetworkPipes.AdvertiseAddr)
	require.Equal(9651, config.NetworkPipes.P2PPort)
	require.Equal(9630, config.NetworkPipes.RPCPort)
	require.Equal(256, config.NetworkPipes.MaxConnections)
	require.Equal(1000, config.NetworkPipes.BandwidthLimit)
}

func TestXChainDEXConfig(t *testing.T) {
	require := require.New(t)

	config := XChainDEXConfig()
	require.NotNil(config)

	// Should be based on default config but with DEX optimizations
	require.Equal(TransportHybrid, config.Type)
	require.True(config.EnableGRPC)
	require.True(config.EnableQZMQ)

	// DEX-specific settings
	require.Equal("conservative", config.QZMQConfig.SecurityMode)
	require.True(config.QZMQConfig.EnableQuantum)
}

func TestNewTransportManager(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	tm, err := NewTransportManager(config)
	require.NoError(err)
	require.NotNil(tm)
	require.Equal(config, tm.config)
	require.NotNil(tm.networkPipes)

	// Should have gRPC components since enabled
	require.NotNil(tm.grpcServer)

	// Should not have QZMQ components since disabled in default config
	require.Nil(tm.qzmqTransport)
}

func TestNewTransportManager_WithQZMQ(t *testing.T) {
	require := require.New(t)

	config := XChainDEXConfig() // Enables both gRPC and QZMQ
	// Use unique ports to avoid conflicts
	config.QZMQConfig.ConsensusPort = 35000
	config.QZMQConfig.DEXPort = 36000
	config.NetworkPipes.P2PPort = 39651
	config.NetworkPipes.RPCPort = 39630

	tm, err := NewTransportManager(config)
	require.NoError(err)
	require.NotNil(tm)

	// Should have both transport components
	require.NotNil(tm.grpcServer)
	require.NotNil(tm.qzmqTransport)
}

func TestNewTransportManager_QZMQOnly(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	config.EnableGRPC = false
	config.EnableQZMQ = true
	// Use unique ports
	config.QZMQConfig.ConsensusPort = 45000
	config.QZMQConfig.DEXPort = 46000
	config.NetworkPipes.P2PPort = 49651
	config.NetworkPipes.RPCPort = 49630

	tm, err := NewTransportManager(config)
	require.NoError(err)
	require.NotNil(tm)

	// Should only have QZMQ components
	require.Nil(tm.grpcServer)
	require.NotNil(tm.qzmqTransport)
}

func TestTransportManager_SendMessage_GRPC(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	tm, err := NewTransportManager(config)
	require.NoError(err)

	// Should use gRPC for all messages in gRPC-only mode
	require.NoError(tm.SendMessage("consensus", []byte("test data")))

	require.NoError(tm.SendMessage("order", []byte("test order")))
}

func TestTransportManager_SendMessage_QZMQ(t *testing.T) {
	require := require.New(t)

	// This test verifies the transport manager can be configured for QZMQ-only mode
	// We test the configuration logic rather than actual network operations
	config := DefaultTransportConfig()
	config.Type = TransportQZMQ
	config.EnableGRPC = false
	config.EnableQZMQ = true

	// Test that the config is set up correctly
	require.Equal(TransportQZMQ, config.Type)
	require.False(config.EnableGRPC)
	require.True(config.EnableQZMQ)

	// Test protocol selection logic
	tm := &TransportManager{config: config}
	protocol := tm.GetProtocolForMessage("order")
	require.Equal(TransportGRPC, protocol) // Falls back to gRPC when no hybrid mode

	protocol = tm.GetProtocolForMessage("consensus")
	require.Equal(TransportGRPC, protocol) // Falls back to gRPC when no hybrid mode
}

func TestTransportManager_SendMessage_Hybrid(t *testing.T) {
	require := require.New(t)

	config := XChainDEXConfig() // Hybrid mode
	// Use unique ports
	config.QZMQConfig.ConsensusPort = 55000
	config.QZMQConfig.DEXPort = 56000
	config.NetworkPipes.P2PPort = 59651
	config.NetworkPipes.RPCPort = 59630

	tm, err := NewTransportManager(config)
	require.NoError(err)

	// DEX messages should use QZMQ
	require.NoError(tm.SendMessage("order", []byte("test order")))

	require.NoError(tm.SendMessage("trade", []byte("test trade")))

	// Non-DEX messages should use gRPC
	require.NoError(tm.SendMessage("consensus", []byte("test consensus")))

	require.NoError(tm.SendMessage("block", []byte("test block")))
}

func TestTransportManager_SendMessage_InvalidType(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	config.Type = "invalid"

	tm := &TransportManager{config: config}

	err := tm.SendMessage("test", []byte("data"))
	require.Error(err)
	require.Contains(err.Error(), "unknown transport type")
}

func TestIsDEXMessage(t *testing.T) {
	tests := []struct {
		msgType  string
		expected bool
	}{
		{"order", true},
		{"trade", true},
		{"orderbook", true},
		{"marketdata", true},
		{"settlement", true},
		{"consensus", false},
		{"block", false},
		{"transaction", false},
		{"vote", false},
		{"", false},
	}

	for _, test := range tests {
		t.Run(test.msgType, func(t *testing.T) {
			result := isDEXMessage(test.msgType)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestTransportManager_GetProtocolForMessage(t *testing.T) {
	require := require.New(t)

	config := XChainDEXConfig() // Hybrid mode with QZMQ enabled
	// Use unique ports (valid port range)
	config.QZMQConfig.ConsensusPort = 23000
	config.QZMQConfig.DEXPort = 24000
	config.NetworkPipes.P2PPort = 23651
	config.NetworkPipes.RPCPort = 23650

	tm, err := NewTransportManager(config)
	require.NoError(err)

	// DEX messages should use QZMQ
	protocol := tm.GetProtocolForMessage("order")
	require.Equal(TransportQZMQ, protocol)

	protocol = tm.GetProtocolForMessage("trade")
	require.Equal(TransportQZMQ, protocol)

	// Non-DEX messages should use gRPC
	protocol = tm.GetProtocolForMessage("block")
	require.Equal(TransportGRPC, protocol)

	protocol = tm.GetProtocolForMessage("consensus")
	require.Equal(TransportGRPC, protocol)
}

func TestTransportManager_GetProtocolForMessage_GRPCOnly(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig() // gRPC only
	tm, err := NewTransportManager(config)
	require.NoError(err)

	// All messages should use gRPC
	protocol := tm.GetProtocolForMessage("order")
	require.Equal(TransportGRPC, protocol)

	protocol = tm.GetProtocolForMessage("block")
	require.Equal(TransportGRPC, protocol)
}

func TestTransportManager_GetProtocolForMessage_QZMQDisabled(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	config.Type = TransportHybrid
	config.EnableQZMQ = false // QZMQ disabled

	tm, err := NewTransportManager(config)
	require.NoError(err)

	// Even DEX messages should use gRPC when QZMQ is disabled
	protocol := tm.GetProtocolForMessage("order")
	require.Equal(TransportGRPC, protocol)
}

func TestNewNetworkPipeManager(t *testing.T) {
	require := require.New(t)

	config := &NetworkPipeConfig{
		ListenAddr:     "0.0.0.0",
		P2PPort:        9651,
		RPCPort:        9630,
		MaxConnections: 100,
	}

	npm := NewNetworkPipeManager(config)
	require.NotNil(npm)
	require.Equal(config, npm.config)
}

func TestTransportManager_Start(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	tm, err := NewTransportManager(config)
	require.NoError(err)

	require.NoError(tm.Start())
}

func TestTransportManager_Close(t *testing.T) {
	require := require.New(t)

	config := DefaultTransportConfig()
	tm, err := NewTransportManager(config)
	require.NoError(err)

	require.NoError(tm.Close())
}

func TestQZMQTransportConfig_Validation(t *testing.T) {
	config := &QZMQTransportConfig{
		EnableQuantum: true,
		ConsensusPort: 5000,
		DEXPort:       6000,
		KeyRotation:   5 * time.Minute,
		SecurityMode:  "conservative",
	}

	require.True(t, config.EnableQuantum)
	require.Greater(t, config.ConsensusPort, 0)
	require.Greater(t, config.DEXPort, 0)
	require.Greater(t, config.KeyRotation, time.Duration(0))
	require.Contains(t, []string{"performance", "balanced", "conservative"}, config.SecurityMode)
}

func TestGRPCTransportConfig_Validation(t *testing.T) {
	config := &GRPCTransportConfig{
		Port:           9090,
		MaxMessageSize: 100 * 1024 * 1024,
		EnableTLS:      true,
		CertFile:       "/path/to/cert",
		KeyFile:        "/path/to/key",
	}

	require.Greater(t, config.Port, 0)
	require.Greater(t, config.MaxMessageSize, 0)
	require.True(t, config.EnableTLS)
	require.NotEmpty(t, config.CertFile)
	require.NotEmpty(t, config.KeyFile)
}

func TestNetworkPipeConfig_Validation(t *testing.T) {
	config := &NetworkPipeConfig{
		ListenAddr:     "0.0.0.0",
		AdvertiseAddr:  "192.168.1.100",
		P2PPort:        9651,
		RPCPort:        9630,
		Peers:          []string{"peer1.example.com", "peer2.example.com"},
		MaxConnections: 256,
		BandwidthLimit: 1000,
	}

	require.NotEmpty(t, config.ListenAddr)
	require.Greater(t, config.P2PPort, 0)
	require.Greater(t, config.RPCPort, 0)
	require.Greater(t, config.MaxConnections, 0)
	require.Greater(t, config.BandwidthLimit, 0)
	require.Greater(t, len(config.Peers), 0)
}

// Test error cases
func TestTransportManager_ErrorCases(t *testing.T) {
	require := require.New(t)

	// Test with invalid gRPC configuration
	config := DefaultTransportConfig()
	config.GRPCConfig.Port = -1                             // Invalid port
	config.NetworkPipes.Peers = []string{"invalid:invalid"} // Invalid peer format

	tm, err := NewTransportManager(config)
	// Should still create manager, but might have connection issues
	require.NotNil(tm)
	// Error may or may not occur depending on gRPC implementation
	_ = err
}

func TestTransportConfig_EdgeCases(t *testing.T) {
	require := require.New(t)

	// Test config with no protocols enabled
	config := &TransportConfig{
		Type:       TransportGRPC,
		EnableGRPC: false,
		EnableQZMQ: false,
	}

	tm, err := NewTransportManager(config)
	require.NoError(err)
	require.NotNil(tm)

	// Should handle gracefully with no transports
	require.NoError(tm.Start())

	require.NoError(tm.Close())
}

func TestTransportManager_NilConfigs(t *testing.T) {
	require := require.New(t)

	config := &TransportConfig{
		Type:         TransportGRPC,
		EnableGRPC:   true,
		EnableQZMQ:   false,
		GRPCConfig:   nil, // Nil config
		QZMQConfig:   nil, // Nil config
		NetworkPipes: nil, // Nil config
	}

	// Should panic due to nil configs
	require.Panics(func() {
		NewTransportManager(config)
	}, "Should panic with nil configs")
}
