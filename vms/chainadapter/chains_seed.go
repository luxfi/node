// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.

package chainadapter

// DefaultChainSeed is the canonical chain taxonomy that ships with the
// package. Adding a chain is one row here — no source edits elsewhere.
//
// The numeric IDs are stable wire constants used across adapter signatures
// and on-disk encodings; they are NOT EIP-155 chain IDs (those live in the
// per-row config under ChainConfig.EVMChainID).
//
// Each row may carry a Constructor closure that returns a ChainAdapter; this
// is the sole dispatch seam used by NewExtendedAdapter — no switch, no per-id
// case statement. Rows without an adapter constructor today leave Constructor
// nil; lookup returns nil cleanly for those.
func DefaultChainSeed() []Chain {
	// Group factories. Each helper builds a Constructor closure that resolves
	// its row's ChainConfig at adapter-construction time (lazy), so the seed
	// stays pure-data without baking config literals inline.
	zk := func(name string) func() ChainAdapter {
		return func() ChainAdapter {
			c := chainConfigFor(name)
			return NewZKRollupAdapter(c.ChainID, c.Name, c.EVMChainID, "plonk")
		}
	}
	btcFork := func(name string) func() ChainAdapter {
		return func() ChainAdapter {
			c := chainConfigFor(name)
			return NewBitcoinForkAdapter(c.ChainID, c.Name, c.BlockTime, c.RequiredConfirmations)
		}
	}
	dag := func(name, kind string) func() ChainAdapter {
		return func() ChainAdapter {
			c := chainConfigFor(name)
			return NewDAGAdapter(c.ChainID, c.Name, kind, c.BlockTime)
		}
	}
	cosmosSDK := func(name string) func() ChainAdapter {
		return func() ChainAdapter {
			c := chainConfigFor(name)
			return NewCosmosSDKAdapter(c.ChainID, c.Name, "cosmos", c.BlockTime)
		}
	}
	parachain := func(name string) func() ChainAdapter {
		return func() ChainAdapter {
			c := chainConfigFor(name)
			return NewParachainAdapter(c.ChainID, c.Name, 0, c.BlockTime)
		}
	}
	genericEVM := func(name string) func() ChainAdapter {
		return func() ChainAdapter {
			c := chainConfigFor(name)
			return NewGenericEVMAdapter(c.ChainID, c.Name, c.EVMChainID, c.BlockTime, c.RequiredConfirmations, evmModeFor(c.FinalityMode))
		}
	}

	return []Chain{
		// Major L1s (1-20)
		{ID: 1, Name: "bitcoin", EVM: false, Constructor: func() ChainAdapter { return NewBitcoinAdapter() }},
		{ID: 2, Name: "ethereum", EVM: true, Constructor: func() ChainAdapter { return NewEthereumAdapter() }},
		{ID: 3, Name: "solana", EVM: false, Constructor: func() ChainAdapter { return NewSolanaAdapter() }},
		{ID: 4, Name: "cosmos", EVM: false, Constructor: func() ChainAdapter { return NewCosmosAdapter() }},
		{ID: 5, Name: "polkadot", EVM: false, Constructor: func() ChainAdapter { return NewPolkadotAdapter() }},
		{ID: 6, Name: "polygon", EVM: true, Constructor: func() ChainAdapter { return NewPolygonAdapter() }},
		{ID: 7, Name: "bsc", EVM: true, Constructor: func() ChainAdapter { return NewBSCAdapter() }},
		{ID: 8, Name: "ripple", EVM: false, Constructor: func() ChainAdapter { return NewRippleAdapter() }},
		{ID: 10, Name: "arbitrum", EVM: true, Constructor: func() ChainAdapter { return adapterCtorByName["arbitrum"]() }},
		{ID: 11, Name: "optimism", EVM: true, Constructor: func() ChainAdapter { return adapterCtorByName["optimism"]() }},
		{ID: 12, Name: "base", EVM: true, Constructor: func() ChainAdapter { return adapterCtorByName["base"]() }},
		{ID: 13, Name: "tron", EVM: true, Constructor: func() ChainAdapter { return NewTRONAdapter() }},
		{ID: 14, Name: "cardano", EVM: false, Constructor: func() ChainAdapter { return NewCardanoAdapter() }},
		{ID: 15, Name: "near", EVM: false, Constructor: func() ChainAdapter { return NewNEARAdapter() }},
		{ID: 16, Name: "aptos", EVM: false, Constructor: func() ChainAdapter { return NewAptosAdapter() }},
		{ID: 17, Name: "sui", EVM: false, Constructor: func() ChainAdapter { return NewSuiAdapter() }},
		{ID: 18, Name: "ton", EVM: false, Constructor: func() ChainAdapter { return NewTONAdapter() }},
		{ID: 19, Name: "stellar", EVM: false, Constructor: func() ChainAdapter { return NewStellarAdapter() }},
		{ID: 20, Name: "algorand", EVM: false, Constructor: func() ChainAdapter { return NewAlgorandAdapter() }},

		// EVM L1s (21-40)
		{ID: 21, Name: "fantom", EVM: true, Constructor: genericEVM("fantom")},
		{ID: 22, Name: "cronos", EVM: true, Constructor: genericEVM("cronos")},
		{ID: 23, Name: "gnosis", EVM: true, Constructor: genericEVM("gnosis")},
		{ID: 24, Name: "celo", EVM: true, Constructor: genericEVM("celo")},
		{ID: 25, Name: "moonbeam", EVM: true, Constructor: parachain("moonbeam")},
		{ID: 26, Name: "moonriver", EVM: true, Constructor: parachain("moonriver")},
		{ID: 27, Name: "astar", EVM: true, Constructor: parachain("astar")},
		{ID: 28, Name: "metis", EVM: true, Constructor: genericEVM("metis")},
		{ID: 29, Name: "boba", EVM: true, Constructor: genericEVM("boba")},
		{ID: 30, Name: "aurora", EVM: true, Constructor: genericEVM("aurora")},
		{ID: 31, Name: "klaytn", EVM: true, Constructor: genericEVM("klaytn")},
		{ID: 32, Name: "fuse", EVM: true, Constructor: genericEVM("fuse")},
		{ID: 33, Name: "evmos", EVM: true, Constructor: genericEVM("evmos")},
		{ID: 34, Name: "kava", EVM: true, Constructor: genericEVM("kava")},
		{ID: 35, Name: "okx", EVM: true, Constructor: genericEVM("okx")},
		{ID: 36, Name: "pulse", EVM: true, Constructor: genericEVM("pulse")},
		{ID: 37, Name: "core", EVM: true, Constructor: genericEVM("core")},
		{ID: 38, Name: "flare", EVM: true, Constructor: genericEVM("flare")},
		{ID: 39, Name: "songbird", EVM: true, Constructor: genericEVM("songbird")},
		{ID: 40, Name: "ronin", EVM: true, Constructor: genericEVM("ronin")},

		// EVM L2s and Rollups (41-70)
		{ID: 41, Name: "zksync", EVM: true, Constructor: zk("zksync")},
		{ID: 42, Name: "starknet", EVM: false, Constructor: zk("starknet")},
		{ID: 43, Name: "scroll", EVM: true, Constructor: zk("scroll")},
		{ID: 44, Name: "linea", EVM: true, Constructor: zk("linea")},
		{ID: 45, Name: "mantle", EVM: true, Constructor: genericEVM("mantle")},
		{ID: 46, Name: "zora", EVM: true, Constructor: genericEVM("zora")},
		{ID: 47, Name: "mode", EVM: true, Constructor: genericEVM("mode")},
		{ID: 48, Name: "blast", EVM: true, Constructor: genericEVM("blast")},
		{ID: 49, Name: "manta", EVM: true, Constructor: genericEVM("manta")},
		{ID: 50, Name: "polygon-zkevm", EVM: true, Constructor: zk("polygon-zkevm")},
		{ID: 51, Name: "loopring", EVM: false},
		{ID: 52, Name: "immutable-x", EVM: false},
		{ID: 53, Name: "dydx", EVM: false},
		{ID: 54, Name: "apechain", EVM: true, Constructor: genericEVM("apechain")},
		{ID: 55, Name: "worldchain", EVM: true, Constructor: genericEVM("worldchain")},
		{ID: 56, Name: "taiko", EVM: true, Constructor: zk("taiko")},
		{ID: 57, Name: "frax", EVM: true, Constructor: genericEVM("frax")},
		{ID: 58, Name: "redstone", EVM: true, Constructor: genericEVM("redstone")},
		{ID: 59, Name: "lisk", EVM: true, Constructor: genericEVM("lisk")},
		{ID: 60, Name: "bob", EVM: true, Constructor: genericEVM("bob")},
		{ID: 61, Name: "cyber", EVM: true, Constructor: genericEVM("cyber")},
		{ID: 62, Name: "mint", EVM: true, Constructor: genericEVM("mint")},
		{ID: 63, Name: "kroma", EVM: true, Constructor: zk("kroma")},
		{ID: 64, Name: "opbnb", EVM: true, Constructor: genericEVM("opbnb")},
		{ID: 65, Name: "xlayer", EVM: true, Constructor: zk("xlayer")},
		{ID: 66, Name: "zircuit", EVM: true, Constructor: zk("zircuit")},

		// Cosmos Ecosystem (71-100)
		{ID: 71, Name: "osmosis", EVM: false, Constructor: cosmosSDK("osmosis")},
		{ID: 72, Name: "injective", EVM: false, Constructor: cosmosSDK("injective")},
		{ID: 73, Name: "sei", EVM: true, Constructor: cosmosSDK("sei")},
		{ID: 74, Name: "celestia", EVM: false, Constructor: cosmosSDK("celestia")},
		{ID: 75, Name: "thorchain", EVM: false, Constructor: cosmosSDK("thorchain")},
		{ID: 76, Name: "akash", EVM: false, Constructor: cosmosSDK("akash")},
		{ID: 77, Name: "juno", EVM: false, Constructor: cosmosSDK("juno")},
		{ID: 78, Name: "stargaze", EVM: false, Constructor: cosmosSDK("stargaze")},
		{ID: 79, Name: "secret", EVM: false, Constructor: cosmosSDK("secret")},
		{ID: 80, Name: "axelar", EVM: false, Constructor: cosmosSDK("axelar")},
		{ID: 81, Name: "stride", EVM: false, Constructor: cosmosSDK("stride")},
		{ID: 82, Name: "neutron", EVM: false, Constructor: cosmosSDK("neutron")},
		{ID: 83, Name: "noble", EVM: false, Constructor: cosmosSDK("noble")},
		{ID: 84, Name: "mars", EVM: false},
		{ID: 85, Name: "persistence", EVM: false},
		{ID: 86, Name: "fetch-ai", EVM: false},
		{ID: 87, Name: "band", EVM: false},
		{ID: 88, Name: "regen", EVM: false},
		{ID: 89, Name: "sommelier", EVM: false},
		{ID: 90, Name: "umee", EVM: false},
		{ID: 91, Name: "canto", EVM: false},
		{ID: 92, Name: "dymension", EVM: false, Constructor: cosmosSDK("dymension")},
		{ID: 93, Name: "saga", EVM: false, Constructor: cosmosSDK("saga")},

		// DAG-based and Unique Consensus (101-120)
		{ID: 101, Name: "hedera", EVM: true, Constructor: dag("hedera", "hashgraph")},
		{ID: 102, Name: "iota", EVM: true, Constructor: dag("iota", "tangle")},
		{ID: 103, Name: "kaspa", EVM: false, Constructor: dag("kaspa", "ghostdag")},
		{ID: 104, Name: "filecoin", EVM: true, Constructor: genericEVM("filecoin")},
		{ID: 105, Name: "icp", EVM: false, Constructor: func() ChainAdapter { return NewICPAdapter() }},
		{ID: 106, Name: "flow", EVM: true, Constructor: genericEVM("flow")},
		{ID: 107, Name: "mina", EVM: false},
		{ID: 108, Name: "multiversx", EVM: false},
		{ID: 109, Name: "harmony", EVM: true, Constructor: genericEVM("harmony")},
		{ID: 110, Name: "zilliqa", EVM: true, Constructor: genericEVM("zilliqa")},
		{ID: 111, Name: "vechain", EVM: true, Constructor: genericEVM("vechain")},
		{ID: 112, Name: "theta", EVM: true, Constructor: genericEVM("theta")},
		{ID: 113, Name: "eos", EVM: true, Constructor: genericEVM("eos")},
		{ID: 114, Name: "wax", EVM: false},
		{ID: 115, Name: "tezos", EVM: false, Constructor: func() ChainAdapter { return NewTezosAdapter() }},
		{ID: 116, Name: "neo", EVM: true, Constructor: genericEVM("neo")},
		{ID: 117, Name: "qtum", EVM: false},
		{ID: 118, Name: "waves", EVM: false},
		{ID: 119, Name: "ontology", EVM: false},
		{ID: 120, Name: "ravencoin", EVM: false},

		// Bitcoin Forks and PoW Chains (121-140)
		{ID: 121, Name: "litecoin", EVM: false, Constructor: btcFork("litecoin")},
		{ID: 122, Name: "bitcoin-cash", EVM: false, Constructor: btcFork("bitcoin-cash")},
		{ID: 123, Name: "dogecoin", EVM: false, Constructor: btcFork("dogecoin")},
		{ID: 124, Name: "zcash", EVM: false, Constructor: btcFork("zcash")},
		{ID: 125, Name: "monero", EVM: false, Constructor: func() ChainAdapter { return NewMoneroAdapter() }},
		{ID: 126, Name: "dash", EVM: false, Constructor: btcFork("dash")},
		{ID: 127, Name: "decred", EVM: false, Constructor: btcFork("decred")},
		{ID: 128, Name: "digibyte", EVM: false, Constructor: btcFork("digibyte")},
		{ID: 129, Name: "siacoin", EVM: false},
		{ID: 130, Name: "horizen", EVM: false},
		{ID: 131, Name: "ergo", EVM: false, Constructor: btcFork("ergo")},
		{ID: 132, Name: "firo", EVM: false},
		{ID: 133, Name: "komodo", EVM: false},
		{ID: 134, Name: "pivx", EVM: false},
		{ID: 135, Name: "bsv", EVM: false},
		{ID: 136, Name: "ethereum-classic", EVM: true, Constructor: genericEVM("ethereum-classic")},
		{ID: 137, Name: "flux", EVM: true, Constructor: genericEVM("flux")},
		{ID: 138, Name: "handshake", EVM: false},
		{ID: 139, Name: "nervos", EVM: true, Constructor: genericEVM("nervos")},
		{ID: 140, Name: "conflux", EVM: true, Constructor: genericEVM("conflux")},

		// Polkadot Parachains (141-160)
		{ID: 141, Name: "acala", EVM: false, Constructor: parachain("acala")},
		{ID: 142, Name: "phala", EVM: false, Constructor: parachain("phala")},
		{ID: 143, Name: "bifrost", EVM: false, Constructor: parachain("bifrost")},
		{ID: 144, Name: "parallel", EVM: false},
		{ID: 145, Name: "clover", EVM: false},
		{ID: 146, Name: "centrifuge", EVM: false},
		{ID: 147, Name: "interlay", EVM: false},
		{ID: 148, Name: "hydra", EVM: false},
		{ID: 149, Name: "nodle", EVM: false},
		{ID: 150, Name: "efinity", EVM: false},
		{ID: 151, Name: "mangata", EVM: false},
		{ID: 152, Name: "zeitgeist", EVM: false},
		{ID: 153, Name: "kusama", EVM: false},
		{ID: 154, Name: "polimec", EVM: false},

		// Gaming and NFT Chains (161-180)
		{ID: 161, Name: "wemix", EVM: true, Constructor: genericEVM("wemix")},
		{ID: 162, Name: "oasys", EVM: true, Constructor: genericEVM("oasys")},
		{ID: 163, Name: "beam", EVM: true, Constructor: genericEVM("beam")},
		{ID: 164, Name: "xai", EVM: true, Constructor: genericEVM("xai")},
		{ID: 165, Name: "saakuru", EVM: true, Constructor: genericEVM("saakuru")},
		{ID: 166, Name: "viction", EVM: true, Constructor: genericEVM("viction")},
		{ID: 167, Name: "playdapp", EVM: true, Constructor: genericEVM("playdapp")},
		{ID: 168, Name: "treasure", EVM: true, Constructor: genericEVM("treasure")},
		{ID: 169, Name: "skale", EVM: true, Constructor: genericEVM("skale")},
		{ID: 170, Name: "loom", EVM: true, Constructor: genericEVM("loom")},
		{ID: 171, Name: "enjin", EVM: false},

		// DeFi and Finance Chains (181-200)
		{ID: 181, Name: "unichain", EVM: true, Constructor: genericEVM("unichain")},
		{ID: 182, Name: "swell", EVM: true, Constructor: genericEVM("swell")},
		{ID: 183, Name: "etherfi", EVM: true, Constructor: genericEVM("etherfi")},
		{ID: 184, Name: "ink", EVM: true, Constructor: genericEVM("ink")},
		{ID: 185, Name: "morph", EVM: true, Constructor: genericEVM("morph")},
		{ID: 186, Name: "rari", EVM: true, Constructor: genericEVM("rari")},
		{ID: 187, Name: "shape", EVM: true, Constructor: genericEVM("shape")},
		{ID: 188, Name: "abstract", EVM: true, Constructor: genericEVM("abstract")},
		{ID: 189, Name: "soneium", EVM: true, Constructor: genericEVM("soneium")},
		{ID: 190, Name: "ailayer", EVM: true, Constructor: genericEVM("ailayer")},
		{ID: 191, Name: "hyperliquid", EVM: true, Constructor: genericEVM("hyperliquid")},
		{ID: 192, Name: "berachain", EVM: true, Constructor: genericEVM("berachain")},
		{ID: 193, Name: "monad", EVM: true, Constructor: genericEVM("monad")},
		{ID: 194, Name: "megaeth", EVM: true, Constructor: genericEVM("megaeth")},

		// Lux ecosystem (self-reference, taxonomic id, NOT the EIP-155 chain id)
		{ID: 1000, Name: "lux", EVM: true, Constructor: genericEVM("lux")},
	}
}

// chainConfigFor resolves a chain's full ChainConfig by its canonical name.
// It is a package-level function variable rather than a regular function so
// the static init-cycle detector does not trace closures inside
// DefaultChainSeed back into AllChainConfigs (which references package-level
// idXxx vars whose own initialisation calls into DefaultChainSeed via mustID).
// The variable is assigned in init() below; until then it is nil. By design
// it is never invoked during package-level var initialisation — only later,
// when NewExtendedAdapter calls a row's Constructor closure.
var chainConfigFor func(name string) *ChainConfig

// adapterCtorByName is the same indirection seam for the three EVM-L2
// constructors (Arbitrum, Optimism, Base) whose bodies happen to reference
// package-level idXxx vars. Routing them through a function variable lets the
// seed close over them by name without forming a static init cycle.
var adapterCtorByName map[string]func() ChainAdapter

func init() {
	chainConfigFor = func(name string) *ChainConfig {
		id, ok := DefaultChainTaxonomy().GetByName(name)
		if !ok {
			return &ChainConfig{Name: name}
		}
		if cfg, ok := AllChainConfigs()[id.ID]; ok {
			return cfg
		}
		return &ChainConfig{ChainID: id.ID, Name: name}
	}
	adapterCtorByName = map[string]func() ChainAdapter{
		"arbitrum": func() ChainAdapter { return NewArbitrumAdapter() },
		"optimism": func() ChainAdapter { return NewOptimismAdapter() },
		"base":     func() ChainAdapter { return NewBaseAdapter() },
	}
}

// evmModeFor maps a ChainConfig.FinalityMode string to the VerificationMode
// used by NewGenericEVMAdapter, matching the prior switch in NewExtendedAdapter.
func evmModeFor(finalityMode string) VerificationMode {
	switch finalityMode {
	case "zk":
		return ModeZKProof
	case "optimistic":
		return ModeOptimistic
	default:
		return ModeLightClient
	}
}
