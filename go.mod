module github.com/luxfi/node

// - Changes to the minimum golang version must also be replicated in:
//   - CONTRIBUTING.md
//   - README.md
//   - go.mod (here)
//
// - If updating between minor versions (e.g. 1.23.x -> 1.24.x):
//   - Consider updating the version of golangci-lint (in scripts/lint.sh).
go 1.26.1

exclude github.com/luxfi/geth v1.16.1

require (
	connectrpc.com/connect v1.19.1
	github.com/DataDog/zstd v1.5.7 // indirect
	github.com/StephenButtolph/canoto v0.17.3
	github.com/btcsuite/btcd/btcutil v1.1.6
	github.com/cockroachdb/pebble v1.1.5 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/google/btree v1.1.3
	github.com/google/renameio/v2 v2.0.2
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/rpc v1.2.1
	github.com/holiman/uint256 v1.3.2
	github.com/huin/goupnp v1.3.0
	github.com/jackpal/gateway v1.1.1
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/luxfi/consensus v1.22.69
	github.com/luxfi/crypto v1.17.42
	github.com/luxfi/database v1.17.43
	github.com/luxfi/ids v1.2.9
	github.com/luxfi/keychain v1.0.2
	github.com/luxfi/log v1.4.1
	github.com/luxfi/math v1.2.4
	github.com/luxfi/metric v1.5.0
	github.com/luxfi/mock v0.1.1
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mr-tron/base58 v1.2.0
	github.com/onsi/ginkgo/v2 v2.27.5
	github.com/pires/go-proxyproto v0.8.1
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/rs/cors v1.11.1
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.11.1
	github.com/supranational/blst v0.3.16 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20220614013038-64ee5596c38a // indirect
	github.com/thepudds/fzgen v0.4.3
	go.opentelemetry.io/otel v1.40.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.39.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.39.0
	go.opentelemetry.io/otel/sdk v1.40.0
	go.opentelemetry.io/otel/trace v1.40.0
	go.uber.org/goleak v1.3.0
	go.uber.org/mock v0.6.0
	golang.org/x/crypto v0.47.0
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/mod v0.32.0
	golang.org/x/net v0.49.0
	golang.org/x/sync v0.19.0
	golang.org/x/term v0.39.0
	golang.org/x/time v0.14.0
	golang.org/x/tools v0.41.0
	gonum.org/v1/gonum v0.16.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/cockroachdb/errors v1.12.0 // indirect
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506 // indirect
	github.com/cockroachdb/redact v1.1.6 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20250429170803-42689b6311bb // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.8.0 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/getsentry/sentry-go v0.40.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.5 // indirect
	github.com/hashicorp/golang-lru v1.0.2
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.18.3
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/procfs v0.19.2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sanity-io/litter v1.5.5 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260120221211-b8f7ae30c516 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

require (
	connectrpc.com/grpcreflect v1.3.0
	github.com/cloudflare/circl v1.6.3
	github.com/consensys/gnark-crypto v0.19.2
	github.com/golang-jwt/jwt/v4 v4.5.2
	github.com/golang/mock v1.7.0-rc.1
	github.com/luxfi/accel v1.0.6
	github.com/luxfi/ai v0.0.0-20251225021023-3f15131f2bd1
	github.com/luxfi/api v1.0.4
	github.com/luxfi/atomic v1.0.0
	github.com/luxfi/compress v0.0.5
	github.com/luxfi/constants v1.4.4
	github.com/luxfi/container v0.0.4
	github.com/luxfi/filesystem v0.0.1
	github.com/luxfi/genesis v1.6.2
	github.com/luxfi/geth v1.16.76
	github.com/luxfi/go-bip39 v1.1.2
	github.com/luxfi/lattice/v7 v7.0.0
	github.com/luxfi/math/safe v0.0.1
	github.com/luxfi/net v0.0.4
	github.com/luxfi/p2p v1.19.2-zap
	github.com/luxfi/resource v0.0.1
	github.com/luxfi/rpc v1.0.0
	github.com/luxfi/runtime v1.0.1
	github.com/luxfi/sdk v1.16.45
	github.com/luxfi/sys v0.0.0-20260110090042-50187ec5ffd8
	github.com/luxfi/threshold v1.5.5
	github.com/luxfi/timer v1.0.2
	github.com/luxfi/units v1.0.0
	github.com/luxfi/utils v1.1.4
	github.com/luxfi/utxo v0.2.4
	github.com/luxfi/validators v1.0.0
	github.com/luxfi/vm v1.0.39
	github.com/luxfi/warp v1.18.5
	github.com/nbutton23/zxcvbn-go v0.0.0-20210217022336-fa2cb2858354
	github.com/spaolacci/murmur3 v1.1.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.39.0
	go.uber.org/zap v1.27.1
)

require (
	github.com/luxfi/staking v1.1.0 // indirect
	github.com/luxfi/trace v0.1.4 // indirect
	github.com/luxfi/zapdb/v4 v4.9.3 // indirect
)

require (
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/luxfi/concurrent v0.0.3
	github.com/luxfi/protocol v0.0.3 // indirect
	github.com/luxfi/upgrade v1.0.0 // indirect
	github.com/luxfi/version v1.0.1
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)

require (
	github.com/ALTree/bigfloat v0.2.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20251230134950-44c893854e3f // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240816210425-c5d0cb0b6fc0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.4.0 // indirect
	github.com/cronokirby/saferith v0.33.0 // indirect
	github.com/dgraph-io/ristretto/v2 v2.4.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emicklei/dot v1.10.0 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.5 // indirect
	github.com/ethereum/go-bigmodexpfix v0.0.0-20250911101455-f9e208c548ab // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/ferranbt/fastssz v1.0.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20251213031049-b05bdaca462f // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/luxfi/address v1.0.1
	github.com/luxfi/cache v1.2.1 // indirect
	github.com/luxfi/codec v1.1.4
	github.com/luxfi/formatting v1.0.1
	github.com/luxfi/go-bip32 v1.0.2 // indirect
	github.com/luxfi/math/big v0.1.0 // indirect
	github.com/luxfi/precompile v0.4.7 // indirect
	github.com/luxfi/ringtail v0.2.0 // indirect
	github.com/luxfi/sampler v1.0.0 // indirect
	github.com/luxfi/tls v1.0.3 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
)

exclude github.com/ethereum/go-ethereum v1.10.26
