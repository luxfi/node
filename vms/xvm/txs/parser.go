// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Parse dispatch. There is no codec: ParseTx wraps the wire.SignedTx envelope
// zero-copy, reads the 1-byte xkind at object offset 0 of the unsigned body to
// select the concrete tx type, and reconstructs each fx credential by its
// (TypeKind, ShapeKind) wire discriminator — no reflect, no slot map.

import (
	"fmt"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/wire"
	"github.com/luxfi/zap"
)

var (
	_ Parser         = (*parser)(nil)
	_ secp256k1fx.VM = (*fxVM)(nil)
)

type Parser interface {
	ParseTx(bytes []byte) (*Tx, error)
	ParseGenesisTx(bytes []byte) (*Tx, error)
}

type parser struct {
	fxs []fxs.Fx
}

func NewParser(fxs []fxs.Fx) (Parser, error) {
	return NewCustomParser(
		NewFxIndex(),
		&mockable.Clock{},
		log.Noop(),
		fxs,
	)
}

func NewCustomParser(
	fxIndex *FxIndex,
	clock *mockable.Clock,
	logger log.Logger,
	fxList []fxs.Fx,
) (Parser, error) {
	vm := &fxVM{clock: clock, log: logger}
	for i, fx := range fxList {
		if err := fx.Initialize(vm); err != nil {
			return nil, err
		}
		// Record the fx family -> list-position mapping the semantic verifier's
		// getFx and tx_init consult. Keyed by wire.TypeKind (the family tag),
		// filled by the SAME closed-set match used everywhere else — no
		// reflect.TypeOf, no map[reflect.Type]int.
		switch fx.(type) {
		case *secp256k1fx.Fx:
			fxIndex.Set(wire.TypeKindSecp256k1, i)
		case *nftfx.Fx:
			fxIndex.Set(wire.TypeKindNFT, i)
		case *propertyfx.Fx:
			fxIndex.Set(wire.TypeKindProperty, i)
		}
	}
	return &parser{fxs: fxList}, nil
}

// Parse decodes a signed X-chain tx from its wire bytes. It is the codec-free,
// parser-instance-free entry point used by the block layer and any consumer
// holding raw tx bytes; fx credential dispatch is envelope-based (stateless).
func Parse(signedBytes []byte) (*Tx, error) {
	return parseSignedTx(signedBytes)
}

func (*parser) ParseTx(bytes []byte) (*Tx, error) {
	return parseSignedTx(bytes)
}

func (*parser) ParseGenesisTx(bytes []byte) (*Tx, error) {
	return parseSignedTx(bytes)
}

// parseSignedTx wraps a wire.SignedTx envelope: the leading unsigned body plus
// the packed fx credential list. TxID = hash(signedBytes), byte-preserving.
func parseSignedTx(signedBytes []byte) (*Tx, error) {
	st, err := wire.WrapSignedTx(signedBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse signed tx: %w", err)
	}
	unsigned, err := parseUnsigned(st.UnsignedBytes())
	if err != nil {
		return nil, fmt.Errorf("couldn't parse unsigned tx: %w", err)
	}
	creds, err := parseCreds(st)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse credentials: %w", err)
	}
	return &Tx{
		Unsigned: unsigned,
		Creds:    creds,
		TxID:     hash.ComputeHash256Array(signedBytes),
		bytes:    signedBytes,
	}, nil
}

// parseUnsigned wraps the leading unsigned body as the typed UnsignedTx by
// dispatching on its xkind discriminator (object offset 0).
func parseUnsigned(unsignedBytes []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(unsignedBytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	switch k := xkindOf(msg); k {
	case xkindBase:
		return parseBaseTx(unsignedBytes, obj)
	case xkindCreateAsset:
		return parseCreateAssetTx(unsignedBytes, obj)
	case xkindOperation:
		return parseOperationTx(unsignedBytes, obj)
	case xkindImport:
		return parseImportTx(unsignedBytes, obj)
	case xkindExport:
		return parseExportTx(unsignedBytes, obj)
	default:
		return nil, fmt.Errorf("xvm txs: unknown tx kind %d", k)
	}
}

// parseCreds reconstructs the fx credential list, dispatching each envelope on
// its TypeKind. FxID is recovered later by tx_init.InitializeFx.
func parseCreds(st wire.SignedTx) ([]*fxs.FxCredential, error) {
	n := st.CredentialCount()
	if n == 0 {
		return nil, nil
	}
	creds := make([]*fxs.FxCredential, 0, n)
	blob := st.CredentialBytes()
	for i := uint32(0); i < n; i++ {
		env, rest, err := wire.NextEnvelope(blob)
		if err != nil {
			return nil, err
		}
		cred, err := wrapFxCredential(env)
		if err != nil {
			return nil, err
		}
		creds = append(creds, &fxs.FxCredential{Credential: cred})
		blob = rest
	}
	return creds, nil
}

// fxVM is the minimal VM surface an fx needs at Initialize time (clock +
// logger). ZAP-native: fx wire schemas are compile-time static, so there is no
// codec registry to provide.
type fxVM struct {
	clock *mockable.Clock
	log   log.Logger
}

func (vm *fxVM) Clock() *mockable.Clock { return vm.clock }
func (vm *fxVM) Logger() log.Logger     { return vm.log }

// ParseUnsignedTx decodes native-ZAP unsigned-tx bytes (as produced by
// UnsignedBytes) back into the concrete UnsignedTx, dispatching on the xkind
// discriminator. Used by the X-Chain genesis wire to re-hydrate each
// GenesisAsset's embedded CreateAssetTx.
func ParseUnsignedTx(unsignedBytes []byte) (UnsignedTx, error) {
	return parseUnsigned(unsignedBytes)
}
