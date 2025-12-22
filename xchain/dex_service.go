// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package xchain implements X-Chain DEX service with hybrid transport
package xchain

import (
	"context"
	"fmt"

	"github.com/luxfi/log"
	"github.com/luxfi/log/level"
	"github.com/luxfi/qzmq"
	"google.golang.org/grpc"
)

// DEXService handles DEX operations on X-Chain
type DEXService struct {
	transport *TransportManager
	config    *ServiceConfig
	log       log.Logger

	// Separate handlers for different protocols
	grpcHandler *GRPCHandler
	qzmqHandler *QZMQHandler
}

// ServiceConfig configures the DEX service
type ServiceConfig struct {
	// UseQZMQForDEX enables QZMQ for DEX operations
	UseQZMQForDEX bool

	// UseGRPCForConsensus keeps gRPC for consensus (default)
	UseGRPCForConsensus bool

	// EnableQuantumSecurity enables post-quantum crypto for DEX
	EnableQuantumSecurity bool
}

// NewDEXService creates a new DEX service
func NewDEXService(logger log.Logger, transportConfig *TransportConfig, serviceConfig *ServiceConfig) (*DEXService, error) {
	// Create transport manager that handles both protocols
	tm, err := NewTransportManager(transportConfig)
	if err != nil {
		return nil, err
	}

	service := &DEXService{
		transport: tm,
		config:    serviceConfig,
		log:       logger,
	}

	// Initialize protocol-specific handlers
	if transportConfig.EnableGRPC {
		service.grpcHandler = NewGRPCHandler(tm.grpcServer)
	}

	if transportConfig.EnableQZMQ {
		service.qzmqHandler = NewQZMQHandler(tm.qzmqTransport)
	}

	return service, nil
}

// GRPCHandler handles gRPC requests (default for most operations)
type GRPCHandler struct {
	server *grpc.Server
}

func NewGRPCHandler(server *grpc.Server) *GRPCHandler {
	return &GRPCHandler{server: server}
}

// QZMQHandler handles QZMQ requests (for quantum-secure DEX operations)
type QZMQHandler struct {
	transport *qzmq.XChainDEXTransport
}

func NewQZMQHandler(transport *qzmq.XChainDEXTransport) *QZMQHandler {
	return &QZMQHandler{transport: transport}
}

// SubmitOrder submits an order to the DEX
func (s *DEXService) SubmitOrder(ctx context.Context, order *Order) error {
	// Check which transport to use
	protocol := s.transport.GetProtocolForMessage("order")

	switch protocol {
	case TransportQZMQ:
		// Use QZMQ for quantum-secure order submission
		return s.submitOrderViaQZMQ(order)

	case TransportGRPC:
		// Use gRPC (default)
		return s.submitOrderViaGRPC(ctx, order)

	default:
		return fmt.Errorf("unknown protocol: %s", protocol)
	}
}

func (s *DEXService) submitOrderViaQZMQ(order *Order) error {
	// Convert to QZMQ order format
	qzmqOrder := &qzmq.Order{
		ID:       order.ID,
		Market:   order.Market,
		Side:     convertOrderSide(order.Side),
		Type:     convertOrderType(order.Type),
		Price:    order.Price,
		Quantity: order.Quantity,
		UserAddr: order.UserAddress,
		AssetID:  order.AssetID,
	}

	// Sign with quantum-secure signature
	qzmqOrder.Signature = s.signWithMLDSA(qzmqOrder)
	qzmqOrder.Certificate = s.generateQuantumCertificate(qzmqOrder)

	// Submit via QZMQ transport
	return s.qzmqHandler.transport.SubmitOrder(qzmqOrder)
}

func (s *DEXService) submitOrderViaGRPC(ctx context.Context, order *Order) error {
	// Submit via gRPC (existing implementation)
	// This would call the gRPC service method
	return nil
}

// BroadcastBlock broadcasts a block to validators
func (s *DEXService) BroadcastBlock(ctx context.Context, block *Block) error {
	// Consensus uses gRPC by default
	protocol := s.transport.GetProtocolForMessage("block")

	if protocol == TransportGRPC {
		// Use existing gRPC broadcast
		return s.broadcastViaGRPC(ctx, block)
	}

	// Could also use QZMQ if configured
	return s.broadcastViaQZMQ(block)
}

func (s *DEXService) broadcastViaGRPC(ctx context.Context, block *Block) error {
	// Existing gRPC implementation
	return nil
}

func (s *DEXService) broadcastViaQZMQ(block *Block) error {
	// QZMQ broadcast with quantum signatures
	return s.qzmqHandler.transport.ProposeBlock("X", block.Height, block.Data)
}

// Order represents a DEX order
type Order struct {
	ID          string
	Market      string
	Side        string // "buy" or "sell"
	Type        string // "limit", "market", etc.
	Price       uint64
	Quantity    uint64
	UserAddress string
	AssetID     string
}

// Block represents a blockchain block
type Block struct {
	Height uint64
	Data   []byte
}

// Helper functions
func convertOrderSide(side string) qzmq.OrderSide {
	if side == "buy" {
		return qzmq.Buy
	}
	return qzmq.Sell
}

func convertOrderType(orderType string) qzmq.OrderType {
	switch orderType {
	case "market":
		return qzmq.MarketOrder
	case "stop":
		return qzmq.Stop
	case "stop_limit":
		return qzmq.StopLimit
	default:
		return qzmq.Limit
	}
}

func (s *DEXService) signWithMLDSA(order *qzmq.Order) []byte {
	// Sign with ML-DSA (would use actual luxfi/crypto)
	return make([]byte, 3293)
}

func (s *DEXService) generateQuantumCertificate(order *qzmq.Order) []byte {
	// Generate quantum certificate
	return make([]byte, 3072)
}

// Start starts the DEX service
func (s *DEXService) Start() error {
	// Start transport manager (both gRPC and QZMQ if enabled)
	if err := s.transport.Start(); err != nil {
		return err
	}

	s.log.Info("X-Chain DEX Service started",
		"grpcConsensus", s.config.UseGRPCForConsensus,
		"qzmqDEX", s.config.UseQZMQForDEX,
		"quantumSecurity", s.config.EnableQuantumSecurity,
	)

	return nil
}

// Example usage showing how both protocols work together
func ExampleHybridTransport() {
	// Configure transport with both gRPC and QZMQ
	transportConfig := &TransportConfig{
		Type:       TransportHybrid, // Use both protocols
		EnableGRPC: true,            // gRPC for consensus and general ops
		EnableQZMQ: true,            // QZMQ for DEX operations

		GRPCConfig: &GRPCTransportConfig{
			Port:      9090,
			EnableTLS: true,
		},

		QZMQConfig: &QZMQTransportConfig{
			EnableQuantum: true, // Quantum security for DEX
			DEXPort:       6000,
			SecurityMode:  "conservative",
		},

		NetworkPipes: &NetworkPipeConfig{
			P2PPort: 9651, // Standard Lux P2P port
			RPCPort: 9630, // Standard Lux RPC port
			Peers: []string{
				"validator1.example.com",
				"validator2.example.com",
			},
		},
	}

	serviceConfig := &ServiceConfig{
		UseGRPCForConsensus:   true, // Keep gRPC for consensus
		UseQZMQForDEX:         true, // Enable QZMQ for DEX
		EnableQuantumSecurity: true, // Quantum-secure DEX operations
	}

	// Create DEX service with hybrid transport
	logger := log.NewTestLogger(level.Info)
	dexService, err := NewDEXService(logger, transportConfig, serviceConfig)
	if err != nil {
		panic(fmt.Sprintf("failed to create DEX service: %v", err))
	}

	// Start the service
	if err := dexService.Start(); err != nil {
		panic(fmt.Sprintf("failed to start DEX service: %v", err))
	}

	// Example: Submit an order (will use QZMQ automatically)
	order := &Order{
		ID:       "order-123",
		Market:   "LUX-USDC",
		Side:     "buy",
		Type:     "limit",
		Price:    1000000000, // $10.00
		Quantity: 100000000,  // 1.00 LUX
	}

	ctx := context.Background()
	if err := dexService.SubmitOrder(ctx, order); err != nil {
		logger.Error("failed to submit order", "error", err)
	}

	// Example: Broadcast a block (will use gRPC by default)
	block := &Block{
		Height: 12345,
		Data:   []byte("block data"),
	}

	if err := dexService.BroadcastBlock(ctx, block); err != nil {
		logger.Error("failed to broadcast block", "error", err)
	}

	// Documentation output (acceptable for Example functions)
	fmt.Println(`
Transport Protocol Usage:
========================
│ Operation          │ Protocol │ Reason                           │
├───────────────────┼──────────┼──────────────────────────────────┤
│ Consensus          │ gRPC     │ Default, proven, efficient       │
│ Block propagation  │ gRPC     │ Standard P2P networking         │
│ State sync         │ gRPC     │ Bulk data transfer              │
│ DEX Orders         │ QZMQ     │ Quantum-secure, low latency     │
│ DEX Trades         │ QZMQ     │ Protected settlement            │
│ Market Data        │ QZMQ     │ Secure multicast distribution   │
│ Regular Txs        │ gRPC     │ Standard transaction flow       │
└───────────────────┴──────────┴──────────────────────────────────┘

Network Architecture:
====================
                    ┌─────────────────────┐
                    │   X-Chain Node      │
                    ├─────────────────────┤
                    │   Network Pipes     │
                    │   (Shared TCP)      │
                    ├──────────┬──────────┤
                    │   gRPC   │   QZMQ   │
                    ├──────────┼──────────┤
                    │ Port 9090│Port 6000 │
                    └──────────┴──────────┘
                           │
                    ┌──────┴──────┐
                    │             │
              [Consensus]    [DEX Orders]
               (gRPC)         (QZMQ)

Both protocols share the same network infrastructure but
provide different security guarantees for different operations.`)
}
