module github.com/luxfi/node

// - Changes to the minimum golang version must also be replicated in:
//   - CONTRIBUTING.md
//   - README.md
//   - go.mod (here)
//
// - If updating between minor versions (e.g. 1.23.x -> 1.24.x):
//   - Consider updating the version of golangci-lint (in scripts/lint.sh).
go 1.24.5

require (
	connectrpc.com/connect v1.18.1
	connectrpc.com/grpcreflect v1.3.0
	github.com/DataDog/zstd v1.5.7
	github.com/StephenButtolph/canoto v0.17.1
	github.com/btcsuite/btcd/btcutil v1.1.3
	github.com/cockroachdb/pebble v1.1.5
	github.com/ethereum/go-ethereum v1.16.1
	github.com/google/btree v1.1.3
	github.com/google/renameio/v2 v2.0.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/rpc v1.2.1
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/holiman/uint256 v1.3.2
	github.com/huin/goupnp v1.3.0
	github.com/jackpal/gateway v1.0.6
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/leanovate/gopter v0.2.11
	github.com/luxfi/crypto v1.1.1
	github.com/luxfi/database v1.1.4
	github.com/luxfi/evm v0.8.2
	github.com/luxfi/geth v1.16.6
	github.com/luxfi/ids v0.1.1
	github.com/luxfi/ledger-lux-go v0.0.2
	github.com/luxfi/log v0.1.1
	github.com/luxfi/metrics v1.1.1
	github.com/luxfi/trace v0.1.0
	github.com/mitchellh/mapstructure v1.5.0
	github.com/mr-tron/base58 v1.2.0
	github.com/nbutton23/zxcvbn-go v0.0.0-20180912185939-ae427f1e4c1d
	github.com/onsi/ginkgo/v2 v2.23.4
	github.com/pires/go-proxyproto v0.7.0
	github.com/prometheus/client_golang v1.22.0
	github.com/prometheus/client_model v0.6.1
	github.com/prometheus/common v0.62.0
	github.com/rs/cors v1.10.1
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/spf13/cast v1.9.2
	github.com/spf13/cobra v1.9.1
	github.com/spf13/pflag v1.0.6
	github.com/spf13/viper v1.20.1
	github.com/stretchr/testify v1.10.0
	github.com/thepudds/fzgen v0.4.3
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
	go.uber.org/goleak v1.3.0
	go.uber.org/mock v0.5.2
	go.uber.org/zap v1.27.0
	golang.org/x/crypto v0.40.0
	golang.org/x/exp v0.0.0-20250718183923-645b1fa84792
	golang.org/x/mod v0.26.0
	golang.org/x/net v0.42.0
	golang.org/x/sync v0.16.0
	golang.org/x/term v0.33.0
	golang.org/x/time v0.12.0
	golang.org/x/tools v0.35.0
	gonum.org/v1/gonum v0.14.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250728155136-f173205681a0
	google.golang.org/grpc v1.74.2
	google.golang.org/protobuf v1.36.6
	k8s.io/api v0.29.0
	k8s.io/apimachinery v0.29.0
	k8s.io/client-go v0.29.0
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b
)

replace (
	github.com/luxfi/bft => ../bft
	github.com/luxfi/crypto => ../crypto
	github.com/luxfi/database => ../database
	github.com/luxfi/ids => ../ids
	github.com/luxfi/ledger-lux-go => ../ledger-lux-go
	github.com/luxfi/log => ../log
	github.com/luxfi/ringtail => ../ringtail
	github.com/luxfi/trace => ../trace
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/VictoriaMetrics/fastcache v1.12.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.4 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.6.1 // indirect
	github.com/cockroachdb/errors v1.11.3 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240606204812-0bbfbd93a7ce // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/redact v1.1.5 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/crate-crypto/go-eth-kzg v1.3.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0 // indirect
	github.com/dgraph-io/badger/v4 v4.8.0 // indirect
	github.com/dgraph-io/ristretto/v2 v2.2.0 // indirect
	github.com/dlclark/regexp2 v1.7.0 // indirect
	github.com/dop251/goja v0.0.0-20230806174421-c933cf95e127 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emicklei/dot v1.6.2 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.1 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/ferranbt/fastssz v0.1.4 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/gballet/go-libpcsclite v0.0.0-20191108122812-4678299bea08 // indirect
	github.com/getsentry/sentry-go v0.27.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20250403155104-27863c87afa6 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.1 // indirect
	github.com/hashicorp/go-bexpr v0.1.14 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d // indirect
	github.com/holiman/billy v0.0.0-20250707135307-f2f9b9aae7db // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/luxfi/go-bip39 v1.1.1 // indirect
	github.com/luxfi/warp v0.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mitchellh/pointerstructure v1.2.1 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/pion/dtls/v2 v2.2.7 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/stun/v2 v2.0.0 // indirect
	github.com/pion/transport/v2 v2.2.1 // indirect
	github.com/pion/transport/v3 v3.0.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sagikazarmark/locafero v0.7.0 // indirect
	github.com/sanity-io/litter v1.5.1 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.14.0 // indirect
	github.com/status-im/keycard-go v0.2.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/supranational/blst v0.3.15 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20220614013038-64ee5596c38a // indirect
	github.com/tklauser/go-sysconf v0.3.13 // indirect
	github.com/tklauser/numcpus v0.7.0 // indirect
	github.com/tyler-smith/go-bip39 v1.1.0 // indirect
	github.com/urfave/cli/v2 v2.27.7 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	github.com/zondax/hid v0.9.2 // indirect
	github.com/zondax/ledger-go v1.0.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.37.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.37.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.37.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	go.opentelemetry.io/otel/sdk v1.37.0 // indirect
	go.opentelemetry.io/proto/otlp v1.7.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250721164621-a45f3dfb1074 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/kube-openapi v0.0.0-20231010175941-2dd684a91f00 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace (
	github.com/luxfi/consensus => ../consensus
	github.com/luxfi/evm => ../evm
	github.com/luxfi/geth => ../geth
	github.com/luxfi/metrics => ../metrics
)
