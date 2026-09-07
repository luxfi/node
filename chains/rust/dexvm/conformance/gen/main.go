// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

// gen emits the D-Chain differential corpus: fixed inputs, and the answers the
// GO REFERENCE gives for them.
//
// It imports the reference package itself — github.com/luxfi/chains/dexvm/registry
// — so every expected value in the corpus is computed by the code the port is a
// port OF, never transcribed by hand. A vector that disagrees is a real
// cross-language divergence, and on the value path a divergence in what
// registers or what an AssetID IS is a fork.
//
// It reaches github.com/luxfi/node for nothing. The only imports are the
// reference and the id type it is written in.
//
// Regenerate with:
//
//	cd chains/rust/dexvm/conformance/gen && go run . > ../dex_differential.json
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/luxfi/chains/dexvm/registry"
	"github.com/luxfi/ids"
)

// ---- the corpus shape ------------------------------------------------------

type assetIDCase struct {
	Name      string `json:"name"`
	NetworkID uint32 `json:"networkID"`
	Chain     string `json:"chain"` // 32-byte hex
	Kind      string `json:"kind"`  // EVM_NATIVE | ERC20 | UTXO | INVALID
	Ref       string `json:"ref"`   // hex, no 0x
	Status    string `json:"status"`
	ID        string `json:"id,omitempty"`   // 32-byte hex when Status == OK
	Code      string `json:"code,omitempty"` // the sentinel when Status == REFUSED
}

type marketIDCase struct {
	Name      string `json:"name"`
	NetworkID uint32 `json:"networkID"`
	Base      string `json:"base"`
	Quote     string `json:"quote"`
	Venue     string `json:"venue"`
	ID        string `json:"id"`
}

type cb58Case struct {
	Hex  string `json:"hex"`
	CB58 string `json:"cb58"`
}

type embeddedCase struct {
	EVMChainID uint64 `json:"evmChainID"`
	Status     string `json:"status"`
	SHA256     string `json:"sha256,omitempty"`
	Network    string `json:"network,omitempty"`
	NetworkID  uint32 `json:"networkID,omitempty"`
	CChainHex  string `json:"cChainHex,omitempty"`
	Assets     int    `json:"assets"`
	Markets    int    `json:"markets"`
}

type labelCase struct {
	S        string `json:"s"`
	Universe bool   `json:"universe"`
	Mock     bool   `json:"mock"`
}

type denyCase struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
	AName  string `json:"assetName"`
	Label  string `json:"chainLabel"`
	Status string `json:"status"`
}

type guardCase struct {
	Name           string `json:"name"`
	ValueEnabled   bool   `json:"valueEnabled"`
	Mode           uint8  `json:"mode"`
	CapsOn         bool   `json:"capsOn"`
	RealAssetsOnly bool   `json:"realAssetsOnly"`
	HaltReady      bool   `json:"haltReady"`
	Status         string `json:"status"`
	StatusString   string `json:"statusString"`
	Code           string `json:"code,omitempty"`
}

// A gate scenario: a chain snapshot, a set of assets to register, a set of
// markets to create, a policy, and what the reference DID about each step.
type seedEntry struct {
	NetworkID uint32 `json:"networkID"`
	Chain     string `json:"chain"`
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	Decimals  uint8  `json:"decimals"`
}

type assetEntry struct {
	NetworkID uint32 `json:"networkID"`
	Chain     string `json:"chain"`
	Kind      string `json:"kind"`
	Ref       string `json:"ref"`
	Decimals  uint8  `json:"decimals"`
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	RiskTier  uint8  `json:"riskTier"`
	// What Register did.
	Status string `json:"status"`
	ID     string `json:"id,omitempty"`
	Code   string `json:"code,omitempty"`
}

type marketEntry struct {
	NetworkID uint32 `json:"networkID"`
	Base      string `json:"base"`
	Quote     string `json:"quote"`
	Venue     string `json:"venue"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"`
	ID        string `json:"id,omitempty"`
	Code      string `json:"code,omitempty"`
}

type policyJSON struct {
	SynthAssets  bool     `json:"allowSyntheticAssets"`
	SynthMarkets bool     `json:"allowSyntheticMarkets"`
	MockLiq      bool     `json:"allowMockLiquidity"`
	Kinds        []string `json:"allowedKinds"`
	// KindBytes carries the RAW policy bytes so a scenario can smuggle a
	// non-real kind, which is the whole point of one of them. It is []int, not
	// []uint8: Go marshals a byte slice as base64, which would hand the other
	// languages a string where they expect numbers.
	KindBytes []int `json:"allowedKindBytes"`
}

type gateCase struct {
	Name    string            `json:"name"`
	Class   uint8             `json:"class"`
	Policy  policyJSON        `json:"policy"`
	Allowed []string          `json:"registryAllowedKinds"`
	Seeds   []seedEntry       `json:"seeds"`
	Assets  []assetEntry      `json:"assets"`
	Markets []marketEntry     `json:"markets"`
	Labels  map[string]string `json:"labels"`
	// The gate's own verdict.
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

type corpus struct {
	Note      string         `json:"note"`
	AssetIDs  []assetIDCase  `json:"assetIds"`
	MarketIDs []marketIDCase `json:"marketIds"`
	CB58      []cb58Case     `json:"cb58"`
	Embedded  []embeddedCase `json:"embedded"`
	Labels    []labelCase    `json:"labels"`
	Deny      []denyCase     `json:"deny"`
	Guards    []guardCase    `json:"guards"`
	Gates     []gateCase     `json:"gates"`
}

// ---- helpers ---------------------------------------------------------------

func hx(b []byte) string { return hex.EncodeToString(b) }

func unhx(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// tid is the generator's deterministic id, byte-identical to the Rust suite's:
// sha256("lux:dexvm:test:" + label). Both sides name the same chain the same way.
func tid(label string) ids.ID {
	return ids.ID(sha256.Sum256([]byte("lux:dexvm:test:" + label)))
}

func kindOf(s string) registry.AssetKind {
	switch s {
	case "EVM_NATIVE":
		return registry.AssetKindEVMNative
	case "ERC20":
		return registry.AssetKindERC20
	case "UTXO":
		return registry.AssetKindUTXO
	default:
		return registry.AssetKindInvalid
	}
}

// codeOf names the sentinel an error carries, so the two languages compare
// error IDENTITY and not prose. "OTHER" is a refusal with no sentinel behind
// it — the corpus records that a refusal happened, not which words it used.
func codeOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, registry.ErrInvalidKind):
		return "InvalidKind"
	case errors.Is(err, registry.ErrBadRef):
		return "BadRef"
	case errors.Is(err, registry.ErrEmptyChainID):
		return "EmptyChainID"
	case errors.Is(err, registry.ErrSameAsset):
		return "SameAsset"
	case errors.Is(err, registry.ErrNetworkMismatch):
		return "NetworkMismatch"
	case errors.Is(err, registry.ErrDuplicateMarket):
		return "DuplicateMarket"
	case errors.Is(err, registry.ErrUnknownAsset):
		return "UnknownAsset"
	case errors.Is(err, registry.ErrAssetDisabled):
		return "AssetDisabled"
	case errors.Is(err, registry.ErrKindNotAllowed):
		return "KindNotAllowed"
	case errors.Is(err, registry.ErrDuplicateAsset):
		return "DuplicateAsset"
	case errors.Is(err, registry.ErrSyntheticOnValueNet):
		return "SyntheticOnValueNet"
	case errors.Is(err, registry.ErrEnabledMarketUnknownAsset):
		return "EnabledMarketUnknownAsset"
	case errors.Is(err, registry.ErrBadAllowedKind):
		return "BadAllowedKind"
	case errors.Is(err, registry.ErrValueModeUnset):
		return "ValueModeUnset"
	case errors.Is(err, registry.ErrValueModeIllegal):
		return "ValueModeIllegal"
	case errors.Is(err, registry.ErrLaunchAssertionsUnmet):
		return "LaunchAssertionsUnmet"
	case errors.Is(err, registry.ErrManifestHashMismatch):
		return "ManifestHashMismatch"
	case errors.Is(err, registry.ErrNoEmbeddedManifest):
		return "NoEmbeddedManifest"
	default:
		return "OTHER"
	}
}

// snapshotChain is the generator's own in-memory chain: it answers "real" only
// for what a scenario seeded, so every refusal in the corpus is a genuine
// refusal by the reference, not a rigged one.
type snapshotChain struct {
	seen map[string]uint8
}

func key(networkID uint32, chain ids.ID, kind string, ref []byte) string {
	return fmt.Sprintf("%d|%s|%s|%s", networkID, chain.Hex(), kind, hx(ref))
}

func (c *snapshotChain) seed(networkID uint32, chain ids.ID, kind string, ref []byte, dec uint8) {
	if c.seen == nil {
		c.seen = map[string]uint8{}
	}
	c.seen[key(networkID, chain, kind, ref)] = dec
}

func (c *snapshotChain) get(networkID uint32, chain ids.ID, kind string, ref []byte) (uint8, error) {
	d, ok := c.seen[key(networkID, chain, kind, ref)]
	if !ok {
		return 0, errors.New("snapshot: no such object on this network/chain")
	}
	return d, nil
}

func (c *snapshotChain) VerifyERC20(n uint32, ch ids.ID, addr []byte) (uint8, error) {
	return c.get(n, ch, "ERC20", addr)
}

func (c *snapshotChain) VerifyEVMNative(n uint32, ch ids.ID) (uint8, error) {
	return c.get(n, ch, "EVM_NATIVE", make([]byte, 20))
}

func (c *snapshotChain) VerifyUTXOAsset(n uint32, ch ids.ID, a ids.ID) (uint8, error) {
	return c.get(n, ch, "UTXO", a[:])
}

func main() {
	out := corpus{
		Note: "Generated by chains/rust/dexvm/conformance/gen from github.com/luxfi/chains/dexvm/registry. " +
			"Every expected value is the GO REFERENCE's own answer. Do not hand-edit: a changed value is a fork, not a fix.",
	}

	chainA := tid("corpus/chain-a")
	chainB := tid("corpus/chain-b")
	allOnes := ids.ID{}
	for i := range allOnes {
		allOnes[i] = 0x11
	}
	erc20A := make([]byte, 20)
	erc20A[19] = 0x01
	utxoA := make([]byte, 32)
	utxoA[31] = 0x07
	addrHigh := unhx("ffeeddccbbaa99887766554433221100aabbccdd")
	utxoHigh := unhx("ffeeddccbbaa99887766554433221100aabbccddeeff00112233445566778899")

	// ---- asset ids, the accepting and the refusing shapes -------------------
	assetVectors := []struct {
		name string
		n    uint32
		ch   ids.ID
		kind string
		ref  []byte
	}{
		{"erc20/golden", 2, allOnes, "ERC20", erc20A},
		{"native/golden", 2, allOnes, "EVM_NATIVE", registry.EVMNativeMarker},
		{"utxo/golden", 2, allOnes, "UTXO", utxoA},
		{"erc20/high-bytes", 1, chainA, "ERC20", addrHigh},
		{"utxo/high-bytes", 1, chainA, "UTXO", utxoHigh},
		{"erc20/other-chain", 1, chainB, "ERC20", addrHigh},
		{"erc20/other-network", 96369, chainA, "ERC20", addrHigh},
		{"native/mainnet", 1, chainA, "EVM_NATIVE", registry.EVMNativeMarker},
		{"native/localnet-id", 1337, chainA, "EVM_NATIVE", registry.EVMNativeMarker},
		// The 20-byte prefix of a UTXO assetID, as an ERC-20: the collision the
		// kind byte must separate.
		{"erc20/utxo-prefix-clash", 1, chainA, "ERC20", utxoHigh[:20]},
		// Refusals.
		{"erc20/short-ref", 1, chainA, "ERC20", addrHigh[:19]},
		{"erc20/long-ref", 1, chainA, "ERC20", append(append([]byte{}, addrHigh...), 0x00)},
		{"erc20/zero-address", 1, chainA, "ERC20", make([]byte, 20)},
		{"native/non-marker", 1, chainA, "EVM_NATIVE", addrHigh},
		{"native/short-marker", 1, chainA, "EVM_NATIVE", make([]byte, 19)},
		{"utxo/short-ref", 1, chainA, "UTXO", utxoHigh[:31]},
		{"utxo/zero-asset", 1, chainA, "UTXO", make([]byte, 32)},
		{"invalid-kind", 1, chainA, "INVALID", addrHigh},
		{"empty-chain", 1, ids.Empty, "ERC20", addrHigh},
		// An empty chain is checked BEFORE the ref shape, so a doubly-bad case
		// pins the ORDER of the two refusals.
		{"empty-chain-and-bad-ref", 1, ids.Empty, "ERC20", addrHigh[:5]},
	}
	for _, v := range assetVectors {
		c := assetIDCase{
			Name: v.name, NetworkID: v.n, Chain: v.ch.Hex(), Kind: v.kind, Ref: hx(v.ref),
		}
		id, err := registry.DeriveAssetID(v.n, v.ch, kindOf(v.kind), v.ref)
		if err != nil {
			c.Status = "REFUSED"
			c.Code = codeOf(err)
		} else {
			c.Status = "OK"
			c.ID = id.Hex()
		}
		out.AssetIDs = append(out.AssetIDs, c)
	}

	// ---- market ids ---------------------------------------------------------
	baseID, _ := registry.DeriveAssetID(1, chainA, registry.AssetKindERC20, addrHigh)
	quoteID, _ := registry.DeriveAssetID(1, chainA, registry.AssetKindUTXO, utxoHigh)
	for _, v := range []struct {
		name  string
		n     uint32
		b, q  ids.ID
		venue []byte
	}{
		{"base/quote/fee30", 1, baseID, quoteID, []byte("tick=1,lot=1,fee=30")},
		{"base/quote/fee5", 1, baseID, quoteID, []byte("tick=1,lot=1,fee=5")},
		{"quote/base/fee30 (direction flipped)", 1, quoteID, baseID, []byte("tick=1,lot=1,fee=30")},
		{"base/quote/no-venue", 1, baseID, quoteID, nil},
		{"base/quote/other-network", 2, baseID, quoteID, []byte("tick=1,lot=1,fee=30")},
		{"empty ids", 1, ids.Empty, ids.Empty, nil},
	} {
		out.MarketIDs = append(out.MarketIDs, marketIDCase{
			Name: v.name, NetworkID: v.n, Base: v.b.Hex(), Quote: v.q.Hex(),
			Venue: hx(v.venue),
			ID:    registry.MarketID(v.n, v.b, v.q, v.venue).Hex(),
		})
	}

	// ---- cb58, the rendering a manifest carries -----------------------------
	cb58Ids := []ids.ID{ids.Empty, allOnes, chainA, chainB, ids.CChainID, ids.XChainID, ids.DChainID}
	var ffs ids.ID
	for i := range ffs {
		ffs[i] = 0xff
	}
	cb58Ids = append(cb58Ids, ffs)
	for _, id := range cb58Ids {
		out.CB58 = append(out.CB58, cb58Case{Hex: id.Hex(), CB58: id.String()})
	}

	// ---- the manifests a node binary carries --------------------------------
	for _, chainID := range []uint64{
		registry.MainnetEVMChainID, registry.TestnetEVMChainID,
		registry.DevnetEVMChainID, registry.LocalnetEVMChainID, 999999,
	} {
		c := embeddedCase{EVMChainID: chainID}
		m, sum, err := registry.EmbeddedManifestFor(chainID)
		if err != nil {
			c.Status = "REFUSED"
			out.Embedded = append(out.Embedded, c)
			continue
		}
		c.Status = "OK"
		c.SHA256 = sum
		c.Network = m.Network
		c.NetworkID = m.NetworkID
		c.CChainHex = m.CChainID.Hex()
		c.Assets = len(m.Assets)
		c.Markets = len(m.Markets)
		out.Embedded = append(out.Embedded, c)
	}

	// ---- the deny predicates ------------------------------------------------
	for _, s := range []string{
		"", "Lux C-Chain", "Lux X-Chain (testnet)", "Liquidity L1 universe", "Liquid EVM",
		"LIQUID DEX", "liquidity", "partner chain", "PARTNER", "mock", "MOCKUSD",
		"mock liquidity token", "synthetic", "Phantom", "placeholder", "testliquidity",
		"d-native", "dnative", "D-NATIVE", "USD Coin", "Wrapped LUX", "Zoo", "Solid",
	} {
		out.Labels = append(out.Labels, labelCase{
			S:        s,
			Universe: registry.IsForbiddenUniverseLabel(s),
			Mock:     registry.IsMockLiquidityRef(s),
		})
	}

	// AssertNoForbiddenAssetRefs is the exported gate over all three predicates,
	// including the ticker shape (whose predicate is unexported), so the corpus
	// pins the ticker rule through the door the gate actually uses.
	for _, v := range []struct{ name, sym, nm, label string }{
		{"clean", "USDC", "USD Coin", "Lux C-Chain"},
		{"pair-symbol/slash", "LUX/USDC", "Lux", "Lux C-Chain"},
		{"pair-symbol/colon", "BTC:USD", "Bitcoin", "Lux C-Chain"},
		{"pair-symbol/underscore", "A_B", "A", "Lux C-Chain"},
		{"pair-symbol/at-uppercase", "LUX-USDC@VENUE", "Lux", "Lux C-Chain"},
		{"pair-symbol/at-lowercase-tail", "LUX-USDC@venue", "Lux", "Lux C-Chain"},
		{"plain-symbol", "LUX", "Lux", "Lux C-Chain"},
		{"mock-symbol", "MOCKUSD", "Mock dollar", "Lux C-Chain"},
		{"mock-name", "USDC", "mock liquidity token", "Lux C-Chain"},
		{"dnative-name", "USDC", "d-native credit", "Lux C-Chain"},
		{"white-label-chain", "USDC", "USD Coin", "Liquidity primary network"},
		{"white-label-and-clean-symbol", "LUX", "Lux", "Liquid EVM"},
		{"no-label", "USDC", "USD Coin", ""},
	} {
		a := registry.Asset{Symbol: v.sym, Name: v.nm}
		st := "OK"
		if err := registry.AssertNoForbiddenAssetRefs(a, v.label); err != nil {
			st = "REFUSED"
		}
		out.Deny = append(out.Deny, denyCase{
			Name: v.name, Symbol: v.sym, AName: v.nm, Label: v.label, Status: st,
		})
	}

	// ---- the consensus-mode value guard -------------------------------------
	for _, v := range []struct {
		name             string
		enabled          bool
		mode             registry.ConsensusMode
		caps, real, halt bool
	}{
		{"disabled/unset", false, registry.ConsensusModeUnset, false, false, false},
		{"disabled/illegal-mode", false, registry.ConsensusMode(99), false, false, false},
		{"quorum/no-bundle", true, registry.ConsensusModeQuorumFinality, false, false, false},
		{"quorum/full-bundle", true, registry.ConsensusModeQuorumFinality, true, true, true},
		{"labeled/full-bundle", true, registry.ConsensusModeHonestValidatorLabeled, true, true, true},
		{"labeled/no-caps", true, registry.ConsensusModeHonestValidatorLabeled, false, true, true},
		{"labeled/no-real-assets", true, registry.ConsensusModeHonestValidatorLabeled, true, false, true},
		{"labeled/no-halt", true, registry.ConsensusModeHonestValidatorLabeled, true, true, false},
		{"labeled/nothing", true, registry.ConsensusModeHonestValidatorLabeled, false, false, false},
		{"unset/value", true, registry.ConsensusModeUnset, true, true, true},
		{"illegal-99/value", true, registry.ConsensusMode(99), true, true, true},
		{"illegal-3/value", true, registry.ConsensusMode(3), true, true, true},
		{"illegal-255/value", true, registry.ConsensusMode(255), true, true, true},
	} {
		g := guardCase{
			Name: v.name, ValueEnabled: v.enabled, Mode: uint8(v.mode),
			CapsOn: v.caps, RealAssetsOnly: v.real, HaltReady: v.halt,
		}
		st, err := registry.GuardValueActivation(v.enabled, v.mode,
			registry.LaunchAssertions{CapsOn: v.caps, RealAssetsOnly: v.real, HaltReady: v.halt})
		if err != nil {
			g.Status = "REFUSED"
			g.Code = codeOf(err)
		} else {
			g.Status = "OK"
			g.StatusString = st.Status
		}
		out.Guards = append(out.Guards, g)
	}

	// ---- whole-registry scenarios: register, create, gate --------------------
	out.Gates = append(out.Gates, gateScenarios(chainA, chainB)...)

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(append(b, '\n')); err != nil {
		panic(err)
	}
}

// scenario is the generator's description of one whole-registry run.
type scenario struct {
	name    string
	class   registry.NetworkClass
	allowed []registry.AssetKind
	policy  registry.DexAssetPolicy
	seeds   []seedEntry
	assets  []registry.Asset
	markets []registry.Market
	labels  map[string]string // chain hex -> label
}

func gateScenarios(chainA, chainB ids.ID) []gateCase {
	const netMain uint32 = 1
	usdc := unhx("1111111111111111111111111111111111111111")
	wlux := unhx("2222222222222222222222222222222222222222")
	ghost := unhx("3333333333333333333333333333333333333333")
	xasset := tid("corpus/x-asset")

	real := func(ref []byte, sym, name string, dec uint8, enabled bool) registry.Asset {
		return registry.Asset{
			NetworkID: netMain, ChainID: chainA, Kind: registry.AssetKindERC20,
			CanonicalRef: ref, Decimals: dec, Symbol: sym, Name: name,
			Enabled: enabled, RiskTier: registry.RiskTier1,
		}
	}
	seedERC20 := func(ref []byte, dec uint8) seedEntry {
		return seedEntry{NetworkID: netMain, Chain: chainA.Hex(), Kind: "ERC20", Ref: hx(ref), Decimals: dec}
	}
	all3 := []registry.AssetKind{registry.AssetKindEVMNative, registry.AssetKindERC20, registry.AssetKindUTXO}

	usdcID, _ := registry.DeriveAssetID(netMain, chainA, registry.AssetKindERC20, usdc)
	wluxID, _ := registry.DeriveAssetID(netMain, chainA, registry.AssetKindERC20, wlux)
	ghostID, _ := registry.DeriveAssetID(netMain, chainA, registry.AssetKindERC20, ghost)

	scenarios := []scenario{
		{
			name: "all-real/mainnet/passes", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6), seedERC20(wlux, 18)},
			assets: []registry.Asset{
				real(usdc, "USDC", "USD Coin", 6, true),
				real(wlux, "WLUX", "Wrapped LUX", 18, true),
			},
			markets: []registry.Market{{
				NetworkID: netMain, BaseAssetID: wluxID, QuoteAssetID: usdcID,
				VenueConfig: []byte("tick=1;lot=1;fee=30"), Enabled: true,
			}},
			labels: map[string]string{chainA.Hex(): "Lux C-Chain"},
		},
		{
			name: "unseeded-asset/refused-at-register", class: registry.NetworkClassMainnet, allowed: all3,
			policy:  registry.DefaultDexAssetPolicy(),
			seeds:   []seedEntry{seedERC20(usdc, 6)},
			assets:  []registry.Asset{real(usdc, "USDC", "USD Coin", 6, true), real(ghost, "GHOST", "Ghost", 18, true)},
			markets: nil,
			labels:  map[string]string{chainA.Hex(): "Lux C-Chain"},
		},
		{
			name: "decimals-mismatch/refused-at-register", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{real(usdc, "USDC", "USD Coin", 18, true)},
		},
		{
			name: "kind-not-allowed/refused-at-register", class: registry.NetworkClassMainnet,
			allowed: []registry.AssetKind{registry.AssetKindUTXO},
			policy:  registry.DefaultDexAssetPolicy(),
			seeds:   []seedEntry{seedERC20(usdc, 6)},
			assets:  []registry.Asset{real(usdc, "USDC", "USD Coin", 6, true)},
		},
		{
			name: "duplicate-asset/refused", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{
				real(usdc, "USDC", "USD Coin", 6, true),
				real(usdc, "USDC", "USD Coin", 6, true),
			},
		},
		{
			name: "market-over-unregistered-base/refused", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{real(usdc, "USDC", "USD Coin", 6, true)},
			markets: []registry.Market{{
				NetworkID: netMain, BaseAssetID: ghostID, QuoteAssetID: usdcID, Enabled: true,
			}},
		},
		{
			name: "market-over-disabled-asset/refused", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6), seedERC20(wlux, 18)},
			assets: []registry.Asset{
				real(usdc, "USDC", "USD Coin", 6, true),
				real(wlux, "WLUX", "Wrapped LUX", 18, false), // disabled
			},
			markets: []registry.Market{{
				NetworkID: netMain, BaseAssetID: wluxID, QuoteAssetID: usdcID, Enabled: true,
			}},
		},
		{
			name: "self-pair-market/refused", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{real(usdc, "USDC", "USD Coin", 6, true)},
			markets: []registry.Market{{
				NetworkID: netMain, BaseAssetID: usdcID, QuoteAssetID: usdcID, Enabled: true,
			}},
		},
		{
			name: "market-network-mismatch/refused", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6), seedERC20(wlux, 18)},
			assets: []registry.Asset{
				real(usdc, "USDC", "USD Coin", 6, true),
				real(wlux, "WLUX", "Wrapped LUX", 18, true),
			},
			markets: []registry.Market{{
				NetworkID: 2, BaseAssetID: wluxID, QuoteAssetID: usdcID, Enabled: true,
			}},
		},
		{
			name: "duplicate-market/refused", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6), seedERC20(wlux, 18)},
			assets: []registry.Asset{
				real(usdc, "USDC", "USD Coin", 6, true),
				real(wlux, "WLUX", "Wrapped LUX", 18, true),
			},
			markets: []registry.Market{
				{NetworkID: netMain, BaseAssetID: wluxID, QuoteAssetID: usdcID, VenueConfig: []byte("v"), Enabled: true},
				{NetworkID: netMain, BaseAssetID: wluxID, QuoteAssetID: usdcID, VenueConfig: []byte("v"), Enabled: true},
			},
		},
		{
			name: "mock-name/passes-register-refused-at-gate", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{real(usdc, "USDC", "mock liquidity token", 6, true)},
			labels: map[string]string{chainA.Hex(): "Lux C-Chain"},
		},
		{
			name: "white-label-chain/refused-at-gate", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{real(usdc, "USDC", "USD Coin", 6, true)},
			labels: map[string]string{chainA.Hex(): "Liquidity primary network"},
		},
		{
			name: "synthetic-flag/mainnet/refused-at-gate", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DexAssetPolicy{AllowSyntheticAssets: true},
		},
		{
			name: "synthetic-flag/testnet/refused-at-gate", class: registry.NetworkClassTestnet, allowed: all3,
			policy: registry.DexAssetPolicy{AllowMockLiquidity: true},
		},
		{
			name: "synthetic-flag/dev/passes", class: registry.NetworkClassDev, allowed: all3,
			policy: registry.DexAssetPolicy{AllowSyntheticAssets: true, AllowSyntheticMarkets: true, AllowMockLiquidity: true},
		},
		{
			name: "bad-allowed-kind/refused-at-gate", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DexAssetPolicy{
				AllowedAssetKinds: []registry.AssetKind{registry.AssetKindERC20, registry.AssetKindInvalid},
			},
		},
		{
			name: "empty-registry/passes", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
		},
		{
			name:  "disabled-market-over-synthetic/passes (gate scans ENABLED markets)",
			class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6), seedERC20(wlux, 18)},
			assets: []registry.Asset{
				real(usdc, "USDC", "USD Coin", 6, true),
				real(wlux, "WLUX", "Wrapped LUX", 18, true),
			},
			markets: []registry.Market{{
				NetworkID: netMain, BaseAssetID: wluxID, QuoteAssetID: usdcID, Enabled: false,
			}},
			labels: map[string]string{chainA.Hex(): "Lux C-Chain"},
		},
		{
			name: "utxo-asset/real/passes", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds: []seedEntry{{
				NetworkID: netMain, Chain: chainB.Hex(), Kind: "UTXO", Ref: hx(xasset[:]), Decimals: 9,
			}},
			assets: []registry.Asset{{
				NetworkID: netMain, ChainID: chainB, Kind: registry.AssetKindUTXO,
				CanonicalRef: xasset[:], Decimals: 9, Symbol: "XAV", Name: "X asset",
				Enabled: true, RiskTier: registry.RiskTier1,
			}},
			labels: map[string]string{chainB.Hex(): "Lux X-Chain"},
		},
		{
			name: "risk-tier-out-of-range/refused-at-register", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{{
				NetworkID: netMain, ChainID: chainA, Kind: registry.AssetKindERC20,
				CanonicalRef: usdc, Decimals: 6, Symbol: "USDC", Name: "USD Coin",
				Enabled: true, RiskTier: registry.RiskTier(9),
			}},
		},
		{
			name: "ticker-symbol/refused-at-register", class: registry.NetworkClassMainnet, allowed: all3,
			policy: registry.DefaultDexAssetPolicy(),
			seeds:  []seedEntry{seedERC20(usdc, 6)},
			assets: []registry.Asset{real(usdc, "LUX/USDC", "Pair", 6, true)},
		},
	}

	cases := make([]gateCase, 0, len(scenarios))
	for _, s := range scenarios {
		cases = append(cases, runScenario(s))
	}
	return cases
}

func runScenario(s scenario) gateCase {
	c := gateCase{
		Name:   s.name,
		Class:  uint8(s.class),
		Labels: s.labels,
		Seeds:  s.seeds,
	}
	for _, k := range s.allowed {
		c.Allowed = append(c.Allowed, k.String())
	}
	c.Policy = policyJSON{
		SynthAssets:  s.policy.AllowSyntheticAssets,
		SynthMarkets: s.policy.AllowSyntheticMarkets,
		MockLiq:      s.policy.AllowMockLiquidity,
	}
	for _, k := range s.policy.AllowedAssetKinds {
		c.Policy.Kinds = append(c.Policy.Kinds, k.String())
		c.Policy.KindBytes = append(c.Policy.KindBytes, int(k))
	}

	// The chain snapshot this scenario declares.
	chain := &snapshotChain{}
	for _, sd := range s.seeds {
		var id ids.ID
		copy(id[:], unhx(sd.Chain))
		chain.seed(sd.NetworkID, id, sd.Kind, unhx(sd.Ref), sd.Decimals)
	}

	reg := registry.New(s.allowed...)
	for _, a := range s.assets {
		e := assetEntry{
			NetworkID: a.NetworkID, Chain: a.ChainID.Hex(), Kind: a.Kind.String(),
			Ref: hx(a.CanonicalRef), Decimals: a.Decimals, Symbol: a.Symbol,
			Name: a.Name, Enabled: a.Enabled, RiskTier: uint8(a.RiskTier),
		}
		id, err := reg.Register(a, chain)
		if err != nil {
			e.Status = "REFUSED"
			e.Code = codeOf(err)
		} else {
			e.Status = "OK"
			e.ID = id.Hex()
		}
		c.Assets = append(c.Assets, e)
	}
	for _, m := range s.markets {
		e := marketEntry{
			NetworkID: m.NetworkID, Base: m.BaseAssetID.Hex(), Quote: m.QuoteAssetID.Hex(),
			Venue: hx(m.VenueConfig), Enabled: m.Enabled,
		}
		id, err := reg.CreateMarket(m)
		if err != nil {
			e.Status = "REFUSED"
			e.Code = codeOf(err)
		} else {
			e.Status = "OK"
			e.ID = id.Hex()
		}
		c.Markets = append(c.Markets, e)
	}

	labelFor := func(id ids.ID) string {
		if s.labels == nil {
			return ""
		}
		return s.labels[id.Hex()]
	}
	if err := registry.RefuseUnderSyntheticConfig(s.class, s.policy, reg, labelFor); err != nil {
		c.Status = "REFUSED"
		c.Code = codeOf(err)
	} else {
		c.Status = "OK"
	}
	return c
}
