# A-Chain Oracle API Documentation

## Overview
A-Chain serves as the oracle chain for the Lux Network ecosystem, providing secure and reliable price feeds, GPU compute market data, and other oracle services. It leverages Trusted Execution Environment (TEE) technology for attestation and data integrity, ensuring that all oracle data is tamper-proof and verifiable.

## Table of Contents
1. [Core Oracle Services](#1-core-oracle-services)
2. [Price Feed APIs](#2-price-feed-apis)
3. [GPU Compute Market APIs](#3-gpu-compute-market-apis)
4. [TEE Attestation APIs](#4-tee-attestation-apis)
5. [Data Provider APIs](#5-data-provider-apis)
6. [WebSocket Subscriptions](#6-websocket-subscriptions)
7. [Integration Guide](#7-integration-guide)

## 1. Core Oracle Services

### 1.1 Oracle Registration

#### `avm.registerOracle`
Register as a data provider on A-Chain.

**Signature:**
```
avm.registerOracle({
    name: string,
    description: string,
    dataTypes: []string,        // ["price", "compute", "weather", "sports", etc.]
    teeAttestation: string,     // TEE attestation certificate
    endpoint: string,           // Oracle endpoint URL
    minStake: string,           // Minimum stake required
    updateFrequency: int,       // Update frequency in seconds
    metadata: {}                // Additional metadata
}) -> {
    oracleId: string,
    status: string,
    registrationTime: int64
}
```

#### `avm.updateOracleConfig`
Update oracle configuration.

**Signature:**
```
avm.updateOracleConfig({
    oracleId: string,
    endpoint: string,           // Optional: new endpoint
    updateFrequency: int,       // Optional: new frequency
    dataTypes: []string,        // Optional: updated data types
    metadata: {}                // Optional: updated metadata
}) -> {
    success: bool,
    updatedConfig: {}
}
```

### 1.2 Oracle Discovery

#### `avm.getOracles`
Get list of registered oracles with filters.

**Signature:**
```
avm.getOracles({
    dataType: string,           // Filter by data type
    status: string,             // "active", "inactive", "slashed"
    minReputation: int,         // Minimum reputation score
    limit: int,
    offset: int
}) -> {
    oracles: [{
        oracleId: string,
        name: string,
        dataTypes: []string,
        reputation: int,
        uptime: string,         // Percentage
        lastUpdate: int64,
        totalReports: int,
        endpoint: string,
        teeAttested: bool
    }],
    total: int
}
```

#### `avm.getOracleDetails`
Get detailed information about a specific oracle.

**Signature:**
```
avm.getOracleDetails({
    oracleId: string
}) -> {
    oracleId: string,
    name: string,
    description: string,
    dataTypes: []string,
    reputation: int,
    stake: string,
    slashingHistory: [{
        reason: string,
        amount: string,
        timestamp: int64
    }],
    performanceMetrics: {
        uptime: string,
        avgResponseTime: int,
        totalRequests: int,
        successRate: string
    },
    teeInfo: {
        attestation: string,
        validUntil: int64,
        enclaveId: string
    }
}
```

## 2. Price Feed APIs

### 2.1 Price Submission

#### `avm.submitPrice`
Submit price data as an oracle provider.

**Signature:**
```
avm.submitPrice({
    assets: [{
        symbol: string,         // e.g., "BTC", "ETH"
        price: string,
        timestamp: int64,
        source: string,         // "binance", "coinbase", etc.
        volume24h: string,      // Optional: 24h volume
        confidence: int         // 0-100 confidence score
    }],
    signature: string,          // Oracle signature
    teeProof: string           // TEE attestation proof
}) -> {
    submissionId: string,
    accepted: int,              // Number of accepted prices
    rejected: int,              // Number of rejected prices
    timestamp: int64
}
```

#### `avm.submitBulkPrices`
Submit multiple price feeds in batch.

**Signature:**
```
avm.submitBulkPrices({
    priceData: string,          // Compressed price data
    encoding: string,           // "gzip", "zstd"
    checksum: string,           // Data integrity checksum
    signature: string,
    teeProof: string
}) -> {
    batchId: string,
    totalPrices: int,
    processingTime: int,
    timestamp: int64
}
```

### 2.2 Price Retrieval

#### `avm.getPrice`
Get current price for an asset.

**Signature:**
```
avm.getPrice({
    asset: string,
    aggregate: bool,            // true for aggregated price
    sources: []string,          // Optional: specific sources
    includeMetadata: bool
}) -> {
    asset: string,
    price: string,
    timestamp: int64,
    sources: [{
        oracleId: string,
        price: string,
        weight: string,         // Weight in aggregation
        timestamp: int64
    }],
    metadata: {
        method: string,         // "median", "twap", "vwap"
        deviation: string,      // Standard deviation
        confidence: int,
        volume24h: string
    },
    attestation: {
        teeProof: string,
        signature: string,
        validUntil: int64
    }
}
```

#### `avm.getPrices`
Get prices for multiple assets.

**Signature:**
```
avm.getPrices({
    assets: []string,
    aggregate: bool,
    includeHistorical: bool,
    lookback: int               // Historical lookback in seconds
}) -> {
    prices: [{
        asset: string,
        currentPrice: string,
        timestamp: int64,
        historical: [{          // If includeHistorical
            price: string,
            timestamp: int64
        }],
        aggregation: {
            method: string,
            sources: int,
            confidence: int
        }
    }],
    attestation: {
        batchProof: string,
        timestamp: int64
    }
}
```

### 2.3 Price Analytics

#### `avm.getPriceStats`
Get price statistics and analytics.

**Signature:**
```
avm.getPriceStats({
    asset: string,
    period: string,             // "1h", "24h", "7d", "30d"
    metrics: []string           // ["volatility", "correlation", "volume"]
}) -> {
    asset: string,
    period: string,
    stats: {
        high: string,
        low: string,
        open: string,
        close: string,
        avgPrice: string,
        volatility: string,
        volumeWeightedPrice: string,
        priceChangePct: string,
        correlations: [{        // With other assets
            asset: string,
            correlation: string
        }]
    },
    dataPoints: int,
    reliability: string
}
```

## 3. GPU Compute Market APIs

### 3.1 Compute Provider Registration

#### `avm.registerComputeProvider`
Register as a GPU compute provider.

**Signature:**
```
avm.registerComputeProvider({
    name: string,
    gpuInventory: [{
        model: string,          // "NVIDIA A100", "H100", etc.
        quantity: int,
        memory: int,            // GB
        cuda: string,           // CUDA version
        location: string,       // Geographic location
        connectivity: int       // Bandwidth in Gbps
    }],
    pricing: {
        basePrice: string,      // Per hour base price
        spotDiscount: int,      // Spot instance discount %
        reservedDiscount: int,  // Reserved instance discount %
        volumeDiscounts: [{
            hours: int,
            discount: int
        }]
    },
    availability: {
        schedule: string,       // Cron expression
        maintenanceWindow: string
    },
    teeAttestation: string
}) -> {
    providerId: string,
    status: string,
    endpoint: string
}
```

### 3.2 Compute Market Data

#### `avm.getComputePricing`
Get current GPU compute pricing.

**Signature:**
```
avm.getComputePricing({
    gpuModel: string,           // Optional: specific model
    minMemory: int,             // Optional: minimum memory
    location: string,           // Optional: geographic filter
    duration: int               // Hours needed
}) -> {
    pricing: [{
        providerId: string,
        gpuModel: string,
        pricePerHour: string,
        spotPrice: string,
        availability: string,   // "immediate", "1h", "24h"
        location: string,
        reputation: int,
        sla: {
            uptime: string,
            support: string,
            refundPolicy: string
        }
    }],
    marketStats: {
        avgPrice: string,
        minPrice: string,
        maxPrice: string,
        trend: string           // "increasing", "stable", "decreasing"
    }
}
```

#### `avm.getComputeAvailability`
Check GPU availability across providers.

**Signature:**
```
avm.getComputeAvailability({
    requirements: {
        gpuModel: string,
        quantity: int,
        memory: int,
        duration: int,
        startTime: int64
    }
}) -> {
    available: [{
        providerId: string,
        gpuModel: string,
        quantity: int,
        priceEstimate: string,
        availableFrom: int64,
        location: string,
        reservationId: string   // For immediate reservation
    }],
    alternativeOptions: [{      // If exact match not available
        gpuModel: string,
        similarity: int,        // 0-100 similarity score
        performance: string,    // Relative performance
        price: string
    }]
}
```

### 3.3 Compute Usage Reporting

#### `avm.reportComputeUsage`
Report compute usage for billing and analytics.

**Signature:**
```
avm.reportComputeUsage({
    sessionId: string,
    providerId: string,
    usage: {
        gpuHours: string,
        cpuHours: string,
        memoryGBHours: string,
        bandwidthGB: string,
        storageGBHours: string
    },
    performance: {
        avgUtilization: string,
        peakUtilization: string,
        jobsCompleted: int,
        errors: int
    },
    teeMetrics: string          // Encrypted usage metrics
}) -> {
    reportId: string,
    billingAmount: string,
    credits: string,            // Any credits applied
    invoice: string             // Invoice reference
}
```

## 4. TEE Attestation APIs

### 4.1 Attestation Management

#### `avm.generateAttestation`
Generate TEE attestation for data integrity.

**Signature:**
```
avm.generateAttestation({
    data: string,               // Data to attest
    dataType: string,           // "price", "compute", "custom"
    enclaveId: string,
    nonce: string,              // Fresh nonce for replay protection
    expiryTime: int64
}) -> {
    attestation: {
        proof: string,
        signature: string,
        certificate: string,
        enclaveId: string,
        timestamp: int64,
        expiryTime: int64
    },
    metadata: {
        teeType: string,        // "SGX", "SEV", "TDX"
        version: string,
        mrenclave: string,
        mrsigner: string
    }
}
```

#### `avm.verifyAttestation`
Verify TEE attestation proof.

**Signature:**
```
avm.verifyAttestation({
    attestation: string,
    expectedData: string,       // Optional: verify specific data
    allowedEnclaves: []string   // Optional: whitelist enclaves
}) -> {
    valid: bool,
    enclaveId: string,
    dataHash: string,
    timestamp: int64,
    expiryTime: int64,
    details: {
        teeType: string,
        securityLevel: int,
        tcbStatus: string,
        advisories: []string
    }
}
```

### 4.2 Secure Computation

#### `avm.requestSecureComputation`
Request computation within TEE.

**Signature:**
```
avm.requestSecureComputation({
    function: string,           // Function to execute
    inputs: []string,           // Encrypted inputs
    policy: {
        allowedOracles: []string,
        minReputation: int,
        requiredAttestations: int,
        timeout: int
    },
    payment: string             // Payment amount
}) -> {
    computationId: string,
    status: string,
    estimatedTime: int,
    assignedOracles: []string
}
```

## 5. Data Provider APIs

### 5.1 Custom Data Feeds

#### `avm.createDataFeed`
Create a custom data feed.

**Signature:**
```
avm.createDataFeed({
    name: string,
    description: string,
    schema: {
        fields: [{
            name: string,
            type: string,       // "number", "string", "boolean", "object"
            required: bool,
            validation: string  // Validation rules
        }],
        updateFrequency: int,
        aggregationMethod: string
    },
    pricing: {
        subscriptionFee: string,
        perQueryFee: string,
        volumeDiscounts: bool
    },
    access: {
        public: bool,
        whitelist: []string,
        requiresAttestation: bool
    }
}) -> {
    feedId: string,
    endpoint: string,
    status: string
}
```

#### `avm.publishData`
Publish data to a custom feed.

**Signature:**
```
avm.publishData({
    feedId: string,
    data: {},
    timestamp: int64,
    signature: string,
    metadata: {
        source: string,
        methodology: string,
        confidence: int
    },
    teeProof: string
}) -> {
    publicationId: string,
    status: string,
    consumers: int              // Number of consumers notified
}
```

### 5.2 Data Aggregation

#### `avm.createAggregator`
Create a data aggregator for multiple sources.

**Signature:**
```
avm.createAggregator({
    name: string,
    sources: [{
        oracleId: string,
        feedId: string,
        weight: int,            // Weight in aggregation
        minReputation: int
    }],
    aggregationRules: {
        method: string,         // "median", "weighted_avg", "trimmed_mean"
        outlierDetection: bool,
        minSources: int,
        maxDeviation: string,
        updateTrigger: string   // "time", "deviation", "count"
    },
    output: {
        format: string,
        precision: int,
        includeMetadata: bool
    }
}) -> {
    aggregatorId: string,
    status: string,
    endpoint: string
}
```

## 6. WebSocket Subscriptions

### 6.1 Real-time Price Feeds

#### WebSocket Endpoint
```
wss://achain.lux.network/ws
```

#### Price Subscription
```json
{
    "op": "subscribe",
    "channel": "prices",
    "assets": ["BTC", "ETH", "LUX"],
    "aggregated": true,
    "includeProof": true
}
```

#### Price Update Message
```json
{
    "channel": "prices",
    "data": {
        "asset": "BTC",
        "price": "45000.00",
        "previousPrice": "44950.00",
        "timestamp": 1234567890,
        "sources": 5,
        "confidence": 98,
        "proof": {
            "teeAttestation": "...",
            "aggregationProof": "..."
        }
    }
}
```

### 6.2 Compute Market Updates

#### Compute Availability Subscription
```json
{
    "op": "subscribe",
    "channel": "compute",
    "gpuModels": ["A100", "H100"],
    "minQuantity": 4
}
```

## 7. Integration Guide

### 7.1 X-Chain DEX Integration

```solidity
// Example: Using A-Chain price feeds in X-Chain smart contract
interface IAChainOracle {
    function getPrice(string asset) external view returns (uint256 price, uint256 timestamp);
    function getPriceWithProof(string asset) external view returns (
        uint256 price,
        uint256 timestamp,
        bytes proof
    );
}

contract DEXPriceConsumer {
    IAChainOracle public oracle;
    
    function getMarkPrice(string memory asset) public view returns (uint256) {
        (uint256 price, uint256 timestamp) = oracle.getPrice(asset);
        require(block.timestamp - timestamp < 60, "Price too old");
        return price;
    }
}
```

### 7.2 TEE Verification Example

```go
// Example: Verifying TEE attestation in Go
func verifyPriceFeed(attestation []byte, expectedPrice *big.Int) error {
    // Verify TEE attestation
    proof, err := tee.VerifyAttestation(attestation)
    if err != nil {
        return err
    }
    
    // Check enclave measurement
    if !isApprovedEnclave(proof.Mrenclave) {
        return errors.New("untrusted enclave")
    }
    
    // Verify price data
    priceData := proof.GetUserData()
    if !bytes.Equal(priceData.Price, expectedPrice.Bytes()) {
        return errors.New("price mismatch")
    }
    
    return nil
}
```

### 7.3 Aggregated Price Example

```javascript
// Example: Getting aggregated price with multiple oracle sources
async function getSecurePrice(asset) {
    const response = await achain.getPrice({
        asset: asset,
        aggregate: true,
        sources: ["oracle1", "oracle2", "oracle3"],
        includeMetadata: true
    });
    
    // Verify at least 3 sources
    if (response.sources.length < 3) {
        throw new Error("Insufficient oracle sources");
    }
    
    // Check price deviation
    const prices = response.sources.map(s => parseFloat(s.price));
    const avgPrice = prices.reduce((a, b) => a + b) / prices.length;
    const maxDeviation = Math.max(...prices.map(p => Math.abs(p - avgPrice) / avgPrice));
    
    if (maxDeviation > 0.02) { // 2% max deviation
        throw new Error("Price deviation too high");
    }
    
    return {
        price: response.price,
        confidence: response.metadata.confidence,
        attestation: response.attestation
    };
}
```

## Summary

A-Chain provides a comprehensive oracle infrastructure with:

- **Secure Price Feeds**: TEE-attested price data with multiple sources
- **GPU Compute Market**: Real-time compute pricing and availability
- **Custom Data Feeds**: Flexible framework for any oracle data
- **TEE Integration**: Hardware-based security for data integrity
- **Real-time Updates**: WebSocket subscriptions for live data
- **Cross-chain Support**: Easy integration with X-Chain DEX and other chains

The architecture ensures reliable, tamper-proof oracle services essential for DeFi, compute markets, and other blockchain applications.