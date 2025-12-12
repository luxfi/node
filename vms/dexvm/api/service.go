// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package api provides RPC and REST API handlers for the DEX VM.
package api

import (
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/dexvm/liquidity"
	"github.com/luxfi/node/vms/dexvm/orderbook"
)

var (
	ErrNotBootstrapped     = errors.New("DEX not bootstrapped")
	ErrInvalidRequest      = errors.New("invalid request")
	ErrOrderNotFound       = errors.New("order not found")
	ErrPoolNotFound        = errors.New("pool not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
)

// VM interface for the API service.
type VM interface {
	IsBootstrapped() bool
	GetOrderbook(symbol string) (*orderbook.Orderbook, error)
	GetOrCreateOrderbook(symbol string) *orderbook.Orderbook
	GetLiquidityManager() *liquidity.Manager
}

// Service provides the RPC API for the DEX VM.
type Service struct {
	vm VM
}

// NewService creates a new API service.
func NewService(vm VM) *Service {
	return &Service{vm: vm}
}

// ============================================
// Health and Status APIs
// ============================================

// PingArgs is the argument for the Ping API.
type PingArgs struct{}

// PingReply is the reply for the Ping API.
type PingReply struct {
	Success bool `json:"success"`
}

// Ping returns a simple health check response.
func (s *Service) Ping(_ *http.Request, _ *PingArgs, reply *PingReply) error {
	reply.Success = true
	return nil
}

// StatusArgs is the argument for the Status API.
type StatusArgs struct{}

// StatusReply is the reply for the Status API.
type StatusReply struct {
	Bootstrapped bool   `json:"bootstrapped"`
	Version      string `json:"version"`
	Uptime       int64  `json:"uptime"`
}

// Status returns the DEX status.
func (s *Service) Status(_ *http.Request, _ *StatusArgs, reply *StatusReply) error {
	reply.Bootstrapped = s.vm.IsBootstrapped()
	reply.Version = "1.0.0"
	return nil
}

// ============================================
// Orderbook APIs
// ============================================

// GetOrderbookArgs is the argument for the GetOrderbook API.
type GetOrderbookArgs struct {
	Symbol string `json:"symbol"`
	Depth  int    `json:"depth"`
}

// GetOrderbookReply is the reply for the GetOrderbook API.
type GetOrderbookReply struct {
	Symbol    string                  `json:"symbol"`
	Bids      []*orderbook.PriceLevel `json:"bids"`
	Asks      []*orderbook.PriceLevel `json:"asks"`
	BestBid   uint64                  `json:"bestBid"`
	BestAsk   uint64                  `json:"bestAsk"`
	Spread    uint64                  `json:"spread"`
	MidPrice  uint64                  `json:"midPrice"`
	Timestamp int64                   `json:"timestamp"`
}

// GetOrderbook returns the current orderbook for a symbol.
func (s *Service) GetOrderbook(_ *http.Request, args *GetOrderbookArgs, reply *GetOrderbookReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	if args.Symbol == "" {
		return fmt.Errorf("%w: symbol required", ErrInvalidRequest)
	}

	depth := args.Depth
	if depth <= 0 {
		depth = 20
	}

	ob, err := s.vm.GetOrderbook(args.Symbol)
	if err != nil {
		return err
	}

	bids, asks := ob.GetDepth(depth)

	reply.Symbol = args.Symbol
	reply.Bids = bids
	reply.Asks = asks
	reply.BestBid = ob.GetBestBid()
	reply.BestAsk = ob.GetBestAsk()
	reply.Spread = ob.GetSpread()
	reply.MidPrice = ob.GetMidPrice()
	reply.Timestamp = time.Now().UnixNano()

	return nil
}

// PlaceOrderArgs is the argument for the PlaceOrder API.
type PlaceOrderArgs struct {
	Owner       string `json:"owner"`       // hex-encoded address
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`        // "buy" or "sell"
	Type        string `json:"type"`        // "limit", "market", etc.
	Price       uint64 `json:"price"`
	Quantity    uint64 `json:"quantity"`
	TimeInForce string `json:"timeInForce"` // "GTC", "IOC", "FOK"
	PostOnly    bool   `json:"postOnly"`
	ReduceOnly  bool   `json:"reduceOnly"`
}

// PlaceOrderReply is the reply for the PlaceOrder API.
type PlaceOrderReply struct {
	OrderID   string             `json:"orderId"`
	Status    string             `json:"status"`
	FilledQty uint64             `json:"filledQty"`
	Trades    []*orderbook.Trade `json:"trades"`
}

// PlaceOrder places a new order on the orderbook.
func (s *Service) PlaceOrder(_ *http.Request, args *PlaceOrderArgs, reply *PlaceOrderReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	if args.Symbol == "" {
		return fmt.Errorf("%w: symbol required", ErrInvalidRequest)
	}
	if args.Quantity == 0 {
		return fmt.Errorf("%w: quantity required", ErrInvalidRequest)
	}

	// Parse owner address
	ownerBytes, err := ids.ShortFromString(args.Owner)
	if err != nil {
		return fmt.Errorf("%w: invalid owner address", ErrInvalidRequest)
	}

	// Parse side
	var side orderbook.Side
	switch args.Side {
	case "buy":
		side = orderbook.Buy
	case "sell":
		side = orderbook.Sell
	default:
		return fmt.Errorf("%w: invalid side (must be 'buy' or 'sell')", ErrInvalidRequest)
	}

	// Parse order type
	var orderType orderbook.OrderType
	switch args.Type {
	case "limit":
		orderType = orderbook.Limit
	case "market":
		orderType = orderbook.Market
	case "stop_loss":
		orderType = orderbook.StopLoss
	case "take_profit":
		orderType = orderbook.TakeProfit
	case "stop_limit":
		orderType = orderbook.StopLimit
	default:
		orderType = orderbook.Limit
	}

	// Create order
	order := &orderbook.Order{
		ID:          ids.GenerateTestID(),
		Owner:       ownerBytes,
		Symbol:      args.Symbol,
		Side:        side,
		Type:        orderType,
		Price:       args.Price,
		Quantity:    args.Quantity,
		TimeInForce: args.TimeInForce,
		PostOnly:    args.PostOnly,
		ReduceOnly:  args.ReduceOnly,
		CreatedAt:   time.Now().UnixNano(),
		Status:      orderbook.StatusOpen,
	}

	// Get or create orderbook
	ob := s.vm.GetOrCreateOrderbook(args.Symbol)

	// Add order
	trades, err := ob.AddOrder(order)
	if err != nil {
		return err
	}

	reply.OrderID = order.ID.String()
	reply.Status = order.Status.String()
	reply.FilledQty = order.FilledQty
	reply.Trades = trades

	return nil
}

// CancelOrderArgs is the argument for the CancelOrder API.
type CancelOrderArgs struct {
	OrderID string `json:"orderId"`
	Symbol  string `json:"symbol"`
}

// CancelOrderReply is the reply for the CancelOrder API.
type CancelOrderReply struct {
	Success bool `json:"success"`
}

// CancelOrder cancels an existing order.
func (s *Service) CancelOrder(_ *http.Request, args *CancelOrderArgs, reply *CancelOrderReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	orderID, err := ids.FromString(args.OrderID)
	if err != nil {
		return fmt.Errorf("%w: invalid order ID", ErrInvalidRequest)
	}

	ob, err := s.vm.GetOrderbook(args.Symbol)
	if err != nil {
		return err
	}

	if err := ob.CancelOrder(orderID); err != nil {
		return err
	}

	reply.Success = true
	return nil
}

// GetOrderArgs is the argument for the GetOrder API.
type GetOrderArgs struct {
	OrderID string `json:"orderId"`
	Symbol  string `json:"symbol"`
}

// GetOrderReply is the reply for the GetOrder API.
type GetOrderReply struct {
	Order *orderbook.Order `json:"order"`
}

// GetOrder returns an order by ID.
func (s *Service) GetOrder(_ *http.Request, args *GetOrderArgs, reply *GetOrderReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	orderID, err := ids.FromString(args.OrderID)
	if err != nil {
		return fmt.Errorf("%w: invalid order ID", ErrInvalidRequest)
	}

	ob, err := s.vm.GetOrderbook(args.Symbol)
	if err != nil {
		return err
	}

	order, err := ob.GetOrder(orderID)
	if err != nil {
		return err
	}

	reply.Order = order
	return nil
}

// ============================================
// Liquidity Pool APIs
// ============================================

// GetPoolsArgs is the argument for the GetPools API.
type GetPoolsArgs struct{}

// GetPoolsReply is the reply for the GetPools API.
type GetPoolsReply struct {
	Pools []*liquidity.Pool `json:"pools"`
}

// GetPools returns all liquidity pools.
func (s *Service) GetPools(_ *http.Request, _ *GetPoolsArgs, reply *GetPoolsReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	reply.Pools = s.vm.GetLiquidityManager().GetAllPools()
	return nil
}

// GetPoolArgs is the argument for the GetPool API.
type GetPoolArgs struct {
	PoolID string `json:"poolId"`
}

// GetPoolReply is the reply for the GetPool API.
type GetPoolReply struct {
	Pool *liquidity.Pool `json:"pool"`
}

// GetPool returns a specific liquidity pool.
func (s *Service) GetPool(_ *http.Request, args *GetPoolArgs, reply *GetPoolReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	poolID, err := ids.FromString(args.PoolID)
	if err != nil {
		return fmt.Errorf("%w: invalid pool ID", ErrInvalidRequest)
	}

	pool, err := s.vm.GetLiquidityManager().GetPool(poolID)
	if err != nil {
		return err
	}

	reply.Pool = pool
	return nil
}

// GetQuoteArgs is the argument for the GetQuote API.
type GetQuoteArgs struct {
	PoolID   string `json:"poolId"`
	TokenIn  string `json:"tokenIn"`
	AmountIn string `json:"amountIn"` // String for big.Int
}

// GetQuoteReply is the reply for the GetQuote API.
type GetQuoteReply struct {
	AmountOut     string `json:"amountOut"`
	EffectiveRate string `json:"effectiveRate"`
}

// GetQuote returns a swap quote.
func (s *Service) GetQuote(_ *http.Request, args *GetQuoteArgs, reply *GetQuoteReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	poolID, err := ids.FromString(args.PoolID)
	if err != nil {
		return fmt.Errorf("%w: invalid pool ID", ErrInvalidRequest)
	}

	tokenIn, err := ids.FromString(args.TokenIn)
	if err != nil {
		return fmt.Errorf("%w: invalid token ID", ErrInvalidRequest)
	}

	amountIn, ok := new(big.Int).SetString(args.AmountIn, 10)
	if !ok {
		return fmt.Errorf("%w: invalid amount", ErrInvalidRequest)
	}

	amountOut, err := s.vm.GetLiquidityManager().GetQuote(poolID, tokenIn, amountIn)
	if err != nil {
		return err
	}

	reply.AmountOut = amountOut.String()
	if amountIn.Sign() > 0 {
		rate := new(big.Float).Quo(
			new(big.Float).SetInt(amountOut),
			new(big.Float).SetInt(amountIn),
		)
		reply.EffectiveRate = rate.Text('f', 8)
	}

	return nil
}

// SwapArgs is the argument for the Swap API.
type SwapArgs struct {
	PoolID       string `json:"poolId"`
	TokenIn      string `json:"tokenIn"`
	AmountIn     string `json:"amountIn"`
	MinAmountOut string `json:"minAmountOut"`
}

// SwapReply is the reply for the Swap API.
type SwapReply struct {
	AmountOut string `json:"amountOut"`
	Fee       string `json:"fee"`
}

// Swap executes a swap on a liquidity pool.
func (s *Service) Swap(_ *http.Request, args *SwapArgs, reply *SwapReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	poolID, err := ids.FromString(args.PoolID)
	if err != nil {
		return fmt.Errorf("%w: invalid pool ID", ErrInvalidRequest)
	}

	tokenIn, err := ids.FromString(args.TokenIn)
	if err != nil {
		return fmt.Errorf("%w: invalid token ID", ErrInvalidRequest)
	}

	amountIn, ok := new(big.Int).SetString(args.AmountIn, 10)
	if !ok {
		return fmt.Errorf("%w: invalid amountIn", ErrInvalidRequest)
	}

	minAmountOut, ok := new(big.Int).SetString(args.MinAmountOut, 10)
	if !ok {
		return fmt.Errorf("%w: invalid minAmountOut", ErrInvalidRequest)
	}

	result, err := s.vm.GetLiquidityManager().Swap(poolID, tokenIn, amountIn, minAmountOut)
	if err != nil {
		return err
	}

	reply.AmountOut = result.AmountOut.String()
	reply.Fee = result.Fee.String()

	return nil
}

// AddLiquidityArgs is the argument for the AddLiquidity API.
type AddLiquidityArgs struct {
	PoolID       string `json:"poolId"`
	Amount0      string `json:"amount0"`
	Amount1      string `json:"amount1"`
	MinLiquidity string `json:"minLiquidity"`
}

// AddLiquidityReply is the reply for the AddLiquidity API.
type AddLiquidityReply struct {
	LPTokens string `json:"lpTokens"`
}

// AddLiquidity adds liquidity to a pool.
func (s *Service) AddLiquidity(_ *http.Request, args *AddLiquidityArgs, reply *AddLiquidityReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	poolID, err := ids.FromString(args.PoolID)
	if err != nil {
		return fmt.Errorf("%w: invalid pool ID", ErrInvalidRequest)
	}

	amount0, ok := new(big.Int).SetString(args.Amount0, 10)
	if !ok {
		return fmt.Errorf("%w: invalid amount0", ErrInvalidRequest)
	}

	amount1, ok := new(big.Int).SetString(args.Amount1, 10)
	if !ok {
		return fmt.Errorf("%w: invalid amount1", ErrInvalidRequest)
	}

	minLiquidity, ok := new(big.Int).SetString(args.MinLiquidity, 10)
	if !ok {
		return fmt.Errorf("%w: invalid minLiquidity", ErrInvalidRequest)
	}

	lpTokens, err := s.vm.GetLiquidityManager().AddLiquidity(poolID, amount0, amount1, minLiquidity)
	if err != nil {
		return err
	}

	reply.LPTokens = lpTokens.String()

	return nil
}

// RemoveLiquidityArgs is the argument for the RemoveLiquidity API.
type RemoveLiquidityArgs struct {
	PoolID     string `json:"poolId"`
	Liquidity  string `json:"liquidity"`
	MinAmount0 string `json:"minAmount0"`
	MinAmount1 string `json:"minAmount1"`
}

// RemoveLiquidityReply is the reply for the RemoveLiquidity API.
type RemoveLiquidityReply struct {
	Amount0 string `json:"amount0"`
	Amount1 string `json:"amount1"`
}

// RemoveLiquidity removes liquidity from a pool.
func (s *Service) RemoveLiquidity(_ *http.Request, args *RemoveLiquidityArgs, reply *RemoveLiquidityReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	poolID, err := ids.FromString(args.PoolID)
	if err != nil {
		return fmt.Errorf("%w: invalid pool ID", ErrInvalidRequest)
	}

	liquidity, ok := new(big.Int).SetString(args.Liquidity, 10)
	if !ok {
		return fmt.Errorf("%w: invalid liquidity", ErrInvalidRequest)
	}

	minAmount0, ok := new(big.Int).SetString(args.MinAmount0, 10)
	if !ok {
		return fmt.Errorf("%w: invalid minAmount0", ErrInvalidRequest)
	}

	minAmount1, ok := new(big.Int).SetString(args.MinAmount1, 10)
	if !ok {
		return fmt.Errorf("%w: invalid minAmount1", ErrInvalidRequest)
	}

	amount0, amount1, err := s.vm.GetLiquidityManager().RemoveLiquidity(poolID, liquidity, minAmount0, minAmount1)
	if err != nil {
		return err
	}

	reply.Amount0 = amount0.String()
	reply.Amount1 = amount1.String()

	return nil
}

// ============================================
// Statistics APIs
// ============================================

// GetStatsArgs is the argument for the GetStats API.
type GetStatsArgs struct {
	Symbol string `json:"symbol"`
}

// GetStatsReply is the reply for the GetStats API.
type GetStatsReply struct {
	TotalVolume   uint64 `json:"totalVolume"`
	TradeCount    uint64 `json:"tradeCount"`
	LastTradeTime int64  `json:"lastTradeTime"`
}

// GetStats returns trading statistics for a symbol.
func (s *Service) GetStats(_ *http.Request, args *GetStatsArgs, reply *GetStatsReply) error {
	if !s.vm.IsBootstrapped() {
		return ErrNotBootstrapped
	}

	ob, err := s.vm.GetOrderbook(args.Symbol)
	if err != nil {
		return err
	}

	totalVolume, tradeCount, lastTradeTime := ob.GetStats()

	reply.TotalVolume = totalVolume
	reply.TradeCount = tradeCount
	reply.LastTradeTime = lastTradeTime

	return nil
}
