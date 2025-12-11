module github.com/luxfi/node

// - Changes to the minimum golang version must also be replicated in:
//   - CONTRIBUTING.md
//   - README.md
//   - go.mod (here)
//
// - If updating between minor versions (e.g. 1.23.x -> 1.24.x):
//   - Consider updating the version of golangci-lint (in scripts/lint.sh).
go 1.25.5

require (
	connectrpc.com/connect v1.18.1
	connectrpc.com/grpcreflect v1.3.0
	github.com/DataDog/zstd v1.5.7
	github.com/StephenButtolph/canoto v0.17.2
	github.com/btcsuite/btcd/btcutil v1.1.6
	github.com/cockroachdb/pebble v1.1.5
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/google/btree v1.1.3
	github.com/google/renameio/v2 v2.0.0
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/rpc v1.2.1
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/holiman/uint256 v1.3.2
	github.com/huin/goupnp v1.3.0
	github.com/jackpal/gateway v1.1.1
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/luxfi/consensus v1.22.4
	github.com/luxfi/crypto v1.17.9
	github.com/luxfi/database v1.2.7
	github.com/luxfi/ids v1.1.2
	github.com/luxfi/ledger-lux-go v1.0.0
	github.com/luxfi/log v1.1.24
	github.com/luxfi/math v0.1.5
	github.com/luxfi/metric v1.4.5
	github.com/luxfi/mock v0.1.0
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mr-tron/base58 v1.2.0
	github.com/nbutton23/zxcvbn-go v0.0.0-20210217022336-fa2cb2858354
	github.com/onsi/ginkgo/v2 v2.25.1
	github.com/pires/go-proxyproto v0.8.1
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.66.1 // indirect
	github.com/rs/cors v1.11.1
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.7
	github.com/spf13/viper v1.20.1
	github.com/stretchr/testify v1.11.1
	github.com/supranational/blst v0.3.16-0.20250831170142-f48500c1fdbe
	github.com/syndtr/goleveldb v1.0.1-0.20220614013038-64ee5596c38a // indirect
	github.com/thepudds/fzgen v0.4.3
	github.com/tyler-smith/go-bip32 v1.0.0
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.37.0
	go.opentelemetry.io/otel/sdk v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
	go.uber.org/goleak v1.3.0
	go.uber.org/mock v0.6.0
	golang.org/x/crypto v0.43.0
	golang.org/x/exp v0.0.0-20250819193227-8b4c13bb791b
	golang.org/x/mod v0.28.0
	golang.org/x/net v0.45.0
	golang.org/x/sync v0.17.0
	golang.org/x/term v0.36.0
	golang.org/x/time v0.12.0
	golang.org/x/tools v0.37.0
	gonum.org/v1/gonum v0.16.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251022142026-3a174f9686a8
	google.golang.org/grpc v1.75.1
	google.golang.org/protobuf v1.36.10
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/FactomProject/basen v0.0.0-20150613233007-fe3947df716e // indirect
	github.com/FactomProject/btcutilecc v0.0.0-20130527213604-d3a63a5752ec // indirect
	github.com/Microsoft/go-winio v0.6.2
	github.com/VictoriaMetrics/fastcache v1.13.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.3 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.5 // indirect
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/cockroachdb/errors v1.12.0 // indirect
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506 // indirect
	github.com/cockroachdb/redact v1.1.6 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20250429170803-42689b6311bb // indirect
	github.com/consensys/gnark-crypto v0.19.2 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.8.0 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/getsentry/sentry-go v0.35.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/pprof v0.0.0-20250820193118-f64d9cf942d6 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.1 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.18.0
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sanity-io/litter v1.5.5 // indirect
	github.com/spf13/afero v1.14.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zondax/hid v0.9.2 // indirect
	github.com/zondax/ledger-go v1.0.1 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	go.opentelemetry.io/proto/otlp v1.7.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250811230008-5f3141c8851a // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

require (
	github.com/dgraph-io/badger/v3 v3.2103.5
	github.com/dgraph-io/badger/v4 v4.8.0
	github.com/ethereum/go-ethereum v1.16.7
	github.com/golang-jwt/jwt/v4 v4.5.2
	github.com/golang/mock v1.7.0-rc.1
	github.com/luxfi/genesis v1.2.10
	github.com/luxfi/geth v1.16.40
	github.com/luxfi/go-bip39 v1.1.2
	github.com/luxfi/qzmq v0.1.1
	github.com/luxfi/threshold v1.1.0
	github.com/luxfi/trace v0.1.2
	github.com/luxfi/warp v1.16.26
	github.com/spaolacci/murmur3 v1.1.0
	github.com/tyler-smith/go-bip39 v1.1.0
	go.uber.org/zap v1.27.0
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20251001021608-1fe7b43fc4d6 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cloudflare/circl v1.6.2-0.20251027185721-da1faa40b98c // indirect
	github.com/cockroachdb/fifo v0.0.0-20240816210425-c5d0cb0b6fc0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/crate-crypto/go-eth-kzg v1.4.0 // indirect
	github.com/cronokirby/saferith v0.33.0 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/dgraph-io/ristretto v0.2.0 // indirect
	github.com/dgraph-io/ristretto/v2 v2.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emicklei/dot v1.9.0 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.5 // indirect
	github.com/ethereum/go-bigmodexpfix v0.0.0-20250911101455-f9e208c548ab // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/ferranbt/fastssz v1.0.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/hashicorp/go-bexpr v0.1.14 // indirect
	github.com/holiman/billy v0.0.0-20250707135307-f2f9b9aae7db // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/luxfi/ai v0.0.0-20251211041856-0feda9795706 // indirect
	github.com/luxfi/czmq/v4 v4.2.0 // indirect
	github.com/luxfi/zmq/v4 v4.2.2 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/pointerstructure v1.2.1 // indirect
	github.com/pion/dtls/v2 v2.2.7 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/stun/v2 v2.0.0 // indirect
	github.com/pion/transport/v2 v2.2.1 // indirect
	github.com/pion/transport/v3 v3.0.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sagikazarmark/locafero v0.10.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/urfave/cli/v2 v2.27.7 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	github.com/zondax/golem v0.27.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
)

replace google.golang.org/genproto => google.golang.org/genproto/googleapis/rpc v0.0.0-20250908214217-97024824d090
