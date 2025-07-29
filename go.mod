module github.com/luxfi/node

// Changes to the minimum golang version must also be replicated in
// CONTRIBUTING.md
// README.md
// go.mod (here)
go 1.24.5

replace github.com/tyler-smith/go-bip39 => github.com/luxfi/go-bip39 v1.1.0

// Pin all OpenTelemetry modules (and metric sub-packages) to v1.37.0
replace go.opentelemetry.io/otel => go.opentelemetry.io/otel v1.37.0

replace go.opentelemetry.io/otel/metric => go.opentelemetry.io/otel/metric v1.37.0

replace go.opentelemetry.io/otel/sdk => go.opentelemetry.io/otel/sdk v1.37.0

replace go.opentelemetry.io/otel/sdk/metric => go.opentelemetry.io/otel/sdk/metric v1.37.0

replace go.opentelemetry.io/otel/sdk/metric/metricdata => go.opentelemetry.io/otel/sdk/metric v1.37.0

replace go.opentelemetry.io/otel/sdk/metric/metricdatatest => go.opentelemetry.io/otel/sdk/metric v1.37.0

// Fix genproto ambiguity
exclude google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1

exclude google.golang.org/genproto v0.0.0-20220519153652-3a47de7e79bd

require (
	github.com/DataDog/zstd v1.5.7
	github.com/Microsoft/go-winio v0.6.2
	github.com/NYTimes/gziphandler v1.1.1
	github.com/antithesishq/antithesis-sdk-go v0.3.8
	github.com/btcsuite/btcd/btcutil v1.1.3
	github.com/cockroachdb/pebble v1.1.5
	github.com/compose-spec/compose-go v1.20.2
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0
	github.com/golang-jwt/jwt/v4 v4.5.1
	github.com/google/btree v1.1.3
	github.com/google/renameio/v2 v2.0.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/rpc v1.2.1
	github.com/gorilla/websocket v1.5.1
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/huin/goupnp v1.3.0
	github.com/jackpal/gateway v1.0.6
	github.com/jackpal/go-nat-pmp v1.0.2
	github.com/leanovate/gopter v0.2.11
	github.com/luxfi/geth v1.16.2
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
	github.com/spf13/viper v1.18.1
	github.com/stretchr/testify v1.10.0
	// github.com/supranational/blst v0.3.11
	github.com/syndtr/goleveldb v1.0.1-0.20220614013038-64ee5596c38a
	github.com/thepudds/fzgen v0.4.3
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.37.0
	go.opentelemetry.io/otel/sdk v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
	go.uber.org/goleak v1.3.0
	go.uber.org/mock v0.5.0
	go.uber.org/zap v1.27.0
	golang.org/x/crypto v0.40.0
	golang.org/x/exp v0.0.0-20250711185948-6ae5c78190dc
	golang.org/x/net v0.42.0
	golang.org/x/sync v0.16.0
	golang.org/x/term v0.33.0
	golang.org/x/time v0.12.0
	gonum.org/v1/gonum v0.14.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250721164621-a45f3dfb1074
	google.golang.org/grpc v1.74.2
	google.golang.org/protobuf v1.36.6
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gopkg.in/yaml.v3 v3.0.1
)

// require github.com/supranational/blst v0.3.11

require (
	github.com/StephenButtolph/canoto v0.17.1
	github.com/holiman/uint256 v1.3.2
	github.com/luxfi/bft v0.0.0-20250725034527-7f9105e62a1d
	github.com/supranational/blst v0.3.15
)

require (
	github.com/VictoriaMetrics/fastcache v1.12.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cockroachdb/errors v1.11.3 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240606204812-0bbfbd93a7ce // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/redact v1.1.5 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.3.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/distribution/reference v0.5.0 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/emicklei/dot v1.6.2 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.0 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/ferranbt/fastssz v0.1.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/getsentry/sentry-go v0.27.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v0.0.5-0.20220116011046-fa5810519dcb // indirect
	github.com/google/pprof v0.0.0-20250403155104-27863c87afa6 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.1 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/holiman/billy v0.0.0-20240216141850-2abb0c79d3c4 // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mattn/go-shellwords v1.0.12 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pelletier/go-toml/v2 v2.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/sagikazarmark/locafero v0.4.0 // indirect
	github.com/sagikazarmark/slog-shim v0.1.0 // indirect
	github.com/sanity-io/litter v1.5.5 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.14.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.13 // indirect
	github.com/tklauser/numcpus v0.7.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.37.0 // indirect
	go.opentelemetry.io/proto/otlp v1.7.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250721164621-a45f3dfb1074 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gotest.tools/v3 v3.5.1 // indirect
)

replace (
	github.com/luxfi/bft => ../bft
	github.com/luxfi/evm => ../evm
	github.com/luxfi/geth => ../geth
	github.com/luxfi/node => .
	launchpad.net/gocheck => gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b
)
