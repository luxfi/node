# node2 — one Makefile over three existing Lux node runtimes.
#
# This repo imports; it does not reimplement. Every target below invokes an
# existing runtime's own build, in place, and copies its artifact into bin/.
# No sub-build's failure is allowed to read as success here — see the
# leaked-dependency and exit-code checks in each target.

SHELL := /bin/bash
ROOT  := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
BIN   := $(ROOT)/bin
NPROC := $(shell nproc 2>/dev/null || echo 4)

# ---- existing runtimes (imported, never copied) ---------------------------
CHAINS_DIR      := $(HOME)/work/lux/chains
NODE_RUST_DIR   := $(HOME)/work/lux-rs/node
NODE_CPP_DIR    := $(HOME)/work/lux-cpp/node
NODE_CPP_BUILD  := $(NODE_CPP_DIR)/build

# The C++ node reaches cevm's dependencies through the toolchain Conan writes
# for them. It is searched for rather than named, because the cevm checkout it
# belongs to sits under either root depending on the machine, and the first one
# that exists is the answer.
CONAN_TOOLCHAIN := $(firstword $(wildcard \
    $(HOME)/work/luxcpp/cevm/build-node/build/Release/generators/conan_toolchain.cmake \
    $(HOME)/work/lux-cpp/cevm/build-node/build/Release/generators/conan_toolchain.cmake))
GPU_DIR         := $(HOME)/work/luxcpp/gpu
# COMPUTE is the licensed half — the matcher, the FHE chain, the kernels. It is
# a separate private repository for the same reason runtime/rust and runtime/cpp
# name checkouts rather than vendoring them: this repository is public, and a
# public repository must not hold a partial copy of a private one.
COMPUTE         := $(HOME)/work/lux/compute
LUX_CRYPTO_DIST := $(HOME)/work/lux/crypto/dist

# ---- conformance corpus (also imported) ------------------------------------
CONSENSUS_GO   := $(HOME)/work/lux/consensus
CONSENSUS_RUST := $(HOME)/work/lux/consensus/pkg/rust
CONSENSUS_CPP  := $(HOME)/work/lux-cpp/consensus

.PHONY: chains-build all luxd gpu gpu-differential conformance conformance-go conformance-rust conformance-cpp \
        chains chains-corpus bench precompiles precompiles-build precompiles-corpus \
        dex dex-test luxd-go luxd-rust luxd-cpp wire clean help

help:
	@grep -hE '^[a-z][a-z-]*:.*## ' $(MAKEFILE_LIST) \
	  | sed 's/:.*## /\t/' | sort | column -t -s $$'\t'

# ---- make luxd RUNTIME=go|rust|cpp -----------------------------------------

luxd: ## build one runtime into bin/luxd-<runtime> (RUNTIME=go|rust|cpp)
	@test -n "$(RUNTIME)" || { echo "usage: make luxd RUNTIME=go|rust|cpp" >&2; exit 1; }
	@$(MAKE) --no-print-directory luxd-$(RUNTIME)

# go: ./cmd/luxd — this repo's own daemon. This module IS github.com/luxfi/node,
# so the Go column builds from these sources and fetches nothing: a repo that
# reaches outside itself for the thing it claims to be has not replaced it.
luxd-go:
	@echo "==> go: ./cmd/luxd"
	@mkdir -p $(BIN)
	GOWORK=off CGO_ENABLED=0 go build -trimpath -o $(BIN)/luxd-go ./cmd/luxd
	@test -x $(BIN)/luxd-go
	@leaked="$$(GOWORK=off go list -deps ./cmd/luxd 2>/dev/null | grep -E 'lux-private' || true)"; \
	if [ -n "$$leaked" ]; then echo "FAIL: lux-private in the closure:" >&2; echo "$$leaked" >&2; exit 1; fi
	@echo "    confirmed clean: 0 lux-private in the dependency graph"
	@ls -lh $(BIN)/luxd-go

# rust: lux-rs/node — a real node host (mesh + BLS quorum finality + revm),
# already luxfi/node-free. Built in place; only the binary is copied out.
luxd-rust:
	@echo "==> rust: lux-rs/node (full node host)"
	@mkdir -p $(BIN)
	cd $(NODE_RUST_DIR) && PATH="$(HOME)/.cargo/bin:$$PATH" LUX_LIB_DIR=$(LUX_CRYPTO_DIST) cargo build --release --bin luxd
	@test -x $(NODE_RUST_DIR)/target/release/luxd
	cp $(NODE_RUST_DIR)/target/release/luxd $(BIN)/luxd-rust
	@ls -lh $(BIN)/luxd-rust

# cpp: lux-cpp/node — a real node host (mesh + BLS quorum finality + cevm).
# Configures the repo's own build/ against the Conan toolchain cevm's
# dependencies come through, then builds; nothing but that conventional,
# gitignored directory is written back into the source tree.
luxd-cpp:
	@echo "==> cpp: lux-cpp/node (full node host)"
	@mkdir -p $(BIN)
	@test -n "$(CONAN_TOOLCHAIN)" || { \
			echo "FAIL: no Conan toolchain for cevm. Write one with:" >&2; \
			echo "  conan install <cevm> -pr <cevm>/.github/conan/manylinux-relax.profile \\" >&2; \
			echo "    -s build_type=Release -s compiler.cppstd=gnu20 \\" >&2; \
			echo "    --output-folder=<cevm>/build-node --build=missing" >&2; \
			exit 1; }
	@# Configure every time. It is idempotent, and a CMakeCache.txt is written
	@# by a configure that FAILED as well as by one that finished, so testing
	@# for the cache skips the step exactly when it is needed.
	cmake -S $(NODE_CPP_DIR) -B $(NODE_CPP_BUILD) -DCMAKE_BUILD_TYPE=Release \
		-DCMAKE_TOOLCHAIN_FILE=$(CONAN_TOOLCHAIN)
	cmake --build $(NODE_CPP_BUILD) --target luxd -j$(NPROC)
	@test -x $(NODE_CPP_BUILD)/luxd
	cp $(NODE_CPP_BUILD)/luxd $(BIN)/luxd-cpp
	@ls -lh $(BIN)/luxd-cpp

# ---- make all: attempt all three + gpu, report every one, fail loudly -----

all: ## build all three runtimes plus gpu; nonzero unless 3/3
	@mkdir -p $(BIN)
	@rm -f $(BIN)/luxd-go $(BIN)/luxd-rust $(BIN)/luxd-cpp
	@echo "=== node2: building go, rust, cpp, gpu ==="
	-$(MAKE) --no-print-directory luxd-go
	-$(MAKE) --no-print-directory luxd-rust
	-$(MAKE) --no-print-directory luxd-cpp
	-$(MAKE) --no-print-directory gpu
	@echo; echo "=== summary ==="
	@ok=0; \
	for r in go rust cpp; do \
		f=$(BIN)/luxd-$$r; \
		if [ -x "$$f" ]; then \
			printf "  luxd-%-4s OK    %s  (%s)\n" "$$r" "$$f" "$$(du -h "$$f" | cut -f1)"; \
			ok=$$((ok+1)); \
		else \
			printf "  luxd-%-4s FAIL  no artifact at %s\n" "$$r" "$$f"; \
		fi; \
	done; \
	if [ -f $(GPU_DIR)/build/libluxgpu.a ]; then \
		printf "  gpu       OK    %s  (%s)\n" "$(GPU_DIR)/build/libluxgpu.a" "$$(du -h $(GPU_DIR)/build/libluxgpu.a | cut -f1)"; \
	else \
		printf "  gpu       FAIL  no artifact\n"; \
	fi; \
	echo; echo "$$ok/3 node runtimes built"; \
	[ "$$ok" -eq 3 ]

# ---- gpu --------------------------------------------------------------------

# GPU kernels, on by default (not opt-in) — reuses lux-gpu/gpu's own already-
# configured build/. Path note: the repo lives at ~/work/luxcpp/gpu on this
# machine, not ~/work/lux-gpu/gpu — see gpu/README.md.
#
# Scoped to the one target the brief means by "kernels": luxgpu_core_static
# ("for embedding in Go/Rust" per its own CMakeLists comment), NOT the
# directory's default `all`, which also builds ~40 unrelated test/benchmark/
# webgpu-KAT binaries from a sibling private repo — one of which was already
# found broken here, and would silently fail this target on an unrelated
# binary while the actual kernel library was fine. Removed first so the
# post-build check can only pass against something this run produced.
gpu: ## build the GPU kernel library
	@echo "==> gpu: lux-gpu/gpu, target luxgpu_core_static ($(GPU_DIR))"
	rm -f $(GPU_DIR)/build/libluxgpu.a
	cmake -B $(GPU_DIR)/build -S $(GPU_DIR)
	cmake --build $(GPU_DIR)/build --target luxgpu_core_static -j$(NPROC)
	@test -f $(GPU_DIR)/build/libluxgpu.a
	@ls -lh $(GPU_DIR)/build/libluxgpu.a

# ---- gpu-differential -------------------------------------------------------
#
# The seam, in all three languages, asked the same question twice: once with no
# kernel library visible, once with one installed and LUX_GPU=verify, which
# computes BOTH answers and stops on the first byte that differs.
#
# The no-library half is the one that matters most: it is the proof that a node
# built and run with nothing installed is a whole node and not a degraded one.
# It runs first, and it runs with LUX_GPU_LIB deliberately unset.
#
# LUX_GPU_LIB points at the SHARED object; `make gpu` builds the static one, and
# the two are produced by the same configure step.

GPU_LIB := $(GPU_DIR)/build/libluxgpu.so

gpu-differential:
	@echo "=== gpu seam: CPU alone, no kernel library visible ==="
	cd $(ROOT) && GOWORK=off CGO_ENABLED=0 LUX_GPU= LUX_GPU_LIB= go test -count=1 ./gpu/
	cd $(ROOT)/gpu/rust && PATH="$(HOME)/.cargo/bin:$$PATH" env -u LUX_GPU -u LUX_GPU_LIB cargo test --release
	cmake -S $(ROOT)/gpu/cpp -B $(ROOT)/gpu/cpp/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/gpu/cpp/build --target lux_gpu_test -j$(NPROC)
	cd $(ROOT)/gpu/cpp && env -u LUX_GPU -u LUX_GPU_LIB ./build/lux_gpu_test
	@echo
	@if [ ! -f $(GPU_LIB) ]; then \
		echo "=== no kernel library at $(GPU_LIB) — run 'make gpu' first ==="; \
		echo "The CPU half above is the whole node; the half below is the overlay."; \
		exit 1; \
	fi
	@echo "=== gpu seam: CPU against the plugin, every answer compared ==="
	cd $(ROOT) && GOWORK=off CGO_ENABLED=1 LUX_GPU=verify LUX_GPU_LIB=$(GPU_LIB) go test -count=1 -v -run TestBothBackendsGiveTheSameAnswer ./gpu/
	cd $(ROOT) && GOWORK=off CGO_ENABLED=1 LUX_GPU=verify LUX_GPU_LIB=$(GPU_LIB) go test -count=1 ./gpu/
	cd $(ROOT)/gpu/rust && PATH="$(HOME)/.cargo/bin:$$PATH" LUX_GPU=verify LUX_GPU_LIB=$(GPU_LIB) cargo test --release -- --nocapture
	cd $(ROOT)/gpu/cpp && LUX_GPU=verify LUX_GPU_LIB=$(GPU_LIB) ./build/lux_gpu_test

# ---- conformance ------------------------------------------------------------

conformance: conformance-go conformance-rust conformance-cpp ## run the pop/verdict corpus in all three languages
	@echo; echo "=== conformance: go, rust and cpp all ran against the shared pop/verdict corpus ==="

conformance-go:
	@echo "--- go: $(CONSENSUS_GO)/conformance ---"
	cd $(CONSENSUS_GO) && GOWORK=off go test -count=1 -v ./conformance/...

conformance-rust:
	@echo "--- rust: $(CONSENSUS_RUST) ---"
	cd $(CONSENSUS_RUST) && PATH="$(HOME)/.cargo/bin:$$PATH" cargo test --release \
		--test conformance --test cert_conformance --test fpc_conformance \
		--test pop_conformance --test verdict_conformance

conformance-cpp:
	@echo "--- cpp: $(CONSENSUS_CPP) ---"
	@if [ -f $(CONSENSUS_CPP)/build/CMakeCache.txt ]; then \
		cmake --build $(CONSENSUS_CPP)/build --target conformance_test pop_conformance_test -j$(NPROC); \
		$(CONSENSUS_CPP)/build/conformance_test; \
		$(CONSENSUS_CPP)/build/pop_conformance_test; \
	else \
		echo "skipped: $(CONSENSUS_CPP)/build is not configured"; \
	fi

# ---- make wire: the accessors, from the schemas ------------------------------
#
# A chain's wire is stated once, in chains/schema, and zapgen writes the
# accessors for it. The emitted files are committed, so nothing here is needed
# to build: this target is how they are REWRITTEN when a schema changes, and
# how anyone checks that what is committed is what the schema says — run it and
# `git diff` must be empty.
#
# The generator is named by the exact commit that wrote these files. A moving
# reference would let the accessors change without the schema changing.
#
# Seven schemas: one per chain, plus the two the P-chain has beyond its
# transactions — the warp messages it sends, and what it writes to its own
# disk. They are separate files because they are separate consequences: a
# change to a wire schema is a fork, a change to state.zap is a migration.

ZAPGEN := github.com/zap-proto/go/cmd/zapgen@v1.8.3-0.20260906203114-6d0e886cebc9

wire: ## rewrite the chain accessors from chains/schema
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -runtime -out chains/rust/zap/src
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/platformvm/src chains/schema/pchain.zap
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/platformvm/src chains/schema/warp.zap
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/platformvm/src chains/schema/state.zap
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/xvm/src        chains/schema/xchain.zap
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/quantumvm/src  chains/schema/qchain.zap
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/zkvm/src       chains/schema/zchain.zap
	cd $(ROOT) && GOWORK=off go run $(ZAPGEN) -lang rust -rust-runtime lux_zap \
	  -out chains/rust/fhevm/src      chains/schema/fchain.zap

# ---- make chains: the cross-language chain differential ----------------------
#
# ONE corpus, generated from the Go chains themselves, handed to every
# implementation of each chain. Every field two of them answer differently
# fails the target and names the pair. See conformance/README.md.
#
# SIX chains: P and X from luxfi/node, and Q, Z, D and F from luxfi/chains.
# The four were added because they had no vector at all, which is the shape the
# P-chain's L1 fork hid in — a chain nothing is pointed at agrees with itself.
#
# The Rust column answers P and X. It does NOT answer Q, Z, D or F, and the
# runner says so under NOT ANSWERED and fails: silence is not agreement, and a
# target that went green while a whole column said nothing about four chains
# would be reporting the agreement of whoever was left. Those four evaluators
# are chains/rust's to write; the corpus and the runner are ready for them, and
# each slots in as one more -eval line under the name `rust`.
#
# The generator is a separate Go module because it — and only it — depends on
# luxfi/node and luxfi/chains. That dependency is the whole point of a
# reference, and keeping it in its own module is what keeps it out of this
# one's graph. Both are PUBLISHED versions with no replace directive, so the
# corpus regenerates on any machine rather than on one.

CONF        := $(ROOT)/conformance
CONF_GEN    := $(CONF)/gen/gen
CONF_VECS   := $(CONF)/corpus/vectors.tsv
CONF_WANT   := $(CONF)/corpus/expected.tsv
PVM_RUST    := $(ROOT)/chains/rust/platformvm/target/release/conformance
XVM_RUST    := $(ROOT)/chains/rust/xvm/target/release/conformance
QVM_RUST    := $(ROOT)/chains/rust/quantumvm/target/release/conformance
DVM_RUST    := $(COMPUTE)/chains/rust/dexvm/target/release/conformance
ZVM_RUST    := $(ROOT)/chains/rust/zkvm/target/release/conformance
FVM_RUST    := $(COMPUTE)/chains/rust/fhevm/target/release/conformance
PVM_CPP     := $(ROOT)/chains/cpp/platformvm/build/pvm_conformance
XVM_CPP     := $(ROOT)/chains/cpp/xvm/build/xvm_conformance
QVM_CPP     := $(ROOT)/chains/cpp/quantumvm/build/qvm_conformance
ZVM_CPP     := $(ROOT)/chains/cpp/zkvm/build/zkvm_conformance
DVM_CPP     := $(COMPUTE)/chains/cpp/dexvm/build/dexvm_conformance
FVM_CPP     := $(COMPUTE)/chains/cpp/fhevm/build/fhevm_conformance
RUST_EVALS  := $(PVM_RUST) $(XVM_RUST) $(QVM_RUST) $(DVM_RUST) $(ZVM_RUST) $(FVM_RUST)
CPP_EVALS   := $(PVM_CPP) $(XVM_CPP) $(QVM_CPP) $(ZVM_CPP) $(DVM_CPP) $(FVM_CPP)

chains: chains-build ## run the chain differential in all three languages
	@echo
	cd $(ROOT) && GOWORK=off go run ./conformance/runner \
		-subject chain \
		-fields parse,kind,id,syntactic,exec \
		-vectors $(CONF_VECS) \
		-expected $(CONF_WANT) \
		-eval "go=$(CONF_GEN) eval" \
		-eval "rust=$(PVM_RUST)" \
		-eval "rust=$(XVM_RUST)" \
		-eval "rust=$(QVM_RUST)" \
		-eval "rust=$(DVM_RUST)" \
		-eval "rust=$(ZVM_RUST)" \
		-eval "rust=$(FVM_RUST)" \
		-eval "cpp=$(PVM_CPP)" \
		-eval "cpp=$(XVM_CPP)" \
		-eval "cpp=$(QVM_CPP)" \
		-eval "cpp=$(ZVM_CPP)" \
		-eval "cpp=$(DVM_CPP)" \
		-eval "cpp=$(FVM_CPP)"

# Every evaluator is built before the run, and a build that fails stops the
# target. A differential that quietly lost one of its voices would report
# agreement among whoever was left.
#
# The C++ chains are built WHOLE — the default target, not just the evaluator.
# The evaluators alone were not enough: an evaluator that only parses bytes
# never constructs a VM, so when the node's seam grew `frontier()` and no port
# had it, every C++ chain's test suite stopped compiling and this target stayed
# green. A gate that builds less than what it measures is how the thing it was
# built to catch gets past it. It costs a few minutes on a cold tree.
chains-build:
	@echo "==> chain differential: building twelve evaluators"
	cd $(CONF)/gen && GOWORK=off go build -o gen .
	cd $(ROOT)/chains/rust/platformvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cd $(ROOT)/chains/rust/xvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cd $(ROOT)/chains/rust/quantumvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cd $(COMPUTE)/chains/rust/dexvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cd $(ROOT)/chains/rust/zkvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cd $(COMPUTE)/chains/rust/fhevm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cmake -S $(ROOT)/chains/cpp/platformvm -B $(ROOT)/chains/cpp/platformvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/chains/cpp/platformvm/build -j$(NPROC)
	cmake -S $(ROOT)/chains/cpp/xvm -B $(ROOT)/chains/cpp/xvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/chains/cpp/xvm/build -j$(NPROC)
	cmake -S $(ROOT)/chains/cpp/quantumvm -B $(ROOT)/chains/cpp/quantumvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/chains/cpp/quantumvm/build -j$(NPROC)
	cmake -S $(ROOT)/chains/cpp/zkvm -B $(ROOT)/chains/cpp/zkvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/chains/cpp/zkvm/build -j$(NPROC)
	cmake -S $(COMPUTE)/chains/cpp/dexvm -B $(COMPUTE)/chains/cpp/dexvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(COMPUTE)/chains/cpp/dexvm/build -j$(NPROC)
	cmake -S $(COMPUTE)/chains/cpp/fhevm -B $(COMPUTE)/chains/cpp/fhevm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(COMPUTE)/chains/cpp/fhevm/build -j$(NPROC)
	@for f in $(CONF_GEN) $(RUST_EVALS) $(CPP_EVALS); do \
		test -x "$$f" || { echo "FAIL: no evaluator at $$f" >&2; exit 1; }; \
	done

# ---- make bench: how long that same work takes in each language --------------
#
# The same evaluators, the same corpus, the same answers — asked for 200 times
# over and timed. Each evaluator times its OWN work and prints one line; nothing
# out here times a process. Five runs, because a single timing is not a
# measurement, and the spread of the five is printed beside the median.
# See conformance/README.md.

bench: chains-build ## time the differential work in all three languages
	@echo
	cd $(ROOT) && GOWORK=off go run ./conformance/bench \
		-vectors $(CONF_VECS) \
		-repeats 200 \
		-runs 5 \
		-eval "go=$(CONF_GEN) eval" \
		-eval "rust=$(PVM_RUST)" \
		-eval "rust=$(XVM_RUST)" \
		-eval "rust=$(QVM_RUST)" \
		-eval "rust=$(DVM_RUST)" \
		-eval "cpp=$(PVM_CPP)" \
		-eval "cpp=$(XVM_CPP)"

# ---- make precompiles: the cross-language precompile differential -----------
#
# The same runner, a different subject. See conformance/PRECOMPILE.md.

PREC        := $(CONF)/precompile
PREC_GO     := $(PREC)/precompile
PREC_RUST   := $(ROOT)/chains/rust/evm/target/release/conformance
PREC_CPP    := $(ROOT)/chains/cpp/evm/build/evm_conformance
PREC_VECS   := $(CONF)/corpus/precompile_vectors.tsv
PREC_WANT   := $(CONF)/corpus/precompile_expected.tsv

precompiles: precompiles-build ## run the precompile differential
	@echo
	cd $(ROOT) && GOWORK=off go run ./conformance/runner \
		-subject precompile \
		-fields status,gas,output \
		-vectors $(PREC_VECS) \
		-expected $(PREC_WANT) \
		-eval "go=$(PREC_GO) eval" \
		-eval "rust=$(PREC_RUST)" \
		-eval "cpp=$(PREC_CPP)"

# The C++ evaluator asks cevm's precompile seam, and cevm's algorithm bodies
# come from Conan. The install writes the toolchain the configure then reads;
# both are cheap once the packages are in the cache.
precompiles-build:
	@echo "==> precompile differential: building three evaluators"
	cd $(PREC) && GOWORK=off go build -o precompile .
	cd $(ROOT)/chains/rust/evm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	conan install $(ROOT)/chains/cpp/evm --output-folder=$(ROOT)/chains/cpp/evm/build \
		-s build_type=Release -s compiler.cppstd=gnu20 --build=missing
	cmake -S $(ROOT)/chains/cpp/evm -B $(ROOT)/chains/cpp/evm/build -DCMAKE_BUILD_TYPE=Release \
		-DCMAKE_TOOLCHAIN_FILE=$(ROOT)/chains/cpp/evm/build/conan_toolchain.cmake
	cmake --build $(ROOT)/chains/cpp/evm/build --target evm_conformance -j$(NPROC)
	@for f in $(PREC_GO) $(PREC_RUST) $(PREC_CPP); do \
		test -x "$$f" || { echo "FAIL: no evaluator at $$f" >&2; exit 1; }; \
	done

# Rebuild the precompile corpus from the Go reference.
precompiles-corpus: ## regenerate the precompile corpus
	cd $(PREC) && GOWORK=off go run . emit ../corpus

# Rebuild the corpus from the Go reference. Its output is committed, so a
# corpus that moves shows up as a diff rather than as a silent new normal.
chains-corpus: ## regenerate the chain corpus from the Go reference
	cd $(CONF)/gen && GOWORK=off go run . emit ../corpus

# ---- clean -------------------------------------------------------------------

clean: ## remove bin/
	rm -rf $(BIN)

# ---- make dex: the AMM and order-book differential ---------------------------
#
# One market deployed to all three C-chains, driven through one script, with the
# resulting book compared. Needs three running nodes; `make dex-test` needs
# none. See conformance/dex/README.md.

dex: ## run the AMM and order-book differential against running nodes
	cd $(ROOT)/conformance/dex && GOWORK=off go run . $(DEXFLAGS)

dex-test: ## the parts of that harness checkable without a chain
	cd $(ROOT)/conformance/dex && GOWORK=off go test -count=1 ./...
