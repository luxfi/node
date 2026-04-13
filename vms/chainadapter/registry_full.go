// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"fmt"
	"time"
)

// AllChainConfigs returns configurations for all 200+ supported chains
// with proper EVM chain IDs where applicable
func AllChainConfigs() map[ChainID]*ChainConfig {
	return map[ChainID]*ChainConfig{
		// ======== Major L1s ========
		ChainBitcoin: {
			ChainID: ChainBitcoin, Name: "Bitcoin", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "BTC", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://blockstream.info",
		},
		ChainEthereum: {
			ChainID: ChainEthereum, Name: "Ethereum", NetworkID: 1,
			EVMChainID: 1, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://etherscan.io",
		},
		ChainSolana: {
			ChainID: ChainSolana, Name: "Solana", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SOL", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 32, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://solscan.io",
		},
		ChainCosmos: {
			ChainID: ChainCosmos, Name: "Cosmos Hub", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ATOM", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/cosmos",
		},
		ChainPolkadot: {
			ChainID: ChainPolkadot, Name: "Polkadot", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "DOT", NativeDecimals: 10, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://polkadot.subscan.io",
		},
		ChainPolygon: {
			ChainID: ChainPolygon, Name: "Polygon", NetworkID: 137,
			EVMChainID: 137, NativeSymbol: "MATIC", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 256, FinalityMode: "checkpoint",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://polygonscan.com",
		},
		ChainBSC: {
			ChainID: ChainBSC, Name: "BNB Smart Chain", NetworkID: 56,
			EVMChainID: 56, NativeSymbol: "BNB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 15, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://bscscan.com",
		},
		ChainRipple: {
			ChainID: ChainRipple, Name: "XRP Ledger", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "XRP", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 4 * time.Second, TrustThreshold: 0.80,
			ExplorerURL: "https://xrpscan.com",
		},
		ChainAvalanche: {
			ChainID: ChainAvalanche, Name: "Avalanche C-Chain", NetworkID: 43114,
			EVMChainID: 43114, NativeSymbol: "AVAX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://snowtrace.io",
		},
		ChainArbitrum: {
			ChainID: ChainArbitrum, Name: "Arbitrum One", NetworkID: 42161,
			EVMChainID: 42161, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 250 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://arbiscan.io",
		},
		ChainOptimism: {
			ChainID: ChainOptimism, Name: "Optimism", NetworkID: 10,
			EVMChainID: 10, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://optimistic.etherscan.io",
		},
		ChainBase: {
			ChainID: ChainBase, Name: "Base", NetworkID: 8453,
			EVMChainID: 8453, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://basescan.org",
		},
		ChainTron: {
			ChainID: ChainTron, Name: "TRON", NetworkID: 728126428,
			EVMChainID: 728126428, NativeSymbol: "TRX", NativeDecimals: 6, IsEVM: true,
			RequiredConfirmations: 19, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://tronscan.org",
		},
		ChainCardano: {
			ChainID: ChainCardano, Name: "Cardano", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ADA", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 2160, FinalityMode: "probabilistic",
			BlockTime: 20 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://cardanoscan.io",
		},
		ChainNear: {
			ChainID: ChainNear, Name: "NEAR", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "NEAR", NativeDecimals: 24, IsEVM: false,
			RequiredConfirmations: 3, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://nearblocks.io",
		},
		ChainAptos: {
			ChainID: ChainAptos, Name: "Aptos", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "APT", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://aptoscan.com",
		},
		ChainSui: {
			ChainID: ChainSui, Name: "Sui", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SUI", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://suiscan.xyz",
		},
		ChainTON: {
			ChainID: ChainTON, Name: "TON", NetworkID: 0xFFFFFFFFFFFFFF11,
			EVMChainID: 0, NativeSymbol: "TON", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://tonscan.org",
		},
		ChainStellar: {
			ChainID: ChainStellar, Name: "Stellar", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "XLM", NativeDecimals: 7, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://stellarscan.io",
		},
		ChainAlgorand: {
			ChainID: ChainAlgorand, Name: "Algorand", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ALGO", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3300 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://algoexplorer.io",
		},

		// ======== EVM L1s (21-40) ========
		ChainFantom: {
			ChainID: ChainFantom, Name: "Fantom", NetworkID: 250,
			EVMChainID: 250, NativeSymbol: "FTM", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://ftmscan.com",
		},
		ChainCronos: {
			ChainID: ChainCronos, Name: "Cronos", NetworkID: 25,
			EVMChainID: 25, NativeSymbol: "CRO", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://cronoscan.com",
		},
		ChainGnosis: {
			ChainID: ChainGnosis, Name: "Gnosis", NetworkID: 100,
			EVMChainID: 100, NativeSymbol: "xDAI", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://gnosisscan.io",
		},
		ChainCelo: {
			ChainID: ChainCelo, Name: "Celo", NetworkID: 42220,
			EVMChainID: 42220, NativeSymbol: "CELO", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://celoscan.io",
		},
		ChainMoonbeam: {
			ChainID: ChainMoonbeam, Name: "Moonbeam", NetworkID: 1284,
			EVMChainID: 1284, NativeSymbol: "GLMR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://moonscan.io",
		},
		ChainMoonriver: {
			ChainID: ChainMoonriver, Name: "Moonriver", NetworkID: 1285,
			EVMChainID: 1285, NativeSymbol: "MOVR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://moonriver.moonscan.io",
		},
		ChainAstar: {
			ChainID: ChainAstar, Name: "Astar", NetworkID: 592,
			EVMChainID: 592, NativeSymbol: "ASTR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://astar.subscan.io",
		},
		ChainMetis: {
			ChainID: ChainMetis, Name: "Metis", NetworkID: 1088,
			EVMChainID: 1088, NativeSymbol: "METIS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 4 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://andromeda-explorer.metis.io",
		},
		ChainBoba: {
			ChainID: ChainBoba, Name: "Boba Network", NetworkID: 288,
			EVMChainID: 288, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://bobascan.com",
		},
		ChainAurora: {
			ChainID: ChainAurora, Name: "Aurora", NetworkID: 1313161554,
			EVMChainID: 1313161554, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.aurora.dev",
		},
		ChainKlaytn: {
			ChainID: ChainKlaytn, Name: "Klaytn", NetworkID: 8217,
			EVMChainID: 8217, NativeSymbol: "KLAY", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://klaytnscope.com",
		},
		ChainFuse: {
			ChainID: ChainFuse, Name: "Fuse", NetworkID: 122,
			EVMChainID: 122, NativeSymbol: "FUSE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.fuse.io",
		},
		ChainEvmos: {
			ChainID: ChainEvmos, Name: "Evmos", NetworkID: 9001,
			EVMChainID: 9001, NativeSymbol: "EVMOS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://escan.live",
		},
		ChainKava: {
			ChainID: ChainKava, Name: "Kava", NetworkID: 2222,
			EVMChainID: 2222, NativeSymbol: "KAVA", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://kavascan.com",
		},
		ChainOKX: {
			ChainID: ChainOKX, Name: "OKX Chain", NetworkID: 66,
			EVMChainID: 66, NativeSymbol: "OKT", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://www.oklink.com/okc",
		},
		ChainPulse: {
			ChainID: ChainPulse, Name: "PulseChain", NetworkID: 369,
			EVMChainID: 369, NativeSymbol: "PLS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 10 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://scan.pulsechain.com",
		},
		ChainCore: {
			ChainID: ChainCore, Name: "Core", NetworkID: 1116,
			EVMChainID: 1116, NativeSymbol: "CORE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://scan.coredao.org",
		},
		ChainFlare: {
			ChainID: ChainFlare, Name: "Flare", NetworkID: 14,
			EVMChainID: 14, NativeSymbol: "FLR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://flare-explorer.flare.network",
		},
		ChainSongbird: {
			ChainID: ChainSongbird, Name: "Songbird", NetworkID: 19,
			EVMChainID: 19, NativeSymbol: "SGB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://songbird-explorer.flare.network",
		},
		ChainRON: {
			ChainID: ChainRON, Name: "Ronin", NetworkID: 2020,
			EVMChainID: 2020, NativeSymbol: "RON", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://app.roninchain.com",
		},

		// ======== EVM L2s and Rollups (41-70) ========
		ChainZkSync: {
			ChainID: ChainZkSync, Name: "zkSync Era", NetworkID: 324,
			EVMChainID: 324, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 1 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://explorer.zksync.io",
		},
		ChainStarknet: {
			ChainID: ChainStarknet, Name: "Starknet", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 30 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://starkscan.co",
		},
		ChainScroll: {
			ChainID: ChainScroll, Name: "Scroll", NetworkID: 534352,
			EVMChainID: 534352, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 3 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://scrollscan.com",
		},
		ChainLinea: {
			ChainID: ChainLinea, Name: "Linea", NetworkID: 59144,
			EVMChainID: 59144, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://lineascan.build",
		},
		ChainMantle: {
			ChainID: ChainMantle, Name: "Mantle", NetworkID: 5000,
			EVMChainID: 5000, NativeSymbol: "MNT", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mantlescan.xyz",
		},
		ChainZora: {
			ChainID: ChainZora, Name: "Zora", NetworkID: 7777777,
			EVMChainID: 7777777, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.zora.energy",
		},
		ChainMode: {
			ChainID: ChainMode, Name: "Mode", NetworkID: 34443,
			EVMChainID: 34443, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://modescan.io",
		},
		ChainBlast: {
			ChainID: ChainBlast, Name: "Blast", NetworkID: 81457,
			EVMChainID: 81457, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://blastscan.io",
		},
		ChainManta: {
			ChainID: ChainManta, Name: "Manta Pacific", NetworkID: 169,
			EVMChainID: 169, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://pacific-explorer.manta.network",
		},
		ChainPolygonZk: {
			ChainID: ChainPolygonZk, Name: "Polygon zkEVM", NetworkID: 1101,
			EVMChainID: 1101, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://zkevm.polygonscan.com",
		},
		ChainLoopring: {
			ChainID: ChainLoopring, Name: "Loopring", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "LRC", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 1 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://explorer.loopring.io",
		},
		ChainImmutableX: {
			ChainID: ChainImmutableX, Name: "Immutable X", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 1 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://immutascan.io",
		},
		ChaindYdX: {
			ChainID: ChaindYdX, Name: "dYdX", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "DYDX", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://dydx.exchange",
		},
		ChainApechain: {
			ChainID: ChainApechain, Name: "ApeChain", NetworkID: 33139,
			EVMChainID: 33139, NativeSymbol: "APE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://apescan.io",
		},
		ChainWorldchain: {
			ChainID: ChainWorldchain, Name: "World Chain", NetworkID: 480,
			EVMChainID: 480, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://worldscan.org",
		},
		ChainTaiko: {
			ChainID: ChainTaiko, Name: "Taiko", NetworkID: 167000,
			EVMChainID: 167000, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 12 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://taikoscan.io",
		},
		ChainFrax: {
			ChainID: ChainFrax, Name: "Fraxtal", NetworkID: 252,
			EVMChainID: 252, NativeSymbol: "frxETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://fraxscan.com",
		},
		ChainRedstone: {
			ChainID: ChainRedstone, Name: "Redstone", NetworkID: 690,
			EVMChainID: 690, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.redstone.xyz",
		},
		ChainLisk: {
			ChainID: ChainLisk, Name: "Lisk", NetworkID: 1135,
			EVMChainID: 1135, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://liskscan.com",
		},
		ChainBob: {
			ChainID: ChainBob, Name: "BOB", NetworkID: 60808,
			EVMChainID: 60808, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.gobob.xyz",
		},
		ChainCyber: {
			ChainID: ChainCyber, Name: "Cyber", NetworkID: 7560,
			EVMChainID: 7560, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://cyberscan.co",
		},
		ChainMint: {
			ChainID: ChainMint, Name: "Mint", NetworkID: 185,
			EVMChainID: 185, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.mintchain.io",
		},
		ChainKroma: {
			ChainID: ChainKroma, Name: "Kroma", NetworkID: 255,
			EVMChainID: 255, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://kromascan.com",
		},
		ChainOpBNB: {
			ChainID: ChainOpBNB, Name: "opBNB", NetworkID: 204,
			EVMChainID: 204, NativeSymbol: "BNB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://opbnb.bscscan.com",
		},
		ChainXLayer: {
			ChainID: ChainXLayer, Name: "X Layer", NetworkID: 196,
			EVMChainID: 196, NativeSymbol: "OKB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://www.oklink.com/xlayer",
		},
		ChainZircuit: {
			ChainID: ChainZircuit, Name: "Zircuit", NetworkID: 48900,
			EVMChainID: 48900, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://explorer.zircuit.com",
		},

		// ======== Cosmos Ecosystem (71-100) ========
		ChainOsmosis: {
			ChainID: ChainOsmosis, Name: "Osmosis", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "OSMO", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/osmosis",
		},
		ChainInjective: {
			ChainID: ChainInjective, Name: "Injective", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "INJ", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.injective.network",
		},
		ChainSei: {
			ChainID: ChainSei, Name: "Sei", NetworkID: 1,
			EVMChainID: 1329, NativeSymbol: "SEI", NativeDecimals: 6, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://seitrace.com",
		},
		ChainCelestia: {
			ChainID: ChainCelestia, Name: "Celestia", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "TIA", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://celenium.io",
		},
		ChainThorchain: {
			ChainID: ChainThorchain, Name: "THORChain", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "RUNE", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://thorchain.net",
		},
		ChainAkash: {
			ChainID: ChainAkash, Name: "Akash", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "AKT", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/akash",
		},
		ChainJuno: {
			ChainID: ChainJuno, Name: "Juno", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "JUNO", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/juno",
		},
		ChainStargaze: {
			ChainID: ChainStargaze, Name: "Stargaze", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "STARS", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/stargaze",
		},
		ChainSecret: {
			ChainID: ChainSecret, Name: "Secret Network", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SCRT", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/secret",
		},
		ChainAxelar: {
			ChainID: ChainAxelar, Name: "Axelar", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "AXL", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://axelarscan.io",
		},
		ChainStride: {
			ChainID: ChainStride, Name: "Stride", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "STRD", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/stride",
		},
		ChainNeutron: {
			ChainID: ChainNeutron, Name: "Neutron", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "NTRN", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/neutron",
		},
		ChainNoble: {
			ChainID: ChainNoble, Name: "Noble", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "USDC", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/noble",
		},
		ChainDymension: {
			ChainID: ChainDymension, Name: "Dymension", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "DYM", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://dymension.explorers.guru",
		},
		ChainSaga: {
			ChainID: ChainSaga, Name: "Saga", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SAGA", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://saga.explorers.guru",
		},

		// ======== DAG-based and Unique Consensus (101-120) ========
		ChainHedera: {
			ChainID: ChainHedera, Name: "Hedera", NetworkID: 295,
			EVMChainID: 295, NativeSymbol: "HBAR", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://hashscan.io",
		},
		ChainIOTA: {
			ChainID: ChainIOTA, Name: "IOTA", NetworkID: 8822,
			EVMChainID: 8822, NativeSymbol: "IOTA", NativeDecimals: 6, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.iota.org",
		},
		ChainKaspa: {
			ChainID: ChainKaspa, Name: "Kaspa", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "KAS", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 10, FinalityMode: "probabilistic",
			BlockTime: 1 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.kaspa.org",
		},
		ChainFilecoin: {
			ChainID: ChainFilecoin, Name: "Filecoin", NetworkID: 314,
			EVMChainID: 314, NativeSymbol: "FIL", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 900, FinalityMode: "probabilistic",
			BlockTime: 30 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://filfox.info",
		},
		ChainICP: {
			ChainID: ChainICP, Name: "Internet Computer", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ICP", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://dashboard.internetcomputer.org",
		},
		ChainFlow: {
			ChainID: ChainFlow, Name: "Flow", NetworkID: 1,
			EVMChainID: 747, NativeSymbol: "FLOW", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://flowscan.org",
		},
		ChainMina: {
			ChainID: ChainMina, Name: "Mina", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "MINA", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 15, FinalityMode: "probabilistic",
			BlockTime: 3 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://minascan.io",
		},
		ChainMultiversX: {
			ChainID: ChainMultiversX, Name: "MultiversX", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "EGLD", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.multiversx.com",
		},
		ChainHarmony: {
			ChainID: ChainHarmony, Name: "Harmony", NetworkID: 1666600000,
			EVMChainID: 1666600000, NativeSymbol: "ONE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.harmony.one",
		},
		ChainZilliqa: {
			ChainID: ChainZilliqa, Name: "Zilliqa", NetworkID: 32769,
			EVMChainID: 32769, NativeSymbol: "ZIL", NativeDecimals: 12, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 45 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://viewblock.io/zilliqa",
		},
		ChainVechain: {
			ChainID: ChainVechain, Name: "VeChain", NetworkID: 100009,
			EVMChainID: 100009, NativeSymbol: "VET", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 12, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://vechainstats.com",
		},
		ChainTheta: {
			ChainID: ChainTheta, Name: "Theta", NetworkID: 361,
			EVMChainID: 361, NativeSymbol: "THETA", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.thetatoken.org",
		},
		ChainEOS: {
			ChainID: ChainEOS, Name: "EOS", NetworkID: 17777,
			EVMChainID: 17777, NativeSymbol: "EOS", NativeDecimals: 4, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://bloks.io",
		},
		ChainWAX: {
			ChainID: ChainWAX, Name: "WAX", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "WAXP", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://waxblock.io",
		},
		ChainTezos: {
			ChainID: ChainTezos, Name: "Tezos", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "XTZ", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 2, FinalityMode: "instant",
			BlockTime: 15 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://tzkt.io",
		},
		ChainNEO: {
			ChainID: ChainNEO, Name: "Neo", NetworkID: 47763,
			EVMChainID: 47763, NativeSymbol: "NEO", NativeDecimals: 0, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 15 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://neoscan.io",
		},

		// ======== Bitcoin Forks and PoW Chains (121-140) ========
		ChainLitecoin: {
			ChainID: ChainLitecoin, Name: "Litecoin", NetworkID: 2,
			EVMChainID: 0, NativeSymbol: "LTC", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 150 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://blockchair.com/litecoin",
		},
		ChainBitcoinCash: {
			ChainID: ChainBitcoinCash, Name: "Bitcoin Cash", NetworkID: 145,
			EVMChainID: 0, NativeSymbol: "BCH", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://blockchair.com/bitcoin-cash",
		},
		ChainDogecoin: {
			ChainID: ChainDogecoin, Name: "Dogecoin", NetworkID: 3,
			EVMChainID: 0, NativeSymbol: "DOGE", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 1 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://dogechain.info",
		},
		ChainZcash: {
			ChainID: ChainZcash, Name: "Zcash", NetworkID: 133,
			EVMChainID: 0, NativeSymbol: "ZEC", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 24, FinalityMode: "probabilistic",
			BlockTime: 75 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://blockchair.com/zcash",
		},
		ChainMonero: {
			ChainID: ChainMonero, Name: "Monero", NetworkID: 128,
			EVMChainID: 0, NativeSymbol: "XMR", NativeDecimals: 12, IsEVM: false,
			RequiredConfirmations: 10, FinalityMode: "probabilistic",
			BlockTime: 2 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://xmrchain.net",
		},
		ChainDash: {
			ChainID: ChainDash, Name: "Dash", NetworkID: 5,
			EVMChainID: 0, NativeSymbol: "DASH", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "instant",
			BlockTime: 150 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://insight.dash.org",
		},
		ChainDecred: {
			ChainID: ChainDecred, Name: "Decred", NetworkID: 42,
			EVMChainID: 0, NativeSymbol: "DCR", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 5 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://dcrdata.decred.org",
		},
		ChainDigiByte: {
			ChainID: ChainDigiByte, Name: "DigiByte", NetworkID: 20,
			EVMChainID: 0, NativeSymbol: "DGB", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 40, FinalityMode: "probabilistic",
			BlockTime: 15 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://digiexplorer.info",
		},
		ChainErgo: {
			ChainID: ChainErgo, Name: "Ergo", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ERG", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 10, FinalityMode: "probabilistic",
			BlockTime: 2 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.ergoplatform.com",
		},
		ChainEtherClassic: {
			ChainID: ChainEtherClassic, Name: "Ethereum Classic", NetworkID: 61,
			EVMChainID: 61, NativeSymbol: "ETC", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 40000, FinalityMode: "probabilistic",
			BlockTime: 13 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://etcexplorer.com",
		},
		ChainFlux: {
			ChainID: ChainFlux, Name: "Flux", NetworkID: 19167,
			EVMChainID: 19167, NativeSymbol: "FLUX", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 100, FinalityMode: "probabilistic",
			BlockTime: 2 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.runonflux.io",
		},
		ChainHandshake: {
			ChainID: ChainHandshake, Name: "Handshake", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "HNS", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://hnsnetwork.com",
		},
		ChainNervos: {
			ChainID: ChainNervos, Name: "Nervos", NetworkID: 71402,
			EVMChainID: 71402, NativeSymbol: "CKB", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 24, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.nervos.org",
		},
		ChainConflux: {
			ChainID: ChainConflux, Name: "Conflux", NetworkID: 1030,
			EVMChainID: 1030, NativeSymbol: "CFX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 50, FinalityMode: "probabilistic",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.51,
			ExplorerURL: "https://confluxscan.io",
		},

		// ======== Gaming and NFT Chains ========
		ChainWemix: {
			ChainID: ChainWemix, Name: "WEMIX", NetworkID: 1111,
			EVMChainID: 1111, NativeSymbol: "WEMIX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://wemixscan.com",
		},
		ChainOasys: {
			ChainID: ChainOasys, Name: "Oasys", NetworkID: 248,
			EVMChainID: 248, NativeSymbol: "OAS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 15 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://scan.oasys.games",
		},
		ChainBeam: {
			ChainID: ChainBeam, Name: "Beam", NetworkID: 4337,
			EVMChainID: 4337, NativeSymbol: "BEAM", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://subnets.avax.network/beam",
		},
		ChainXai: {
			ChainID: ChainXai, Name: "Xai", NetworkID: 660279,
			EVMChainID: 660279, NativeSymbol: "XAI", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 250 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.xai-chain.net",
		},
		ChainSkale: {
			ChainID: ChainSkale, Name: "SKALE", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "sFUEL", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://elated-tan-skat.explorer.mainnet.skalenodes.com",
		},

		// ======== DeFi and Finance Chains ========
		ChainHyperliquid: {
			ChainID: ChainHyperliquid, Name: "Hyperliquid", NetworkID: 998,
			EVMChainID: 998, NativeSymbol: "HYPE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 200 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://hyperliquid.xyz",
		},
		ChainBerachain: {
			ChainID: ChainBerachain, Name: "Berachain", NetworkID: 80094,
			EVMChainID: 80094, NativeSymbol: "BERA", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://berascan.io",
		},
		ChainMonad: {
			ChainID: ChainMonad, Name: "Monad", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "MON", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://monad.xyz",
		},
		ChainMegaETH: {
			ChainID: ChainMegaETH, Name: "MegaETH", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://megaeth.systems",
		},

		// ======== Lux Ecosystem ========
		ChainLux: {
			ChainID: ChainLux, Name: "Lux", NetworkID: 96369,
			EVMChainID: 96369, NativeSymbol: "LUX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explore.lux.network",
		},
	}
}

// NewExtendedAdapter creates an adapter for any supported chain
func NewExtendedAdapter(chainID ChainID) (ChainAdapter, error) {
	configs := AllChainConfigs()
	config, ok := configs[chainID]
	if !ok {
		return nil, fmt.Errorf("%w: chain ID %d", ErrChainNotSupported, chainID)
	}

	// First check if there's a specialized adapter
	switch chainID {
	case ChainBitcoin:
		return NewBitcoinAdapter(), nil
	case ChainEthereum:
		return NewEthereumAdapter(), nil
	case ChainSolana:
		return NewSolanaAdapter(), nil
	case ChainCosmos:
		return NewCosmosAdapter(), nil
	case ChainPolkadot:
		return NewPolkadotAdapter(), nil
	case ChainPolygon:
		return NewPolygonAdapter(), nil
	case ChainBSC:
		return NewBSCAdapter(), nil
	case ChainRipple:
		return NewRippleAdapter(), nil
	case ChainAvalanche:
		return NewAvalancheAdapter(), nil
	case ChainArbitrum:
		return NewArbitrumAdapter(), nil
	case ChainOptimism:
		return NewOptimismAdapter(), nil
	case ChainBase:
		return NewBaseAdapter(), nil
	case ChainCardano:
		return NewCardanoAdapter(), nil
	case ChainNear:
		return NewNEARAdapter(), nil
	case ChainAptos:
		return NewAptosAdapter(), nil
	case ChainSui:
		return NewSuiAdapter(), nil
	case ChainTON:
		return NewTONAdapter(), nil
	case ChainTron:
		return NewTRONAdapter(), nil
	case ChainStellar:
		return NewStellarAdapter(), nil
	case ChainAlgorand:
		return NewAlgorandAdapter(), nil
	case ChainICP:
		return NewICPAdapter(), nil
	case ChainMonero:
		return NewMoneroAdapter(), nil
	case ChainTezos:
		return NewTezosAdapter(), nil
	}

	// ZK rollups
	switch chainID {
	case ChainZkSync, ChainStarknet, ChainScroll, ChainLinea, ChainPolygonZk, ChainTaiko, ChainKroma, ChainXLayer, ChainZircuit:
		return NewZKRollupAdapter(chainID, config.Name, config.EVMChainID, "plonk"), nil
	}

	// Bitcoin forks (PoW/SPV)
	switch chainID {
	case ChainLitecoin, ChainBitcoinCash, ChainDogecoin, ChainZcash, ChainDash, ChainDecred, ChainDigiByte, ChainErgo:
		return NewBitcoinForkAdapter(chainID, config.Name, config.BlockTime, config.RequiredConfirmations), nil
	}

	// DAG-based chains
	switch chainID {
	case ChainHedera:
		return NewDAGAdapter(chainID, config.Name, "hashgraph", config.BlockTime), nil
	case ChainIOTA:
		return NewDAGAdapter(chainID, config.Name, "tangle", config.BlockTime), nil
	case ChainKaspa:
		return NewDAGAdapter(chainID, config.Name, "ghostdag", config.BlockTime), nil
	}

	// Cosmos SDK chains
	switch chainID {
	case ChainOsmosis, ChainInjective, ChainSei, ChainCelestia, ChainThorchain, ChainAkash, ChainJuno, ChainStargaze, ChainSecret, ChainAxelar, ChainStride, ChainNeutron, ChainNoble, ChainDymension, ChainSaga:
		return NewCosmosSDKAdapter(chainID, config.Name, "cosmos", config.BlockTime), nil
	}

	// Polkadot parachains
	switch chainID {
	case ChainMoonbeam, ChainMoonriver, ChainAstar, ChainAcala, ChainPhala, ChainBifrost:
		return NewParachainAdapter(chainID, config.Name, 0, config.BlockTime), nil
	}

	// Default: Generic EVM adapter for EVM-compatible chains
	if config.IsEVM {
		var mode VerificationMode
		switch config.FinalityMode {
		case "zk":
			mode = ModeZKProof
		case "optimistic":
			mode = ModeOptimistic
		default:
			mode = ModeLightClient
		}
		return NewGenericEVMAdapter(chainID, config.Name, config.EVMChainID, config.BlockTime, config.RequiredConfirmations, mode), nil
	}

	return nil, fmt.Errorf("%w: no adapter for chain ID %d", ErrChainNotSupported, chainID)
}

// GetAllSupportedChains returns all supported chain IDs
func GetAllSupportedChains() []ChainID {
	configs := AllChainConfigs()
	chains := make([]ChainID, 0, len(configs))
	for id := range configs {
		chains = append(chains, id)
	}
	return chains
}

// GetEVMChainID returns the EVM chain ID for a given internal chain ID
func GetEVMChainID(chainID ChainID) (uint64, bool) {
	configs := AllChainConfigs()
	if config, ok := configs[chainID]; ok && config.IsEVM {
		return config.EVMChainID, true
	}
	return 0, false
}

// GetChainByEVMID finds a chain by its EVM chain ID
func GetChainByEVMID(evmChainID uint64) (ChainID, bool) {
	configs := AllChainConfigs()
	for id, config := range configs {
		if config.IsEVM && config.EVMChainID == evmChainID {
			return id, true
		}
	}
	return 0, false
}

// InferChainType determines the ChainType based on chain characteristics
func InferChainType(chainID ChainID) ChainType {
	switch {
	// UTXO chains
	case chainID == ChainBitcoin || chainID == ChainLitecoin || chainID == ChainBitcoinCash ||
		chainID == ChainDogecoin || chainID == ChainDash || chainID == ChainZcash ||
		chainID == ChainDecred || chainID == ChainDigiByte || chainID == ChainFiro ||
		chainID == ChainRavencoin || chainID == ChainBSV || chainID == ChainHandshake:
		return ChainTypeUTXO

	// Privacy chains (special UTXO variant)
	case chainID == ChainMonero || chainID == ChainHorizen:
		return ChainTypePrivacy

	// Cardano (extended UTXO)
	case chainID == ChainCardano:
		return ChainTypeCardano

	// Cosmos SDK chains
	case chainID == ChainCosmos || chainID == ChainOsmosis || chainID == ChainInjective ||
		chainID == ChainSei || chainID == ChainCelestia || chainID == ChainThorchain ||
		chainID == ChainAkash || chainID == ChainJuno || chainID == ChainStargaze ||
		chainID == ChainSecret || chainID == ChainAxelar || chainID == ChainStride ||
		chainID == ChainNeutron || chainID == ChainNoble || chainID == ChainMars ||
		chainID == ChainPersistence || chainID == ChainFetchAI || chainID == ChainBand ||
		chainID == ChainRegen || chainID == ChainSommelier || chainID == ChainUmee ||
		chainID == ChainCanto || chainID == ChainDymension || chainID == ChainSaga ||
		chainID == ChaindYdX:
		return ChainTypeCosmosSDK

	// Substrate/Polkadot parachains
	case chainID == ChainPolkadot || chainID == ChainKusama || chainID == ChainAcala ||
		chainID == ChainPhala || chainID == ChainBifrost || chainID == ChainParallel ||
		chainID == ChainClover || chainID == ChainCentrifuge || chainID == ChainInterlay ||
		chainID == ChainHydra || chainID == ChainNodle || chainID == ChainEfinity ||
		chainID == ChainMangata || chainID == ChainZeitgeist || chainID == ChainPolimec ||
		chainID == ChainMoonbeam || chainID == ChainMoonriver || chainID == ChainAstar:
		return ChainTypeSubstrate

	// DAG-based chains
	case chainID == ChainHedera || chainID == ChainIOTA || chainID == ChainKaspa ||
		chainID == ChainHarmony || chainID == ChainMultiversX:
		return ChainTypeDAG

	// Move VM chains
	case chainID == ChainAptos || chainID == ChainSui:
		return ChainTypeMoveVM

	// TON
	case chainID == ChainTON:
		return ChainTypeTVM

	// Stellar
	case chainID == ChainStellar:
		return ChainTypeStellar

	// Algorand
	case chainID == ChainAlgorand:
		return ChainTypeAlgorand

	// Tezos
	case chainID == ChainTezos:
		return ChainTypeTezos

	// Internet Computer
	case chainID == ChainICP:
		return ChainTypeICP

	// XRP Ledger
	case chainID == ChainRipple:
		return ChainTypeRipple

	// Filecoin
	case chainID == ChainFilecoin:
		return ChainTypeFVM

	// Account-based chains (Solana, NEAR, etc.)
	case chainID == ChainSolana || chainID == ChainNear || chainID == ChainFlow ||
		chainID == ChainMina || chainID == ChainTron || chainID == ChainEOS ||
		chainID == ChainWAX || chainID == ChainNEO || chainID == ChainWaves ||
		chainID == ChainOntology:
		return ChainTypeAccount

	// Default: EVM for anything else (especially if IsEVM is true)
	default:
		return ChainTypeEVM
	}
}

// InferAddressFormat determines the address format for a chain
func InferAddressFormat(chainID ChainID, chainType ChainType) AddressFormat {
	switch chainType {
	case ChainTypeEVM:
		return AddressFormatHex
	case ChainTypeUTXO:
		// Modern Bitcoin uses Bech32, legacy uses Base58
		if chainID == ChainBitcoin || chainID == ChainLitecoin {
			return AddressFormatBech32
		}
		return AddressFormatBase58
	case ChainTypeCosmosSDK:
		return AddressFormatBech32
	case ChainTypeSubstrate:
		return AddressFormatSS58
	case ChainTypeAccount, ChainTypeMoveVM:
		return AddressFormatHex
	case ChainTypeStellar:
		return AddressFormatBase58 // Stellar uses a variant of Base58
	case ChainTypeRipple:
		return AddressFormatBase58
	case ChainTypeCardano:
		return AddressFormatBech32
	default:
		return AddressFormatHex
	}
}

// GetAddressPrefix returns the expected address prefix for a chain
func GetAddressPrefix(chainID ChainID, chainType ChainType) string {
	switch chainType {
	case ChainTypeEVM:
		return "0x"
	case ChainTypeCosmosSDK:
		prefixes := map[ChainID]string{
			ChainCosmos:    "cosmos",
			ChainOsmosis:   "osmo",
			ChainInjective: "inj",
			ChainSei:       "sei",
			ChainCelestia:  "celestia",
			ChainThorchain: "thor",
			ChainAkash:     "akash",
			ChainJuno:      "juno",
			ChainSecret:    "secret",
			ChainAxelar:    "axelar",
			ChainStride:    "stride",
			ChainNeutron:   "neutron",
			ChainNoble:     "noble",
			ChainKava:      "kava",
		}
		if prefix, ok := prefixes[chainID]; ok {
			return prefix
		}
		return "cosmos"
	case ChainTypeUTXO:
		if chainID == ChainBitcoin {
			return "bc1" // Bech32 native segwit
		} else if chainID == ChainLitecoin {
			return "ltc1"
		}
		return "" // Legacy base58 has no prefix
	case ChainTypeCardano:
		return "addr"
	default:
		return ""
	}
}

// GetMPCCurve returns the MPC signing curve for a chain
func GetMPCCurve(chainType ChainType) string {
	switch chainType {
	case ChainTypeEVM, ChainTypeUTXO, ChainTypeCosmosSDK:
		return string(CurveSecp256k1)
	case ChainTypeSubstrate:
		return string(CurveSr25519)
	case ChainTypeAccount, ChainTypeMoveVM, ChainTypeStellar, ChainTypeAlgorand,
		ChainTypeCardano, ChainTypeTezos, ChainTypeTVM, ChainTypeRipple:
		return string(CurveEd25519)
	default:
		return string(CurveSecp256k1)
	}
}

// EnrichChainConfig adds ChainType, AddressFormat, and MPC fields to a config
func EnrichChainConfig(config *ChainConfig) *ChainConfig {
	if config == nil {
		return nil
	}

	// Infer chain type if not set
	if config.ChainType == 0 {
		config.ChainType = InferChainType(config.ChainID)
	}

	// Infer address format if not set
	if config.AddressFormat == 0 {
		config.AddressFormat = InferAddressFormat(config.ChainID, config.ChainType)
	}

	// Set address prefix if not set
	if config.AddressPrefix == "" {
		config.AddressPrefix = GetAddressPrefix(config.ChainID, config.ChainType)
	}

	// Set MPC curve if not set
	if config.MPCCurve == "" {
		config.MPCCurve = GetMPCCurve(config.ChainType)
	}

	// All chains support MPC signing
	config.SupportsMPC = true

	// Determine smart contract support
	switch config.ChainType {
	case ChainTypeEVM, ChainTypeCosmosSDK, ChainTypeMoveVM, ChainTypeTVM,
		ChainTypeTezos, ChainTypeAlgorand, ChainTypeCardano:
		config.SupportsSmartContracts = true
	case ChainTypeUTXO, ChainTypePrivacy:
		config.SupportsSmartContracts = false
	default:
		config.SupportsSmartContracts = config.IsEVM
	}

	return config
}

// GetEnrichedChainConfig returns an enriched chain config with all fields populated
func GetEnrichedChainConfig(chainID ChainID) *ChainConfig {
	configs := AllChainConfigs()
	if config, ok := configs[chainID]; ok {
		return EnrichChainConfig(config)
	}
	return nil
}

// GetAllEnrichedConfigs returns all chain configs with ChainType and MPC fields populated
func GetAllEnrichedConfigs() map[ChainID]*ChainConfig {
	configs := AllChainConfigs()
	enriched := make(map[ChainID]*ChainConfig, len(configs))
	for id, config := range configs {
		enriched[id] = EnrichChainConfig(config)
	}
	return enriched
}

// GetChainsByCategory returns chains grouped by their ChainType
func GetChainsByCategory() map[ChainType][]*ChainConfig {
	configs := GetAllEnrichedConfigs()
	categories := make(map[ChainType][]*ChainConfig)

	for _, config := range configs {
		categories[config.ChainType] = append(categories[config.ChainType], config)
	}

	return categories
}

// GetEVMCompatibleChains returns all EVM-compatible chains (L1s, L2s, rollups)
func GetEVMCompatibleChains() []*ChainConfig {
	configs := GetAllEnrichedConfigs()
	var evmChains []*ChainConfig

	for _, config := range configs {
		if config.IsEVM || config.ChainType == ChainTypeEVM {
			evmChains = append(evmChains, config)
		}
	}

	return evmChains
}

// GetNativePrimaryNetworks returns non-EVM native L1 chains
func GetNativePrimaryNetworks() []*ChainConfig {
	configs := GetAllEnrichedConfigs()
	var nativeChains []*ChainConfig

	for _, config := range configs {
		if !config.IsEVM && config.ChainType != ChainTypeEVM {
			nativeChains = append(nativeChains, config)
		}
	}

	return nativeChains
}

// ChainCategory groups chains by their use case
type ChainCategory string

const (
	CategoryMajorL1     ChainCategory = "major_l1"
	CategoryEVML1       ChainCategory = "evm_l1"
	CategoryOptimistic  ChainCategory = "optimistic_rollup"
	CategoryZKRollup    ChainCategory = "zk_rollup"
	CategoryCosmos      ChainCategory = "cosmos_ecosystem"
	CategoryPolkadot    ChainCategory = "polkadot_ecosystem"
	CategoryBitcoinFork ChainCategory = "bitcoin_fork"
	CategoryDAG         ChainCategory = "dag_chain"
	CategoryGaming      ChainCategory = "gaming"
	CategoryDeFi        ChainCategory = "defi"
	CategoryPrivacy     ChainCategory = "privacy"
)

// GetChainCategory returns the category for a chain
func GetChainCategory(chainID ChainID) ChainCategory {
	switch {
	// Major L1s
	case chainID <= ChainAlgorand:
		return CategoryMajorL1

	// EVM L1s
	case chainID >= ChainFantom && chainID <= ChainRON:
		return CategoryEVML1

	// L2s and Rollups
	case chainID >= ChainZkSync && chainID <= ChainZircuit:
		config := GetEnrichedChainConfig(chainID)
		if config != nil && config.FinalityMode == "zk" {
			return CategoryZKRollup
		}
		return CategoryOptimistic

	// Cosmos ecosystem
	case chainID >= ChainOsmosis && chainID <= ChainSaga:
		return CategoryCosmos

	// DAG chains
	case chainID >= ChainHedera && chainID <= ChainRavencoin:
		return CategoryDAG

	// Bitcoin forks
	case chainID >= ChainLitecoin && chainID <= ChainConflux:
		if chainID == ChainMonero || chainID == ChainZcash || chainID == ChainHorizen {
			return CategoryPrivacy
		}
		return CategoryBitcoinFork

	// Polkadot ecosystem
	case chainID >= ChainAcala && chainID <= ChainPolimec:
		return CategoryPolkadot

	// Gaming chains
	case chainID >= ChainWemix && chainID <= ChainEnjin:
		return CategoryGaming

	// DeFi chains
	case chainID >= ChainUnichain && chainID <= ChainMegaETH:
		return CategoryDeFi

	default:
		return CategoryMajorL1
	}
}
