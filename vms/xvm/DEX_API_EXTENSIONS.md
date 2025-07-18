# X-Chain DEX API Extensions Documentation

## Overview
This document outlines the API extensions required to transform the X-Chain into a high-performance decentralized exchange (DEX) comparable to centralized exchanges (CEX) and Hyperliquid. These extensions enable spot and perpetual futures trading with order book functionality, achieving performance targets of 200,000+ orders per second.

## Table of Contents
1. [Trading API Extensions](#1-trading-api-extensions)
2. [Market Data API Extensions](#2-market-data-api-extensions)
3. [Account & Risk Management API Extensions](#3-account--risk-management-api-extensions)
4. [Advanced Features API](#4-advanced-features-api)
5. [WebSocket API for Real-time Updates](#5-websocket-api-for-real-time-updates)
6. [Performance Requirements](#6-performance-requirements)
7. [Implementation Considerations](#7-implementation-considerations)

## 1. Trading API Extensions

### 1.1 Order Management

#### `xvm.placeOrder`
Place a new order on the exchange.

**Signature:**
```
xvm.placeOrder({
    market: string,           // e.g., "BTC-USDT", "ETH-PERP"
    side: string,            // "buy" or "sell"
    orderType: string,       // "market", "limit", "stop", "stopLimit", "trailingStop"
    size: string,            // Order size in base currency
    price: string,           // Required for limit orders
    stopPrice: string,       // Required for stop orders
    leverage: int,           // 1-50x for perpetuals
    reduceOnly: bool,        // For perpetuals: only reduce position
    postOnly: bool,          // Maker-only order
    timeInForce: string,     // "GTC", "IOC", "FOK", "GTX"
    clientOrderId: string,   // Optional client-specified ID
}) -> {
    orderId: string,
    clientOrderId: string,
    status: string,
    timestamp: int64
}
```

**Example Call:**
```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id": 1,
    "method": "xvm.placeOrder",
    "params": {
        "market": "BTC-USDT",
        "side": "buy",
        "orderType": "limit",
        "size": "0.1",
        "price": "45000",
        "timeInForce": "GTC"
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

#### `xvm.cancelOrder`
Cancel an existing order.

**Signature:**
```
xvm.cancelOrder({
    orderId: string,         // Exchange order ID
    market: string,          // Market symbol
}) -> {
    orderId: string,
    status: string,          // "cancelled"
    timestamp: int64
}
```

#### `xvm.cancelAllOrders`
Cancel all open orders for a market or all markets.

**Signature:**
```
xvm.cancelAllOrders({
    market: string,          // Optional: specific market
}) -> {
    cancelledCount: int,
    orderIds: []string
}
```

#### `xvm.modifyOrder`
Modify an existing order's price or size.

**Signature:**
```
xvm.modifyOrder({
    orderId: string,
    price: string,           // New price (optional)
    size: string,            // New size (optional)
}) -> {
    orderId: string,
    newOrderId: string,      // Some implementations create new order
    status: string,
    timestamp: int64
}
```

### 1.2 Order Query

#### `xvm.getOpenOrders`
Get all open orders for the user.

**Signature:**
```
xvm.getOpenOrders({
    market: string,          // Optional: filter by market
    limit: int,              // Max results (default: 100)
    offset: int,             // Pagination offset
}) -> {
    orders: [{
        orderId: string,
        clientOrderId: string,
        market: string,
        side: string,
        orderType: string,
        status: string,
        price: string,
        size: string,
        filled: string,
        remaining: string,
        timestamp: int64,
        updateTime: int64
    }],
    total: int
}
```

#### `xvm.getOrderHistory`
Get historical orders with filters.

**Signature:**
```
xvm.getOrderHistory({
    market: string,          // Optional
    status: string,          // Optional: "filled", "cancelled", "rejected"
    startTime: int64,        // Unix timestamp
    endTime: int64,          // Unix timestamp
    limit: int,
    offset: int,
}) -> {
    orders: [{
        orderId: string,
        market: string,
        side: string,
        orderType: string,
        status: string,
        price: string,
        size: string,
        filled: string,
        avgFillPrice: string,
        fee: string,
        timestamp: int64,
        updateTime: int64
    }],
    total: int
}
```

### 1.3 Position Management (Perpetuals)

#### `xvm.getPositions`
Get all open positions.

**Signature:**
```
xvm.getPositions({
    market: string,          // Optional: filter by market
}) -> {
    positions: [{
        market: string,
        side: string,           // "long" or "short"
        size: string,
        notional: string,
        entryPrice: string,
        markPrice: string,
        liquidationPrice: string,
        unrealizedPnl: string,
        realizedPnl: string,
        margin: string,
        marginRatio: string,
        leverage: int,
        openTime: int64
    }]
}
```

#### `xvm.closePosition`
Close or reduce a position.

**Signature:**
```
xvm.closePosition({
    market: string,
    size: string,            // Optional: partial close
}) -> {
    orderId: string,
    status: string,
    timestamp: int64
}
```

#### `xvm.adjustMargin`
Add or remove margin from a position.

**Signature:**
```
xvm.adjustMargin({
    market: string,
    amount: string,          // Positive to add, negative to remove
}) -> {
    market: string,
    newMargin: string,
    leverage: int,
    liquidationPrice: string
}
```

## 2. Market Data API Extensions

### 2.1 Market Information

#### `xvm.getMarkets`
Get information about all available markets.

**Signature:**
```
xvm.getMarkets({
    type: string,            // Optional: "spot", "perpetual"
}) -> {
    markets: [{
        symbol: string,      // e.g., "BTC-USDT", "ETH-PERP"
        type: string,        // "spot" or "perpetual"
        baseCurrency: string,
        quoteCurrency: string,
        status: string,      // "active", "suspended", "delisted"
        minOrderSize: string,
        maxOrderSize: string,
        tickSize: string,    // Min price increment
        sizeIncrement: string,
        makerFee: string,
        takerFee: string,
        // For perpetuals:
        contractSize: string,
        fundingRate: string,
        nextFundingTime: int64,
        maxLeverage: int,
        initialMargin: string,
        maintenanceMargin: string
    }]
}
```

#### `xvm.getMarketStats`
Get 24-hour statistics for markets.

**Signature:**
```
xvm.getMarketStats({
    market: string,          // Optional: specific market
}) -> {
    stats: [{
        market: string,
        lastPrice: string,
        indexPrice: string,   // For perpetuals
        markPrice: string,    // For perpetuals
        bestBid: string,
        bestAsk: string,
        volume24h: string,
        volumeUsd24h: string,
        high24h: string,
        low24h: string,
        priceChange24h: string,
        priceChangePercent24h: string,
        openInterest: string, // For perpetuals
        fundingRate: string,  // For perpetuals
    }]
}
```

### 2.2 Order Book Data

#### `xvm.getOrderBook`
Get order book snapshot.

**Signature:**
```
xvm.getOrderBook({
    market: string,
    depth: int,              // Number of levels (default: 20, max: 100)
}) -> {
    market: string,
    timestamp: int64,
    sequence: int64,         // For detecting gaps in updates
    bids: [{
        price: string,
        size: string,
        orders: int          // Number of orders at this level
    }],
    asks: [{
        price: string,
        size: string,
        orders: int
    }]
}
```

**Example Call:**
```sh
curl -X POST --data '{
    "jsonrpc":"2.0",
    "id": 1,
    "method": "xvm.getOrderBook",
    "params": {
        "market": "BTC-USDT",
        "depth": 20
    }
}' -H 'content-type:application/json;' 127.0.0.1:9650/ext/bc/X
```

#### `xvm.getAggregatedOrderBook`
Get aggregated order book with configurable price grouping.

**Signature:**
```
xvm.getAggregatedOrderBook({
    market: string,
    grouping: string,        // Price grouping: "0.01", "0.1", "1", "10", etc.
}) -> {
    market: string,
    grouping: string,
    timestamp: int64,
    bids: [{price: string, size: string}],
    asks: [{price: string, size: string}]
}
```

### 2.3 Trade Data

#### `xvm.getTrades`
Get recent trades.

**Signature:**
```
xvm.getTrades({
    market: string,
    limit: int,              // Default: 100, max: 1000
    startTime: int64,        // Optional
    endTime: int64,          // Optional
}) -> {
    trades: [{
        tradeId: string,
        market: string,
        price: string,
        size: string,
        side: string,        // "buy" or "sell"
        timestamp: int64,
        isMaker: bool        // Optional: for authenticated users
    }]
}
```

#### `xvm.getCandles`
Get candlestick/OHLCV data.

**Signature:**
```
xvm.getCandles({
    market: string,
    interval: string,        // "1m", "5m", "15m", "1h", "4h", "1d", "1w"
    startTime: int64,
    endTime: int64,
    limit: int,              // Max candles to return
}) -> {
    candles: [{
        timestamp: int64,
        open: string,
        high: string,
        low: string,
        close: string,
        volume: string,
        volumeUsd: string,
        trades: int          // Number of trades
    }]
}
```

### 2.4 Funding Data (Perpetuals)

#### `xvm.getFundingRates`
Get funding rate history.

**Signature:**
```
xvm.getFundingRates({
    market: string,          // Optional: filter by market
    startTime: int64,
    endTime: int64,
    limit: int,
}) -> {
    rates: [{
        market: string,
        fundingRate: string,
        fundingTime: int64,
        nextFundingRate: string,
        nextFundingTime: int64
    }]
}
```

## 3. Account & Risk Management API Extensions

### 3.1 Account Information

#### `xvm.getAccountInfo`
Get comprehensive account information.

**Signature:**
```
xvm.getAccountInfo() -> {
    accountId: string,
    tier: string,            // "basic", "vip1", "vip2", etc.
    makerFeeRate: string,
    takerFeeRate: string,
    totalVolume30d: string,
    // Balances
    balances: [{
        asset: string,
        free: string,        // Available balance
        locked: string,      // In orders
        total: string
    }],
    // Margin info
    marginLevel: string,     // Account health ratio
    totalCollateral: string,
    totalPositionValue: string,
    totalUnrealizedPnl: string,
    availableMargin: string,
    maintenanceMargin: string,
    liquidationPrice: string  // Approximate
}
```

#### `xvm.getAccountActivity`
Get account activity history.

**Signature:**
```
xvm.getAccountActivity({
    type: string,            // "deposit", "withdrawal", "trade", "liquidation", "funding"
    asset: string,           // Optional filter
    startTime: int64,
    endTime: int64,
    limit: int,
    offset: int,
}) -> {
    activities: [{
        type: string,
        asset: string,
        amount: string,
        balance: string,     // Balance after activity
        fee: string,
        txId: string,        // For deposits/withdrawals
        orderId: string,     // For trades
        market: string,      // For trades/funding
        timestamp: int64,
        details: {}          // Type-specific details
    }],
    total: int
}
```

### 3.2 Collateral Management

#### `xvm.transferCollateral`
Transfer assets between spot wallet and trading accounts.

**Signature:**
```
xvm.transferCollateral({
    asset: string,
    amount: string,
    from: string,            // "spot", "perp", "vault:<id>"
    to: string,              // "spot", "perp", "vault:<id>"
}) -> {
    transferId: string,
    status: string,
    timestamp: int64
}
```

#### `xvm.getCollateralInfo`
Get collateral configuration and balances.

**Signature:**
```
xvm.getCollateralInfo() -> {
    collaterals: [{
        asset: string,
        balance: string,
        usdValue: string,
        weight: string,      // Collateral weight (0.8 = 80%)
        isEnabled: bool,
        oraclePrice: string,
        oracleSource: string // "A-Chain" integration
    }],
    totalCollateralUsd: string,
    borrowingPower: string
}
```

### 3.3 Risk Management

#### `xvm.getRiskLimits`
Get account risk limits and current usage.

**Signature:**
```
xvm.getRiskLimits() -> {
    limits: {
        maxLeverage: int,
        maxPositionSize: string,
        maxOpenOrders: int,
        maxDailyVolume: string,
        maxDrawdown: string
    },
    usage: {
        currentLeverage: string,
        largestPosition: string,
        openOrderCount: int,
        dailyVolume: string,
        currentDrawdown: string
    }
}
```

#### `xvm.setRiskParameters`
Set user-defined risk parameters.

**Signature:**
```
xvm.setRiskParameters({
    maxLeverage: int,        // Personal limit <= exchange limit
    stopLossPercent: string, // Auto stop-loss threshold
    dailyLossLimit: string,  // Daily loss limit in USD
    enableAutoDeleverage: bool
}) -> {
    success: bool,
    parameters: {}           // Updated parameters
}
```

### 3.4 Liquidation Information

#### `xvm.getLiquidations`
Get liquidation history.

**Signature:**
```
xvm.getLiquidations({
    market: string,          // Optional
    startTime: int64,
    endTime: int64,
    limit: int,
}) -> {
    liquidations: [{
        liquidationId: string,
        market: string,
        side: string,
        size: string,
        price: string,       // Liquidation price
        markPrice: string,   // Mark price at liquidation
        loss: string,
        fee: string,
        timestamp: int64
    }]
}
```

## 4. Advanced Features API

### 4.1 Vault System (Copy Trading)

#### `xvm.createVault`
Create a new trading vault.

**Signature:**
```
xvm.createVault({
    name: string,
    description: string,
    minDeposit: string,
    maxCapacity: string,
    performanceFee: int,     // Percentage (0-50)
    managementFee: int,      // Annual percentage (0-10)
    lockupPeriod: int,       // Days
    allowedMarkets: []string, // Tradeable markets
    maxLeverage: int,
    isPublic: bool,          // Public or invite-only
}) -> {
    vaultId: string,
    status: string,
    timestamp: int64
}
```

#### `xvm.getVaults`
Get available vaults with filters.

**Signature:**
```
xvm.getVaults({
    status: string,          // "active", "closed"
    sortBy: string,          // "apy", "aum", "subscribers", "age"
    minAum: string,          // Min assets under management
    limit: int,
    offset: int,
}) -> {
    vaults: [{
        vaultId: string,
        name: string,
        manager: string,
        aum: string,
        subscribers: int,
        performance30d: string,
        performanceAllTime: string,
        sharpeRatio: string,
        maxDrawdown: string,
        performanceFee: int,
        managementFee: int,
        minDeposit: string,
        availableCapacity: string,
        createdAt: int64
    }],
    total: int
}
```

#### `xvm.subscribeToVault`
Subscribe to a vault (deposit funds).

**Signature:**
```
xvm.subscribeToVault({
    vaultId: string,
    amount: string,
    asset: string,           // Deposit asset
}) -> {
    subscriptionId: string,
    shares: string,          // Vault shares received
    sharePrice: string,
    timestamp: int64
}
```

#### `xvm.getVaultPerformance`
Get detailed vault performance metrics.

**Signature:**
```
xvm.getVaultPerformance({
    vaultId: string,
    interval: string,        // "1d", "1w", "1m", "3m", "1y", "all"
}) -> {
    vaultId: string,
    metrics: {
        totalReturn: string,
        annualizedReturn: string,
        sharpeRatio: string,
        sortinoRatio: string,
        maxDrawdown: string,
        winRate: string,
        profitFactor: string,
        averageWin: string,
        averageLoss: string
    },
    performanceHistory: [{
        timestamp: int64,
        nav: string,         // Net Asset Value
        return: string,      // Period return
        aum: string
    }]
}
```

### 4.2 Social Trading Features

#### `xvm.getLeaderboard`
Get trader leaderboard.

**Signature:**
```
xvm.getLeaderboard({
    period: string,          // "24h", "7d", "30d", "all"
    sortBy: string,          // "pnl", "roi", "volume", "winRate"
    market: string,          // Optional: filter by market
    limit: int,
}) -> {
    traders: [{
        traderId: string,
        username: string,    // Anonymized
        pnl: string,
        roi: string,         // Return on investment %
        winRate: string,
        totalTrades: int,
        avgLeverage: string,
        followers: int,
        isVaultManager: bool,
        badges: []string     // "topTrader", "consistent", etc.
    }]
}
```

#### `xvm.followTrader`
Follow a trader's activity (not copy trading).

**Signature:**
```
xvm.followTrader({
    traderId: string,
    notifications: {
        onTrade: bool,
        onPnlUpdate: bool,
        dailySummary: bool
    }
}) -> {
    followId: string,
    status: string
}
```

### 4.3 Advanced Order Types

#### `xvm.placeBracketOrder`
Place an order with automatic stop-loss and take-profit.

**Signature:**
```
xvm.placeBracketOrder({
    market: string,
    side: string,
    size: string,
    orderType: string,       // Main order type
    price: string,           // Main order price (if limit)
    stopLoss: {
        triggerPrice: string,
        orderType: string,   // "market" or "limit"
        price: string        // If limit
    },
    takeProfit: {
        triggerPrice: string,
        orderType: string,
        price: string
    }
}) -> {
    mainOrderId: string,
    stopLossOrderId: string,
    takeProfitOrderId: string,
    bracketId: string        // Links orders together
}
```

#### `xvm.placeOCOOrder`
Place One-Cancels-Other order pair.

**Signature:**
```
xvm.placeOCOOrder({
    market: string,
    orders: [{
        side: string,
        orderType: string,
        size: string,
        price: string,
        stopPrice: string    // If stop order
    }]                       // Exactly 2 orders
}) -> {
    ocoId: string,
    orderIds: []string
}
```

### 4.4 Integration with A-Chain Oracle

#### `xvm.getOraclePrices`
Get price feeds from A-Chain oracle.

**Signature:**
```
xvm.getOraclePrices({
    assets: []string,
    source: string,          // "aggregate", "binance", "coinbase", etc.
    includeConfidence: bool
}) -> {
    prices: [{
        asset: string,
        price: string,
        confidence: string,  // Price confidence interval
        sources: []string,   // Contributing sources
        timestamp: int64,
        attestation: string  // TEE attestation proof
    }]
}
```

### 4.5 Z-Chain Private Trading (Future)

#### `xvm.placePrivateOrder`
Place an order with FHE encryption (Z-Chain integration).

**Signature:**
```
xvm.placePrivateOrder({
    encryptedOrder: string,  // FHE-encrypted order details
    publicMetadata: {
        market: string,
        orderType: string,
        timestamp: int64
    },
    zkProof: string          // Zero-knowledge proof of validity
}) -> {
    orderId: string,
    status: string,
    receipt: string          // Encrypted receipt
}
```

## 5. WebSocket API for Real-time Updates

### 5.1 Connection Management

#### WebSocket Endpoint
```
wss://node.lux.network/ext/bc/X/ws
```

#### Authentication
```json
{
    "op": "auth",
    "args": {
        "apiKey": "string",
        "signature": "string",
        "timestamp": "int64"
    }
}
```

### 5.2 Public Channels

#### Order Book Updates
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "orderbook",
    "market": "BTC-USDT",
    "depth": 20,
    "grouping": "0.01"
}

// Update message
{
    "channel": "orderbook",
    "market": "BTC-USDT",
    "type": "snapshot|update",
    "sequence": 12345,
    "timestamp": 1234567890,
    "bids": [["45000.00", "1.5"]],
    "asks": [["45001.00", "2.0"]],
    "checksum": "crc32"
}
```

#### Trade Stream
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "trades",
    "market": "BTC-USDT"
}

// Trade message
{
    "channel": "trades",
    "market": "BTC-USDT",
    "trades": [{
        "tradeId": "123456",
        "price": "45000.50",
        "size": "0.1",
        "side": "buy",
        "timestamp": 1234567890
    }]
}
```

#### Market Ticker
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "ticker",
    "market": "ALL|BTC-USDT"
}

// Ticker update
{
    "channel": "ticker",
    "market": "BTC-USDT",
    "lastPrice": "45000.00",
    "bestBid": "44999.00",
    "bestAsk": "45001.00",
    "volume24h": "1234.56",
    "priceChange24h": "500.00",
    "timestamp": 1234567890
}
```

#### Funding Rates
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "funding",
    "market": "BTC-PERP"
}

// Funding update
{
    "channel": "funding",
    "market": "BTC-PERP",
    "fundingRate": "0.0001",
    "nextFundingTime": 1234567890,
    "timestamp": 1234567890
}
```

### 5.3 Private Channels (Authenticated)

#### Order Updates
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "orders",
    "market": "ALL|BTC-USDT"
}

// Order update
{
    "channel": "orders",
    "order": {
        "orderId": "123456",
        "clientOrderId": "custom-123",
        "market": "BTC-USDT",
        "side": "buy",
        "orderType": "limit",
        "status": "new|partial|filled|cancelled",
        "price": "45000.00",
        "size": "1.0",
        "filled": "0.5",
        "avgFillPrice": "45000.00",
        "timestamp": 1234567890
    }
}
```

#### Position Updates
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "positions"
}

// Position update
{
    "channel": "positions",
    "positions": [{
        "market": "BTC-PERP",
        "side": "long",
        "size": "1.0",
        "entryPrice": "45000.00",
        "markPrice": "45100.00",
        "unrealizedPnl": "100.00",
        "marginRatio": "0.05",
        "liquidationPrice": "40000.00",
        "timestamp": 1234567890
    }]
}
```

#### Account Updates
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "account"
}

// Account update
{
    "channel": "account",
    "balances": [{
        "asset": "USDT",
        "free": "10000.00",
        "locked": "5000.00",
        "total": "15000.00"
    }],
    "marginLevel": "1.5",
    "totalCollateral": "15000.00",
    "availableMargin": "7500.00",
    "timestamp": 1234567890
}
```

#### Liquidation Warnings
```json
// Subscribe
{
    "op": "subscribe",
    "channel": "risk"
}

// Risk alert
{
    "channel": "risk",
    "type": "marginCall|liquidationWarning",
    "market": "BTC-PERP",
    "marginRatio": "0.15",
    "liquidationPrice": "43000.00",
    "timeToLiquidation": 3600,
    "suggestedAction": "addMargin|reducePosition",
    "timestamp": 1234567890
}
```

### 5.4 Control Messages

#### Ping/Pong
```json
// Server ping
{"op": "ping", "timestamp": 1234567890}

// Client pong response
{"op": "pong", "timestamp": 1234567890}
```

#### Error Messages
```json
{
    "op": "error",
    "code": 4001,
    "message": "Invalid market symbol",
    "details": {}
}
```

## 6. Performance Requirements

### Latency Targets
- Order placement: < 10ms
- Order cancellation: < 10ms
- Market data updates: < 5ms
- WebSocket latency: < 2ms

### Throughput Targets
- Orders per second: 200,000+
- Market data updates: 1M+ per second
- Concurrent WebSocket connections: 100,000+
- API requests per second: 50,000+

### Reliability
- 99.99% uptime SLA
- Automatic failover
- Geographic distribution
- DDoS protection

## 7. Implementation Considerations

### Architecture Changes

1. **Order Matching Engine**: High-performance in-memory order book
   - Implement using lock-free data structures
   - Support for multiple order types
   - Price-time priority matching algorithm
   - Continuous double auction mechanism

2. **Risk Engine**: Real-time position and margin calculations
   - Pre-trade risk checks
   - Post-trade margin calculations
   - Cross-margin portfolio management
   - Automatic liquidation system

3. **Market Data Distribution**: Dedicated infrastructure for market data
   - Separate market data feed servers
   - Multicast/broadcast for efficiency
   - Compression for bandwidth optimization
   - Sequence numbers for gap detection

4. **State Management**: Efficient state storage and recovery
   - In-memory state with persistent logging
   - Snapshot and replay capability
   - Hot-standby failover
   - Distributed consensus for state sync

5. **Oracle Integration**: Direct integration with A-Chain price feeds
   - TEE-attested price sources
   - Multiple price aggregation
   - Outlier detection and filtering
   - Fallback mechanisms

6. **Privacy Layer**: Future integration with Z-Chain for private trading
   - FHE for order encryption
   - Zero-knowledge proofs for validity
   - Private order matching
   - Confidential settlement

### Data Models

1. **Order State Machine**
   ```
   States: NEW -> PARTIALLY_FILLED -> FILLED
                -> CANCELLED
                -> REJECTED
                -> EXPIRED
   ```

2. **Position Tracking**
   ```go
   type Position struct {
       Market          string
       Side            PositionSide
       Size            decimal.Decimal
       EntryPrice      decimal.Decimal
       MarkPrice       decimal.Decimal
       Margin          decimal.Decimal
       UnrealizedPnL   decimal.Decimal
       RealizedPnL     decimal.Decimal
       LastUpdateTime  time.Time
   }
   ```

3. **Margin System**
   ```go
   type MarginAccount struct {
       AccountID       string
       Collateral      map[string]decimal.Decimal
       Positions       map[string]*Position
       OpenOrders      map[string]*Order
       MarginLevel     decimal.Decimal
       MaintenanceReq  decimal.Decimal
   }
   ```

4. **Vault Structures**
   ```go
   type Vault struct {
       VaultID         string
       Manager         string
       Subscribers     map[string]*Subscription
       Performance     *PerformanceMetrics
       Configuration   *VaultConfig
       TotalShares     decimal.Decimal
       NAV             decimal.Decimal
   }
   ```

### Security Considerations

1. **Rate Limiting**
   - Per-endpoint limits
   - Per-user limits
   - Adaptive rate limiting based on behavior
   - DDoS protection at network edge

2. **Order Validation**
   - Price sanity checks
   - Size limits
   - Margin requirements
   - Anti-manipulation checks

3. **Market Manipulation Prevention**
   - Wash trading detection
   - Spoofing prevention
   - Maximum order-to-trade ratios
   - Self-trade prevention

4. **TEE Integration**
   - Secure enclave for price feeds
   - Attestation verification
   - Tamper-proof execution
   - Secure key management

5. **FHE Integration**
   - Order encryption at client
   - Homomorphic order matching
   - Private settlement
   - Audit trail without revealing details

## Summary

This comprehensive API extension transforms the X-Chain into a high-performance DEX with:

- **Complete Trading Functionality**: Spot and perpetual futures with advanced order types
- **Professional Market Data**: Real-time order books, trades, and analytics
- **Risk Management**: Comprehensive margin system and liquidation engine
- **Social Trading**: Vault system for copy trading and leaderboards
- **Cross-chain Integration**: 
  - A-Chain for reliable oracle prices with TEE attestation
  - Z-Chain for future private trading via FHE
- **Performance**: 200k+ orders/second with sub-10ms latency

The architecture leverages Lux Network's multi-chain ecosystem to provide unique features not available in traditional DEXs, particularly in oracle reliability and trading privacy.