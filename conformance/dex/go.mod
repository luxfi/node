// The AMM/DEX differential. Its own module for the same reason
// conformance/gen is: it links an EVM ABI and signer, which the node host
// must not carry.
module github.com/luxfi/node/conformance/dex

go 1.26.8

require (
	github.com/luxfi/crypto v1.20.9
	github.com/luxfi/geth v1.20.1
	golang.org/x/crypto v0.52.0
)

require (
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/consensys/gnark-crypto v0.20.1 // indirect
	github.com/crate-crypto/go-eth-kzg v1.5.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.7 // indirect
	github.com/gorilla/rpc v1.2.1 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/luxfi/accel v1.2.4 // indirect
	github.com/luxfi/cache v1.3.1 // indirect
	github.com/luxfi/container v0.2.1 // indirect
	github.com/luxfi/crypto/ipa v1.2.4 // indirect
	github.com/luxfi/ids v1.3.2 // indirect
	github.com/luxfi/log v1.4.3 // indirect
	github.com/luxfi/math v1.5.1 // indirect
	github.com/luxfi/math/big v0.1.0 // indirect
	github.com/luxfi/mdns v0.1.1 // indirect
	github.com/luxfi/metric v1.8.1 // indirect
	github.com/luxfi/mock v0.1.1 // indirect
	github.com/luxfi/pq v1.1.0 // indirect
	github.com/luxfi/zap v1.2.6 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/mr-tron/base58 v1.3.0 // indirect
	github.com/supranational/blst v0.3.16 // indirect
	go.uber.org/mock v0.6.0 // indirect
	golang.org/x/exp v0.0.0-20260529124908-c761662dc8c9 // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)
