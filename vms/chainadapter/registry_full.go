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
		idBitcoin: {
			ChainID: idBitcoin, Name: "Bitcoin", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "BTC", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://blockstream.info",
		},
		idEthereum: {
			ChainID: idEthereum, Name: "Ethereum", NetworkID: 1,
			EVMChainID: 1, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://etherscan.io",
		},
		idSolana: {
			ChainID: idSolana, Name: "Solana", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SOL", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 32, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://solscan.io",
		},
		idCosmos: {
			ChainID: idCosmos, Name: "Cosmos Hub", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ATOM", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/cosmos",
		},
		idPolkadot: {
			ChainID: idPolkadot, Name: "Polkadot", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "DOT", NativeDecimals: 10, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://polkadot.subscan.io",
		},
		idPolygon: {
			ChainID: idPolygon, Name: "Polygon", NetworkID: 137,
			EVMChainID: 137, NativeSymbol: "MATIC", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 256, FinalityMode: "checkpoint",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://polygonscan.com",
		},
		idBSC: {
			ChainID: idBSC, Name: "BNB Smart Chain", NetworkID: 56,
			EVMChainID: 56, NativeSymbol: "BNB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 15, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://bscscan.com",
		},
		idRipple: {
			ChainID: idRipple, Name: "XRP Ledger", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "XRP", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 4 * time.Second, TrustThreshold: 0.80,
			ExplorerURL: "https://xrpscan.com",
		},
		idAvalanche: {
			ChainID: idAvalanche, Name: "Avalanche C-Chain", NetworkID: 43114,
			EVMChainID: 43114, NativeSymbol: "AVAX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://snowtrace.io",
		},
		idArbitrum: {
			ChainID: idArbitrum, Name: "Arbitrum One", NetworkID: 42161,
			EVMChainID: 42161, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 250 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://arbiscan.io",
		},
		idOptimism: {
			ChainID: idOptimism, Name: "Optimism", NetworkID: 10,
			EVMChainID: 10, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://optimistic.etherscan.io",
		},
		idBase: {
			ChainID: idBase, Name: "Base", NetworkID: 8453,
			EVMChainID: 8453, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://basescan.org",
		},
		idTron: {
			ChainID: idTron, Name: "TRON", NetworkID: 728126428,
			EVMChainID: 728126428, NativeSymbol: "TRX", NativeDecimals: 6, IsEVM: true,
			RequiredConfirmations: 19, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://tronscan.org",
		},
		idCardano: {
			ChainID: idCardano, Name: "Cardano", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ADA", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 2160, FinalityMode: "probabilistic",
			BlockTime: 20 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://cardanoscan.io",
		},
		idNear: {
			ChainID: idNear, Name: "NEAR", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "NEAR", NativeDecimals: 24, IsEVM: false,
			RequiredConfirmations: 3, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://nearblocks.io",
		},
		idAptos: {
			ChainID: idAptos, Name: "Aptos", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "APT", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://aptoscan.com",
		},
		idSui: {
			ChainID: idSui, Name: "Sui", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SUI", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://suiscan.xyz",
		},
		idTON: {
			ChainID: idTON, Name: "TON", NetworkID: 0xFFFFFFFFFFFFFF11,
			EVMChainID: 0, NativeSymbol: "TON", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://tonscan.org",
		},
		idStellar: {
			ChainID: idStellar, Name: "Stellar", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "XLM", NativeDecimals: 7, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://stellarscan.io",
		},
		idAlgorand: {
			ChainID: idAlgorand, Name: "Algorand", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ALGO", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3300 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://algoexplorer.io",
		},

		// ======== EVM L1s (21-40) ========
		idFantom: {
			ChainID: idFantom, Name: "Fantom", NetworkID: 250,
			EVMChainID: 250, NativeSymbol: "FTM", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://ftmscan.com",
		},
		idCronos: {
			ChainID: idCronos, Name: "Cronos", NetworkID: 25,
			EVMChainID: 25, NativeSymbol: "CRO", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://cronoscan.com",
		},
		idGnosis: {
			ChainID: idGnosis, Name: "Gnosis", NetworkID: 100,
			EVMChainID: 100, NativeSymbol: "xDAI", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://gnosisscan.io",
		},
		idCelo: {
			ChainID: idCelo, Name: "Celo", NetworkID: 42220,
			EVMChainID: 42220, NativeSymbol: "CELO", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://celoscan.io",
		},
		idMoonbeam: {
			ChainID: idMoonbeam, Name: "Moonbeam", NetworkID: 1284,
			EVMChainID: 1284, NativeSymbol: "GLMR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://moonscan.io",
		},
		idMoonriver: {
			ChainID: idMoonriver, Name: "Moonriver", NetworkID: 1285,
			EVMChainID: 1285, NativeSymbol: "MOVR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://moonriver.moonscan.io",
		},
		idAstar: {
			ChainID: idAstar, Name: "Astar", NetworkID: 592,
			EVMChainID: 592, NativeSymbol: "ASTR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://astar.subscan.io",
		},
		idMetis: {
			ChainID: idMetis, Name: "Metis", NetworkID: 1088,
			EVMChainID: 1088, NativeSymbol: "METIS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 4 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://andromeda-explorer.metis.io",
		},
		idBoba: {
			ChainID: idBoba, Name: "Boba Network", NetworkID: 288,
			EVMChainID: 288, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://bobascan.com",
		},
		idAurora: {
			ChainID: idAurora, Name: "Aurora", NetworkID: 1313161554,
			EVMChainID: 1313161554, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.aurora.dev",
		},
		idKlaytn: {
			ChainID: idKlaytn, Name: "Klaytn", NetworkID: 8217,
			EVMChainID: 8217, NativeSymbol: "KLAY", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://klaytnscope.com",
		},
		idFuse: {
			ChainID: idFuse, Name: "Fuse", NetworkID: 122,
			EVMChainID: 122, NativeSymbol: "FUSE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.fuse.io",
		},
		idEvmos: {
			ChainID: idEvmos, Name: "Evmos", NetworkID: 9001,
			EVMChainID: 9001, NativeSymbol: "EVMOS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://escan.live",
		},
		idKava: {
			ChainID: idKava, Name: "Kava", NetworkID: 2222,
			EVMChainID: 2222, NativeSymbol: "KAVA", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://kavascan.com",
		},
		idOKX: {
			ChainID: idOKX, Name: "OKX Chain", NetworkID: 66,
			EVMChainID: 66, NativeSymbol: "OKT", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://www.oklink.com/okc",
		},
		idPulse: {
			ChainID: idPulse, Name: "PulseChain", NetworkID: 369,
			EVMChainID: 369, NativeSymbol: "PLS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 10 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://scan.pulsechain.com",
		},
		idCore: {
			ChainID: idCore, Name: "Core", NetworkID: 1116,
			EVMChainID: 1116, NativeSymbol: "CORE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://scan.coredao.org",
		},
		idFlare: {
			ChainID: idFlare, Name: "Flare", NetworkID: 14,
			EVMChainID: 14, NativeSymbol: "FLR", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://flare-explorer.flare.network",
		},
		idSongbird: {
			ChainID: idSongbird, Name: "Songbird", NetworkID: 19,
			EVMChainID: 19, NativeSymbol: "SGB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://songbird-explorer.flare.network",
		},
		idRON: {
			ChainID: idRON, Name: "Ronin", NetworkID: 2020,
			EVMChainID: 2020, NativeSymbol: "RON", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://app.roninchain.com",
		},

		// ======== EVM L2s and Rollups (41-70) ========
		idZkSync: {
			ChainID: idZkSync, Name: "zkSync Era", NetworkID: 324,
			EVMChainID: 324, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 1 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://explorer.zksync.io",
		},
		idStarknet: {
			ChainID: idStarknet, Name: "Starknet", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 30 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://starkscan.co",
		},
		idScroll: {
			ChainID: idScroll, Name: "Scroll", NetworkID: 534352,
			EVMChainID: 534352, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 3 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://scrollscan.com",
		},
		idLinea: {
			ChainID: idLinea, Name: "Linea", NetworkID: 59144,
			EVMChainID: 59144, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://lineascan.build",
		},
		idMantle: {
			ChainID: idMantle, Name: "Mantle", NetworkID: 5000,
			EVMChainID: 5000, NativeSymbol: "MNT", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mantlescan.xyz",
		},
		idZora: {
			ChainID: idZora, Name: "Zora", NetworkID: 7777777,
			EVMChainID: 7777777, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.zora.energy",
		},
		idMode: {
			ChainID: idMode, Name: "Mode", NetworkID: 34443,
			EVMChainID: 34443, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://modescan.io",
		},
		idBlast: {
			ChainID: idBlast, Name: "Blast", NetworkID: 81457,
			EVMChainID: 81457, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://blastscan.io",
		},
		idManta: {
			ChainID: idManta, Name: "Manta Pacific", NetworkID: 169,
			EVMChainID: 169, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://pacific-explorer.manta.network",
		},
		idPolygonZk: {
			ChainID: idPolygonZk, Name: "Polygon zkEVM", NetworkID: 1101,
			EVMChainID: 1101, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://zkevm.polygonscan.com",
		},
		idLoopring: {
			ChainID: idLoopring, Name: "Loopring", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "LRC", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 1 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://explorer.loopring.io",
		},
		idImmutableX: {
			ChainID: idImmutableX, Name: "Immutable X", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 1 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://immutascan.io",
		},
		iddYdX: {
			ChainID: iddYdX, Name: "dYdX", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "DYDX", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://dydx.exchange",
		},
		idApechain: {
			ChainID: idApechain, Name: "ApeChain", NetworkID: 33139,
			EVMChainID: 33139, NativeSymbol: "APE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://apescan.io",
		},
		idWorldchain: {
			ChainID: idWorldchain, Name: "World Chain", NetworkID: 480,
			EVMChainID: 480, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://worldscan.org",
		},
		idTaiko: {
			ChainID: idTaiko, Name: "Taiko", NetworkID: 167000,
			EVMChainID: 167000, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 12 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://taikoscan.io",
		},
		idFrax: {
			ChainID: idFrax, Name: "Fraxtal", NetworkID: 252,
			EVMChainID: 252, NativeSymbol: "frxETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://fraxscan.com",
		},
		idRedstone: {
			ChainID: idRedstone, Name: "Redstone", NetworkID: 690,
			EVMChainID: 690, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.redstone.xyz",
		},
		idLisk: {
			ChainID: idLisk, Name: "Lisk", NetworkID: 1135,
			EVMChainID: 1135, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://liskscan.com",
		},
		idBob: {
			ChainID: idBob, Name: "BOB", NetworkID: 60808,
			EVMChainID: 60808, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.gobob.xyz",
		},
		idCyber: {
			ChainID: idCyber, Name: "Cyber", NetworkID: 7560,
			EVMChainID: 7560, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://cyberscan.co",
		},
		idMint: {
			ChainID: idMint, Name: "Mint", NetworkID: 185,
			EVMChainID: 185, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.mintchain.io",
		},
		idKroma: {
			ChainID: idKroma, Name: "Kroma", NetworkID: 255,
			EVMChainID: 255, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://kromascan.com",
		},
		idOpBNB: {
			ChainID: idOpBNB, Name: "opBNB", NetworkID: 204,
			EVMChainID: 204, NativeSymbol: "BNB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://opbnb.bscscan.com",
		},
		idXLayer: {
			ChainID: idXLayer, Name: "X Layer", NetworkID: 196,
			EVMChainID: 196, NativeSymbol: "OKB", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://www.oklink.com/xlayer",
		},
		idZircuit: {
			ChainID: idZircuit, Name: "Zircuit", NetworkID: 48900,
			EVMChainID: 48900, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "zk",
			BlockTime: 2 * time.Second, TrustThreshold: 1.0,
			ExplorerURL: "https://explorer.zircuit.com",
		},

		// ======== Cosmos Ecosystem (71-100) ========
		idOsmosis: {
			ChainID: idOsmosis, Name: "Osmosis", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "OSMO", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/osmosis",
		},
		idInjective: {
			ChainID: idInjective, Name: "Injective", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "INJ", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.injective.network",
		},
		idSei: {
			ChainID: idSei, Name: "Sei", NetworkID: 1,
			EVMChainID: 1329, NativeSymbol: "SEI", NativeDecimals: 6, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 400 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://seitrace.com",
		},
		idCelestia: {
			ChainID: idCelestia, Name: "Celestia", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "TIA", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 12 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://celenium.io",
		},
		idThorchain: {
			ChainID: idThorchain, Name: "THORChain", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "RUNE", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://thorchain.net",
		},
		idAkash: {
			ChainID: idAkash, Name: "Akash", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "AKT", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/akash",
		},
		idJuno: {
			ChainID: idJuno, Name: "Juno", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "JUNO", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/juno",
		},
		idStargaze: {
			ChainID: idStargaze, Name: "Stargaze", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "STARS", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/stargaze",
		},
		idSecret: {
			ChainID: idSecret, Name: "Secret Network", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SCRT", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/secret",
		},
		idAxelar: {
			ChainID: idAxelar, Name: "Axelar", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "AXL", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://axelarscan.io",
		},
		idStride: {
			ChainID: idStride, Name: "Stride", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "STRD", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/stride",
		},
		idNeutron: {
			ChainID: idNeutron, Name: "Neutron", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "NTRN", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/neutron",
		},
		idNoble: {
			ChainID: idNoble, Name: "Noble", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "USDC", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://mintscan.io/noble",
		},
		idDymension: {
			ChainID: idDymension, Name: "Dymension", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "DYM", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://dymension.explorers.guru",
		},
		idSaga: {
			ChainID: idSaga, Name: "Saga", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "SAGA", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://saga.explorers.guru",
		},

		// ======== DAG-based and Unique Consensus (101-120) ========
		idHedera: {
			ChainID: idHedera, Name: "Hedera", NetworkID: 295,
			EVMChainID: 295, NativeSymbol: "HBAR", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 3 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://hashscan.io",
		},
		idIOTA: {
			ChainID: idIOTA, Name: "IOTA", NetworkID: 8822,
			EVMChainID: 8822, NativeSymbol: "IOTA", NativeDecimals: 6, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 5 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.iota.org",
		},
		idKaspa: {
			ChainID: idKaspa, Name: "Kaspa", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "KAS", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 10, FinalityMode: "probabilistic",
			BlockTime: 1 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.kaspa.org",
		},
		idFilecoin: {
			ChainID: idFilecoin, Name: "Filecoin", NetworkID: 314,
			EVMChainID: 314, NativeSymbol: "FIL", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 900, FinalityMode: "probabilistic",
			BlockTime: 30 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://filfox.info",
		},
		idICP: {
			ChainID: idICP, Name: "Internet Computer", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ICP", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://dashboard.internetcomputer.org",
		},
		idFlow: {
			ChainID: idFlow, Name: "Flow", NetworkID: 1,
			EVMChainID: 747, NativeSymbol: "FLOW", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://flowscan.org",
		},
		idMina: {
			ChainID: idMina, Name: "Mina", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "MINA", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 15, FinalityMode: "probabilistic",
			BlockTime: 3 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://minascan.io",
		},
		idMultiversX: {
			ChainID: idMultiversX, Name: "MultiversX", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "EGLD", NativeDecimals: 18, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.multiversx.com",
		},
		idHarmony: {
			ChainID: idHarmony, Name: "Harmony", NetworkID: 1666600000,
			EVMChainID: 1666600000, NativeSymbol: "ONE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.harmony.one",
		},
		idZilliqa: {
			ChainID: idZilliqa, Name: "Zilliqa", NetworkID: 32769,
			EVMChainID: 32769, NativeSymbol: "ZIL", NativeDecimals: 12, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 45 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://viewblock.io/zilliqa",
		},
		idVechain: {
			ChainID: idVechain, Name: "VeChain", NetworkID: 100009,
			EVMChainID: 100009, NativeSymbol: "VET", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 12, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://vechainstats.com",
		},
		idTheta: {
			ChainID: idTheta, Name: "Theta", NetworkID: 361,
			EVMChainID: 361, NativeSymbol: "THETA", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 6 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.thetatoken.org",
		},
		idEOS: {
			ChainID: idEOS, Name: "EOS", NetworkID: 17777,
			EVMChainID: 17777, NativeSymbol: "EOS", NativeDecimals: 4, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://bloks.io",
		},
		idWAX: {
			ChainID: idWAX, Name: "WAX", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "WAXP", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://waxblock.io",
		},
		idTezos: {
			ChainID: idTezos, Name: "Tezos", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "XTZ", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 2, FinalityMode: "instant",
			BlockTime: 15 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://tzkt.io",
		},
		idNEO: {
			ChainID: idNEO, Name: "Neo", NetworkID: 47763,
			EVMChainID: 47763, NativeSymbol: "NEO", NativeDecimals: 0, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 15 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://neoscan.io",
		},

		// ======== Bitcoin Forks and PoW Chains (121-140) ========
		idLitecoin: {
			ChainID: idLitecoin, Name: "Litecoin", NetworkID: 2,
			EVMChainID: 0, NativeSymbol: "LTC", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 150 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://blockchair.com/litecoin",
		},
		idBitcoinCash: {
			ChainID: idBitcoinCash, Name: "Bitcoin Cash", NetworkID: 145,
			EVMChainID: 0, NativeSymbol: "BCH", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://blockchair.com/bitcoin-cash",
		},
		idDogecoin: {
			ChainID: idDogecoin, Name: "Dogecoin", NetworkID: 3,
			EVMChainID: 0, NativeSymbol: "DOGE", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 1 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://dogechain.info",
		},
		idZcash: {
			ChainID: idZcash, Name: "Zcash", NetworkID: 133,
			EVMChainID: 0, NativeSymbol: "ZEC", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 24, FinalityMode: "probabilistic",
			BlockTime: 75 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://blockchair.com/zcash",
		},
		idMonero: {
			ChainID: idMonero, Name: "Monero", NetworkID: 128,
			EVMChainID: 0, NativeSymbol: "XMR", NativeDecimals: 12, IsEVM: false,
			RequiredConfirmations: 10, FinalityMode: "probabilistic",
			BlockTime: 2 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://xmrchain.net",
		},
		idDash: {
			ChainID: idDash, Name: "Dash", NetworkID: 5,
			EVMChainID: 0, NativeSymbol: "DASH", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "instant",
			BlockTime: 150 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://insight.dash.org",
		},
		idDecred: {
			ChainID: idDecred, Name: "Decred", NetworkID: 42,
			EVMChainID: 0, NativeSymbol: "DCR", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 5 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://dcrdata.decred.org",
		},
		idDigiByte: {
			ChainID: idDigiByte, Name: "DigiByte", NetworkID: 20,
			EVMChainID: 0, NativeSymbol: "DGB", NativeDecimals: 8, IsEVM: false,
			RequiredConfirmations: 40, FinalityMode: "probabilistic",
			BlockTime: 15 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://digiexplorer.info",
		},
		idErgo: {
			ChainID: idErgo, Name: "Ergo", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "ERG", NativeDecimals: 9, IsEVM: false,
			RequiredConfirmations: 10, FinalityMode: "probabilistic",
			BlockTime: 2 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.ergoplatform.com",
		},
		idEtherClassic: {
			ChainID: idEtherClassic, Name: "Ethereum Classic", NetworkID: 61,
			EVMChainID: 61, NativeSymbol: "ETC", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 40000, FinalityMode: "probabilistic",
			BlockTime: 13 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://etcexplorer.com",
		},
		idFlux: {
			ChainID: idFlux, Name: "Flux", NetworkID: 19167,
			EVMChainID: 19167, NativeSymbol: "FLUX", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 100, FinalityMode: "probabilistic",
			BlockTime: 2 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.runonflux.io",
		},
		idHandshake: {
			ChainID: idHandshake, Name: "Handshake", NetworkID: 1,
			EVMChainID: 0, NativeSymbol: "HNS", NativeDecimals: 6, IsEVM: false,
			RequiredConfirmations: 6, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Minute, TrustThreshold: 0.51,
			ExplorerURL: "https://hnsnetwork.com",
		},
		idNervos: {
			ChainID: idNervos, Name: "Nervos", NetworkID: 71402,
			EVMChainID: 71402, NativeSymbol: "CKB", NativeDecimals: 8, IsEVM: true,
			RequiredConfirmations: 24, FinalityMode: "probabilistic",
			BlockTime: 10 * time.Second, TrustThreshold: 0.51,
			ExplorerURL: "https://explorer.nervos.org",
		},
		idConflux: {
			ChainID: idConflux, Name: "Conflux", NetworkID: 1030,
			EVMChainID: 1030, NativeSymbol: "CFX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 50, FinalityMode: "probabilistic",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.51,
			ExplorerURL: "https://confluxscan.io",
		},

		// ======== Gaming and NFT Chains ========
		idWemix: {
			ChainID: idWemix, Name: "WEMIX", NetworkID: 1111,
			EVMChainID: 1111, NativeSymbol: "WEMIX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://wemixscan.com",
		},
		idOasys: {
			ChainID: idOasys, Name: "Oasys", NetworkID: 248,
			EVMChainID: 248, NativeSymbol: "OAS", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 15 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://scan.oasys.games",
		},
		idBeam: {
			ChainID: idBeam, Name: "Beam", NetworkID: 4337,
			EVMChainID: 4337, NativeSymbol: "BEAM", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://subnets.avax.network/beam",
		},
		idXai: {
			ChainID: idXai, Name: "Xai", NetworkID: 660279,
			EVMChainID: 660279, NativeSymbol: "XAI", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "optimistic",
			BlockTime: 250 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://explorer.xai-chain.net",
		},
		idSkale: {
			ChainID: idSkale, Name: "SKALE", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "sFUEL", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://elated-tan-skat.explorer.mainnet.skalenodes.com",
		},

		// ======== DeFi and Finance Chains ========
		idHyperliquid: {
			ChainID: idHyperliquid, Name: "Hyperliquid", NetworkID: 998,
			EVMChainID: 998, NativeSymbol: "HYPE", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 200 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://hyperliquid.xyz",
		},
		idBerachain: {
			ChainID: idBerachain, Name: "Berachain", NetworkID: 80094,
			EVMChainID: 80094, NativeSymbol: "BERA", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://berascan.io",
		},
		idMonad: {
			ChainID: idMonad, Name: "Monad", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "MON", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 500 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://monad.xyz",
		},
		idMegaETH: {
			ChainID: idMegaETH, Name: "MegaETH", NetworkID: 0,
			EVMChainID: 0, NativeSymbol: "ETH", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 1 * time.Millisecond, TrustThreshold: 0.67,
			ExplorerURL: "https://megaeth.systems",
		},

		// ======== Lux Ecosystem ========
		idLux: {
			ChainID: idLux, Name: "Lux", NetworkID: 96369,
			EVMChainID: 96369, NativeSymbol: "LUX", NativeDecimals: 18, IsEVM: true,
			RequiredConfirmations: 1, FinalityMode: "instant",
			BlockTime: 2 * time.Second, TrustThreshold: 0.67,
			ExplorerURL: "https://explore.lux.network",
		},
	}
}

// NewExtendedAdapter creates an adapter for any supported chain by invoking
// the per-row Constructor closure carried on the Chain seed row. Adding a
// chain with adapter support is a one-row edit to DefaultChainSeed — there is
// no switch and no per-id case statement here. Rows whose Constructor is nil
// have no adapter implementation in this package today; the call returns
// ErrChainNotSupported cleanly.
func NewExtendedAdapter(chainID ChainID) (ChainAdapter, error) {
	chain, ok := DefaultChainTaxonomy().Get(chainID)
	if !ok || chain.Constructor == nil {
		return nil, fmt.Errorf("%w: chain ID %d", ErrChainNotSupported, chainID)
	}
	return chain.Constructor(), nil
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
	case chainID == idBitcoin || chainID == idLitecoin || chainID == idBitcoinCash ||
		chainID == idDogecoin || chainID == idDash || chainID == idZcash ||
		chainID == idDecred || chainID == idDigiByte || chainID == idFiro ||
		chainID == idRavencoin || chainID == idBSV || chainID == idHandshake:
		return ChainTypeUTXO

	// Privacy chains (special UTXO variant)
	case chainID == idMonero || chainID == idHorizen:
		return ChainTypePrivacy

	// Cardano (extended UTXO)
	case chainID == idCardano:
		return ChainTypeCardano

	// Cosmos SDK chains
	case chainID == idCosmos || chainID == idOsmosis || chainID == idInjective ||
		chainID == idSei || chainID == idCelestia || chainID == idThorchain ||
		chainID == idAkash || chainID == idJuno || chainID == idStargaze ||
		chainID == idSecret || chainID == idAxelar || chainID == idStride ||
		chainID == idNeutron || chainID == idNoble || chainID == idMars ||
		chainID == idPersistence || chainID == idFetchAI || chainID == idBand ||
		chainID == idRegen || chainID == idSommelier || chainID == idUmee ||
		chainID == idCanto || chainID == idDymension || chainID == idSaga ||
		chainID == iddYdX:
		return ChainTypeCosmosSDK

	// Substrate/Polkadot parachains
	case chainID == idPolkadot || chainID == idKusama || chainID == idAcala ||
		chainID == idPhala || chainID == idBifrost || chainID == idParallel ||
		chainID == idClover || chainID == idCentrifuge || chainID == idInterlay ||
		chainID == idHydra || chainID == idNodle || chainID == idEfinity ||
		chainID == idMangata || chainID == idZeitgeist || chainID == idPolimec ||
		chainID == idMoonbeam || chainID == idMoonriver || chainID == idAstar:
		return ChainTypeSubstrate

	// DAG-based chains
	case chainID == idHedera || chainID == idIOTA || chainID == idKaspa ||
		chainID == idHarmony || chainID == idMultiversX:
		return ChainTypeDAG

	// Move VM chains
	case chainID == idAptos || chainID == idSui:
		return ChainTypeMoveVM

	// TON
	case chainID == idTON:
		return ChainTypeTVM

	// Stellar
	case chainID == idStellar:
		return ChainTypeStellar

	// Algorand
	case chainID == idAlgorand:
		return ChainTypeAlgorand

	// Tezos
	case chainID == idTezos:
		return ChainTypeTezos

	// Internet Computer
	case chainID == idICP:
		return ChainTypeICP

	// XRP Ledger
	case chainID == idRipple:
		return ChainTypeRipple

	// Filecoin
	case chainID == idFilecoin:
		return ChainTypeFVM

	// Account-based chains (Solana, NEAR, etc.)
	case chainID == idSolana || chainID == idNear || chainID == idFlow ||
		chainID == idMina || chainID == idTron || chainID == idEOS ||
		chainID == idWAX || chainID == idNEO || chainID == idWaves ||
		chainID == idOntology:
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
		if chainID == idBitcoin || chainID == idLitecoin {
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
			idCosmos:    "cosmos",
			idOsmosis:   "osmo",
			idInjective: "inj",
			idSei:       "sei",
			idCelestia:  "celestia",
			idThorchain: "thor",
			idAkash:     "akash",
			idJuno:      "juno",
			idSecret:    "secret",
			idAxelar:    "axelar",
			idStride:    "stride",
			idNeutron:   "neutron",
			idNoble:     "noble",
			idKava:      "kava",
		}
		if prefix, ok := prefixes[chainID]; ok {
			return prefix
		}
		return "cosmos"
	case ChainTypeUTXO:
		if chainID == idBitcoin {
			return "bc1" // Bech32 native segwit
		} else if chainID == idLitecoin {
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
	case chainID <= idAlgorand:
		return CategoryMajorL1

	// EVM L1s
	case chainID >= idFantom && chainID <= idRON:
		return CategoryEVML1

	// L2s and Rollups
	case chainID >= idZkSync && chainID <= idZircuit:
		config := GetEnrichedChainConfig(chainID)
		if config != nil && config.FinalityMode == "zk" {
			return CategoryZKRollup
		}
		return CategoryOptimistic

	// Cosmos ecosystem
	case chainID >= idOsmosis && chainID <= idSaga:
		return CategoryCosmos

	// DAG chains
	case chainID >= idHedera && chainID <= idRavencoin:
		return CategoryDAG

	// Bitcoin forks
	case chainID >= idLitecoin && chainID <= idConflux:
		if chainID == idMonero || chainID == idZcash || chainID == idHorizen {
			return CategoryPrivacy
		}
		return CategoryBitcoinFork

	// Polkadot ecosystem
	case chainID >= idAcala && chainID <= idPolimec:
		return CategoryPolkadot

	// Gaming chains
	case chainID >= idWemix && chainID <= idEnjin:
		return CategoryGaming

	// DeFi chains
	case chainID >= idUnichain && chainID <= idMegaETH:
		return CategoryDeFi

	default:
		return CategoryMajorL1
	}
}
