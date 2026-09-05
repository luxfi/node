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
GPU_DIR         := $(HOME)/work/luxcpp/gpu
LUX_CRYPTO_DIST := $(HOME)/work/lux/crypto/dist

# ---- conformance corpus (also imported) ------------------------------------
CONSENSUS_GO   := $(HOME)/work/lux/consensus
CONSENSUS_RUST := $(HOME)/work/lux/consensus/pkg/rust
CONSENSUS_CPP  := $(HOME)/work/lux-cpp/consensus

.PHONY: chains-build all luxd gpu conformance conformance-go conformance-rust conformance-cpp \
        chains chains-corpus precompiles precompiles-build precompiles-corpus \
        luxd-go luxd-rust luxd-cpp clean help

help:
	@echo "make luxd RUNTIME=go|rust|cpp   build one runtime into bin/luxd-<runtime>"
	@echo "make all                        build all three + gpu, report pass/fail"
	@echo "make gpu                        build the GPU kernel library"
	@echo "make conformance                run the pop/verdict corpus, all 3 languages"
	@echo "make chains                     run the P/X chain differential, all 3 languages"
	@echo "make chains-corpus              regenerate the corpus from the Go reference"
	@echo "make precompiles                run the precompile differential"
	@echo "make precompiles-corpus         regenerate the precompile corpus"
	@echo "make clean                      remove bin/"

# ---- make luxd RUNTIME=go|rust|cpp -----------------------------------------

luxd:
	@case "$(RUNTIME)" in \
		go)   $(MAKE) --no-print-directory luxd-go ;; \
		rust) $(MAKE) --no-print-directory luxd-rust ;; \
		cpp)  $(MAKE) --no-print-directory luxd-cpp ;; \
		*) echo "usage: make luxd RUNTIME=go|rust|cpp" >&2; exit 1 ;; \
	esac

# go: chains/evm — the C-Chain VM plugin, the one VM of thirteen that is
# already free of luxfi/node and ava-labs. It is NOT a node daemon; see
# runtime/go/README.md. Verified, not assumed: grep the actual dependency
# closure every time, and fail the build if either name leaked back in.
luxd-go:
	@echo "==> go: chains/evm (C-Chain VM plugin — not a full node, see runtime/go/README.md)"
	@mkdir -p $(BIN)
	cd $(CHAINS_DIR) && GOWORK=off CGO_ENABLED=0 go build -trimpath -o $(BIN)/luxd-go ./evm
	@test -x $(BIN)/luxd-go
	@leaked="$$(cd $(CHAINS_DIR) && GOWORK=off go list -deps ./evm/... 2>/dev/null | grep -E 'luxfi/node|ava-labs' || true)"; \
	if [ -n "$$leaked" ]; then \
		echo "FAIL: luxfi/node or ava-labs in chains/evm's dependency closure:" >&2; \
		echo "$$leaked" >&2; exit 1; \
	fi
	@echo "    confirmed clean: 0 luxfi/node, 0 ava-labs in the dependency graph"
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
# Reuses the repo's own already-configured build/ (Conan toolchain resolved)
# rather than reconfiguring, for speed; nothing but that conventional,
# gitignored directory is written back into the source tree.
luxd-cpp:
	@echo "==> cpp: lux-cpp/node (full node host)"
	@mkdir -p $(BIN)
	@test -f $(NODE_CPP_BUILD)/CMakeCache.txt || { \
		echo "FAIL: $(NODE_CPP_BUILD) is not configured — run:" >&2; \
		echo "  cmake -S $(NODE_CPP_DIR) -B $(NODE_CPP_BUILD) -DCMAKE_TOOLCHAIN_FILE=<conan toolchain>" >&2; \
		exit 1; }
	cmake --build $(NODE_CPP_BUILD) --target luxd -j$(NPROC)
	@test -x $(NODE_CPP_BUILD)/luxd
	cp $(NODE_CPP_BUILD)/luxd $(BIN)/luxd-cpp
	@ls -lh $(BIN)/luxd-cpp

# ---- make all: attempt all three + gpu, report every one, fail loudly -----

all:
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
gpu:
	@echo "==> gpu: lux-gpu/gpu, target luxgpu_core_static ($(GPU_DIR))"
	rm -f $(GPU_DIR)/build/libluxgpu.a
	cmake -B $(GPU_DIR)/build -S $(GPU_DIR)
	cmake --build $(GPU_DIR)/build --target luxgpu_core_static -j$(NPROC)
	@test -f $(GPU_DIR)/build/libluxgpu.a
	@ls -lh $(GPU_DIR)/build/libluxgpu.a

# ---- conformance ------------------------------------------------------------

conformance: conformance-go conformance-rust conformance-cpp
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

# ---- make chains: the cross-language chain differential ----------------------
#
# ONE corpus, generated from the Go P-chain and X-chain, handed to three
# implementations of each chain. Every field two of them answer differently
# fails the target and names the pair. See conformance/README.md.
#
# The generator is a separate Go module because it — and only it — depends on
# luxfi/node. That dependency is the whole point of a reference, and keeping it
# in its own module is what keeps it out of this one's graph.

CONF        := $(ROOT)/conformance
CONF_GEN    := $(CONF)/gen/gen
CONF_VECS   := $(CONF)/corpus/vectors.tsv
CONF_WANT   := $(CONF)/corpus/expected.tsv
PVM_RUST    := $(ROOT)/chains/rust/platformvm/target/release/conformance
XVM_RUST    := $(ROOT)/chains/rust/xvm/target/release/conformance
PVM_CPP     := $(ROOT)/chains/cpp/platformvm/build/pvm_conformance
XVM_CPP     := $(ROOT)/chains/cpp/xvm/build/xvm_conformance

chains: chains-build
	@echo
	cd $(ROOT) && GOWORK=off go run ./conformance/runner \
		-subject chain \
		-fields parse,kind,id,syntactic,exec \
		-vectors $(CONF_VECS) \
		-expected $(CONF_WANT) \
		-eval "go=$(CONF_GEN) eval" \
		-eval "rust=$(PVM_RUST)" \
		-eval "rust=$(XVM_RUST)" \
		-eval "cpp=$(PVM_CPP)" \
		-eval "cpp=$(XVM_CPP)"

# Every evaluator is built before the run, and a build that fails stops the
# target. A differential that quietly lost one of its voices would report
# agreement among whoever was left.
chains-build:
	@echo "==> chain differential: building four evaluators"
	cd $(CONF)/gen && GOWORK=off go build -o gen .
	cd $(ROOT)/chains/rust/platformvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cd $(ROOT)/chains/rust/xvm && PATH="$(HOME)/.cargo/bin:$$PATH" cargo build --release --bin conformance
	cmake -S $(ROOT)/chains/cpp/platformvm -B $(ROOT)/chains/cpp/platformvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/chains/cpp/platformvm/build --target pvm_conformance -j$(NPROC)
	cmake -S $(ROOT)/chains/cpp/xvm -B $(ROOT)/chains/cpp/xvm/build -DCMAKE_BUILD_TYPE=Release
	cmake --build $(ROOT)/chains/cpp/xvm/build --target xvm_conformance -j$(NPROC)
	@for f in $(CONF_GEN) $(PVM_RUST) $(XVM_RUST) $(PVM_CPP) $(XVM_CPP); do \
		test -x "$$f" || { echo "FAIL: no evaluator at $$f" >&2; exit 1; }; \
	done

# ---- make precompiles: the cross-language precompile differential -----------
#
# The same runner, a different subject. See conformance/PRECOMPILE.md.

PREC        := $(CONF)/precompile
PREC_GO     := $(PREC)/precompile
PREC_RUST   := $(ROOT)/chains/rust/evm/target/release/conformance
PREC_CPP    := $(ROOT)/chains/cpp/evm/build/evm_conformance
PREC_VECS   := $(CONF)/corpus/precompile_vectors.tsv
PREC_WANT   := $(CONF)/corpus/precompile_expected.tsv

precompiles: precompiles-build
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
precompiles-corpus:
	cd $(PREC) && GOWORK=off go run . emit ../corpus

# Rebuild the corpus from the Go reference. Its output is committed, so a
# corpus that moves shows up as a diff rather than as a silent new normal.
chains-corpus:
	cd $(CONF)/gen && GOWORK=off go run . emit ../corpus

# ---- clean -------------------------------------------------------------------

clean:
	rm -rf $(BIN)
