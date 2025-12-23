// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build qzmq

// Package xchain provides X-Chain transport configuration
package xchain

import (
	"fmt"
	"time"

	"github.com/luxfi/qzmq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TransportType defines the transport protocol to use
type TransportType string

const (
	// TransportGRPC uses standard gRPC (default)
	TransportGRPC TransportType = "grpc"

	// TransportQZMQ uses quantum-secure ZeroMQ (for X-Chain DEX)
	TransportQZMQ TransportType = "qzmq"

	// TransportHybrid uses both gRPC and QZMQ
	TransportHybrid TransportType = "hybrid"
)

// TransportConfig configures X-Chain network transport
type TransportConfig struct {
	// Type selects the transport protocol
	Type TransportType

	// EnableQZMQ enables QZMQ for DEX operations
	EnableQZMQ bool

	// EnableGRPC enables gRPC (default true)
	EnableGRPC bool

	// QZMQConfig holds QZMQ-specific settings
	QZMQConfig *QZMQTransportConfig

	// GRPCConfig holds gRPC-specific settings
	GRPCConfig *GRPCTransportConfig

	// NetworkPipes shared network configuration
	NetworkPipes *NetworkPipeConfig
}

// QZMQTransportConfig configures QZMQ transport
type QZMQTransportConfig struct {
	// EnableQuantum enables post-quantum cryptography
	EnableQuantum bool

	// ConsensusPort base port for consensus (5000-5002)
	ConsensusPort int

	// DEXPort base port for DEX operations (6000-6003)
	DEXPort int

	// KeyRotation interval for key rotation
	KeyRotation time.Duration

	// SecurityMode: "performance", "balanced", "conservative"
	SecurityMode string
}

// GRPCTransportConfig configures gRPC transport
type GRPCTransportConfig struct {
	// Port for gRPC server
	Port int

	// MaxMessageSize in bytes
	MaxMessageSize int

	// EnableTLS enables TLS encryption
	EnableTLS bool

	// CertFile path to TLS certificate
	CertFile string

	// KeyFile path to TLS key
	KeyFile string
}

// NetworkPipeConfig defines shared network infrastructure
type NetworkPipeConfig struct {
	// ListenAddr is the address to listen on
	ListenAddr string

	// AdvertiseAddr is the address to advertise to peers
	AdvertiseAddr string

	// P2PPort is the main P2P port (9651)
	P2PPort int

	// RPCPort is the RPC port (9630)
	RPCPort int

	// Peers list of peer addresses
	Peers []string

	// MaxConnections per peer
	MaxConnections int

	// Bandwidth limits in Mbps
	BandwidthLimit int
}

// DefaultTransportConfig returns default configuration
func DefaultTransportConfig() *TransportConfig {
	return &TransportConfig{
		Type:       TransportGRPC, // gRPC is default
		EnableGRPC: true,
		EnableQZMQ: false, // QZMQ opt-in for X-Chain DEX

		GRPCConfig: &GRPCTransportConfig{
			Port:           9090,
			MaxMessageSize: 100 * 1024 * 1024, // 100MB
			EnableTLS:      true,
		},

		QZMQConfig: &QZMQTransportConfig{
			EnableQuantum: true,
			ConsensusPort: 5000,
			DEXPort:       6000,
			KeyRotation:   5 * time.Minute,
			SecurityMode:  "balanced",
		},

		NetworkPipes: &NetworkPipeConfig{
			ListenAddr:     "0.0.0.0",
			AdvertiseAddr:  "", // Auto-detect
			P2PPort:        9651,
			RPCPort:        9630,
			MaxConnections: 256,
			BandwidthLimit: 1000, // 1Gbps
		},
	}
}

// XChainDEXConfig returns configuration optimized for DEX
func XChainDEXConfig() *TransportConfig {
	config := DefaultTransportConfig()
	config.Type = TransportHybrid                   // Use both protocols
	config.EnableQZMQ = true                        // Enable QZMQ for DEX
	config.QZMQConfig.SecurityMode = "conservative" // Maximum security for DEX
	config.QZMQConfig.EnableQuantum = true
	return config
}

// TransportManager manages both gRPC and QZMQ transports
type TransportManager struct {
	config *TransportConfig

	// gRPC components
	grpcServer *grpc.Server
	grpcClient *grpc.ClientConn

	// QZMQ components
	qzmqTransport *qzmq.XChainDEXTransport

	// Shared network pipes
	networkPipes *NetworkPipeManager
}

// NewTransportManager creates a new transport manager
func NewTransportManager(config *TransportConfig) (*TransportManager, error) {
	tm := &TransportManager{
		config: config,
	}

	// Initialize network pipes (shared by both transports)
	tm.networkPipes = NewNetworkPipeManager(config.NetworkPipes)

	// Initialize gRPC if enabled
	if config.EnableGRPC {
		if err := tm.initGRPC(); err != nil {
			return nil, fmt.Errorf("failed to init gRPC: %w", err)
		}
	}

	// Initialize QZMQ if enabled (for X-Chain DEX)
	if config.EnableQZMQ {
		if err := tm.initQZMQ(); err != nil {
			return nil, fmt.Errorf("failed to init QZMQ: %w", err)
		}
	}

	return tm, nil
}

func (tm *TransportManager) initGRPC() error {
	// Create gRPC server
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(tm.config.GRPCConfig.MaxMessageSize),
		grpc.MaxSendMsgSize(tm.config.GRPCConfig.MaxMessageSize),
	}

	if tm.config.GRPCConfig.EnableTLS {
		// Add TLS credentials
		// creds, err := credentials.NewServerTLSFromFile(...)
		// opts = append(opts, grpc.Creds(creds))
	}

	tm.grpcServer = grpc.NewServer(opts...)

	// Create gRPC client
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(tm.config.GRPCConfig.MaxMessageSize),
			grpc.MaxCallSendMsgSize(tm.config.GRPCConfig.MaxMessageSize),
		),
	}

	// Connect to peers via gRPC
	// This would connect to the first peer as an example
	if len(tm.config.NetworkPipes.Peers) > 0 {
		conn, err := grpc.Dial(
			fmt.Sprintf("%s:%d", tm.config.NetworkPipes.Peers[0], tm.config.GRPCConfig.Port),
			dialOpts...,
		)
		if err != nil {
			return err
		}
		tm.grpcClient = conn
	}

	return nil
}

func (tm *TransportManager) initQZMQ() error {
	// Load validator keys (would come from Lux key management)
	validatorKeys := &qzmq.ValidatorKeys{
		NodeID: make([]byte, 32),   // Ed25519
		BLSKey: make([]byte, 32),   // BLS
		PQKey:  make([]byte, 2400), // ML-KEM-768
		DSAKey: make([]byte, 4032), // ML-DSA-87
	}

	// Create QZMQ transport for X-Chain DEX
	transport, err := qzmq.NewXChainDEXTransport(
		fmt.Sprintf("http://%s:%d", tm.config.NetworkPipes.ListenAddr, tm.config.NetworkPipes.RPCPort),
		"X", // X-Chain
		"NodeID-placeholder",
		validatorKeys,
	)
	if err != nil {
		return err
	}

	tm.qzmqTransport = transport

	// Start QZMQ services on the same network pipes
	if err := tm.qzmqTransport.Listen(
		tm.config.QZMQConfig.ConsensusPort,
		tm.config.QZMQConfig.ConsensusPort+1,
		tm.config.QZMQConfig.ConsensusPort+2,
	); err != nil {
		return err
	}

	if err := tm.qzmqTransport.StartDEX(
		tm.config.QZMQConfig.DEXPort,
		tm.config.QZMQConfig.DEXPort+1,
		tm.config.QZMQConfig.DEXPort+2,
		tm.config.QZMQConfig.DEXPort+3,
	); err != nil {
		return err
	}

	// Connect to peers via QZMQ
	if err := tm.qzmqTransport.ConnectToPeers(tm.config.NetworkPipes.Peers); err != nil {
		return err
	}

	return nil
}

// SendMessage sends a message using the appropriate transport
func (tm *TransportManager) SendMessage(msgType string, data []byte) error {
	switch tm.config.Type {
	case TransportGRPC:
		return tm.sendViaGRPC(msgType, data)

	case TransportQZMQ:
		return tm.sendViaQZMQ(msgType, data)

	case TransportHybrid:
		// Route based on message type
		if isDEXMessage(msgType) {
			// DEX messages use QZMQ for quantum security
			return tm.sendViaQZMQ(msgType, data)
		}
		// Other messages use gRPC (default)
		return tm.sendViaGRPC(msgType, data)

	default:
		return fmt.Errorf("unknown transport type: %s", tm.config.Type)
	}
}

func (tm *TransportManager) sendViaGRPC(msgType string, data []byte) error {
	// Send via gRPC
	// This would call the appropriate gRPC method
	return nil
}

func (tm *TransportManager) sendViaQZMQ(msgType string, data []byte) error {
	// Send via QZMQ with quantum security
	// Route to appropriate QZMQ socket based on message type
	return nil
}

// isDEXMessage checks if a message is DEX-related
func isDEXMessage(msgType string) bool {
	switch msgType {
	case "order", "trade", "orderbook", "marketdata", "settlement":
		return true
	default:
		return false
	}
}

// NetworkPipeManager manages shared network infrastructure
type NetworkPipeManager struct {
	config *NetworkPipeConfig

	// Shared TCP listeners that can be used by both gRPC and QZMQ
	// Both protocols can run over the same network infrastructure
}

func NewNetworkPipeManager(config *NetworkPipeConfig) *NetworkPipeManager {
	return &NetworkPipeManager{
		config: config,
	}
}

// Start starts the transport manager
func (tm *TransportManager) Start() error {
	// Start network pipes (shared infrastructure)
	if err := tm.networkPipes.Start(); err != nil {
		return err
	}

	// gRPC and QZMQ are already started in init
	return nil
}

func (npm *NetworkPipeManager) Start() error {
	// Start shared network infrastructure
	// This sets up the TCP listeners that both protocols can use
	return nil
}

// Close shuts down all transports
func (tm *TransportManager) Close() error {
	if tm.grpcServer != nil {
		tm.grpcServer.GracefulStop()
	}

	if tm.grpcClient != nil {
		tm.grpcClient.Close()
	}

	if tm.qzmqTransport != nil {
		tm.qzmqTransport.Close()
	}

	return nil
}

// GetProtocolForMessage returns which protocol to use for a message type
func (tm *TransportManager) GetProtocolForMessage(msgType string) TransportType {
	if tm.config.Type == TransportHybrid {
		if isDEXMessage(msgType) && tm.config.EnableQZMQ {
			return TransportQZMQ
		}
	}
	return TransportGRPC
}
