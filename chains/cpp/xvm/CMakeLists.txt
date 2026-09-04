# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# xvm — the Lux X-Chain (the UTXO DAG and asset transfers) in C++, behind the
# node's VM seam.
#
# Layering, each layer below REUSED unmodified:
#
#   luxcpp/crypto        sha256, ripemd160, keccak256, secp256k1 (recover+sign)
#   lux-cpp/consensus    the Id type the seam speaks (header-only here)
#   lux-cpp/node         include/lux/node/vm.hpp — the VM seam itself
#   ─────────────────────────── NEW in xvm ────────────────────────────
#   zap        the zero-copy structural codec (header-only)
#   wire       the (TypeKind, ShapeKind) fx envelopes
#   fx         the fx primitives and the three fx families
#   txs        the five transactions and the UTXO vocabulary
#   state      the occupied set, and a diff over it
#   root       the execution root a validator signs
#   executor   syntax → semantics → apply
#   block      the block
#   vm         the seam implementation
#
# Reused, NOT vendored. The checkouts are found where they actually sit;
# -DLUXCPP_CRYPTO_DIR / -DNODE_DIR / -DCONSENSUS_DIR override.

cmake_minimum_required(VERSION 3.20)
project(xvm LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(XVM_SANITIZE "" CACHE STRING "Sanitizer for xvm's own code (e.g. address,undefined)")

# find_checkout(VAR MARKER candidate...) — first candidate holding MARKER wins.
# The marker is a FILE, not a directory name, because names are ambiguous here:
# `consensus` beside node in the lux checkout is the GO implementation, and only
# the header tells the two apart.
function(find_checkout out marker)
  foreach(_root ${ARGN})
    get_filename_component(_abs "${_root}" ABSOLUTE)
    if(EXISTS "${_abs}/${marker}")
      set(${out} "${_abs}" PARENT_SCOPE)
      return()
    endif()
  endforeach()
  set(${out} "${out}-NOTFOUND" PARENT_SCOPE)
endfunction()

find_checkout(_crypto "include/lux/crypto/secp256k1.h"
  "${LUXCPP_CRYPTO_DIR}"
  "$ENV{HOME}/work/luxcpp/crypto"
  "${CMAKE_SOURCE_DIR}/../../../../luxcpp/crypto")
if(NOT _crypto)
  message(FATAL_ERROR
    "luxcpp/crypto not found (looked for include/lux/crypto/secp256k1.h). "
    "Pass -DLUXCPP_CRYPTO_DIR=/path/to/luxcpp/crypto.")
endif()
message(STATUS "xvm: reusing crypto at ${_crypto}")

find_checkout(_node "include/lux/node/vm.hpp"
  "${NODE_DIR}"
  "$ENV{HOME}/work/lux-cpp/node"
  "${CMAKE_SOURCE_DIR}/../../../../lux-cpp/node")
find_checkout(_consensus "include/lux/consensus/quorum_cert_engine.hpp"
  "${CONSENSUS_DIR}"
  "$ENV{HOME}/work/lux-cpp/consensus"
  "${CMAKE_SOURCE_DIR}/../../../../lux-cpp/consensus")

# The seam is the point of the port, so its absence is a hard error rather than
# a quiet feature-off: a chain built against no seam is a library nothing runs.
if(NOT _node OR NOT _consensus)
  message(FATAL_ERROR
    "the VM seam was not found — node=${_node} consensus=${_consensus}. "
    "Pass -DNODE_DIR=/path/to/lux-cpp/node -DCONSENSUS_DIR=/path/to/lux-cpp/consensus.")
endif()
message(STATUS "xvm: VM seam at ${_node}, Id from ${_consensus}")

# ── the reused crypto, compiled in place (no vendoring, no submodule) ────────
add_library(xvm_crypto STATIC
  ${_crypto}/sha256/cpp/sha256.cpp
  ${_crypto}/ripemd160/cpp/ripemd160.cpp
  ${_crypto}/keccak/cpp/keccak.cpp
  ${_crypto}/secp256k1/cpp/ecrecover.cpp
  ${_crypto}/secp256k1/cpp/batch_inv.cpp
  ${_crypto}/secp256k1/cpp/ecdsa.cpp
)
target_include_directories(xvm_crypto PUBLIC
  ${_crypto}/include
  ${_crypto}/sha256/cpp
  ${_crypto}/ripemd160/cpp
  ${_crypto}/secp256k1/cpp
)

# ── xvm itself ───────────────────────────────────────────────────────────────
add_library(xvm STATIC
  src/id.cpp
  src/wire.cpp
  src/fx.cpp
  src/txs.cpp
  src/state.cpp
  src/root.cpp
  src/executor.cpp
  src/block.cpp
  src/genesis.cpp
  src/vm.cpp
)
target_include_directories(xvm PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}/include
  ${_node}/include
  ${_consensus}/include
)
target_link_libraries(xvm PUBLIC xvm_crypto)
target_compile_options(xvm PRIVATE -Wall -Wextra)

# ── tests ────────────────────────────────────────────────────────────────────
#   wire        the bytes, against the Go X-Chain's own (golden.hpp)
#   root        the execution root, against the cross-language KAT
#   txs         the tx vocabulary: verify, sort, round-trip
#   fx          the three fx families' spend and operation gates
#   syntactic   every syntactic rejection, ported case for case
#   semantic    the chain's own agreement, incl. import/export
#   executor    what applying a tx does to the UTXO set
#   state       the diff, and what acceptance means
#   block       block bytes, ids, and parsing
#   vm          build → verify → accept over the node's seam
enable_testing()
set(XVM_TESTS wire root syntactic txs fx executor state block genesis semantic vm)
foreach(_t ${XVM_TESTS})
  add_executable(${_t}_test test/${_t}_test.cpp)
  target_link_libraries(${_t}_test PRIVATE xvm)
  target_include_directories(${_t}_test PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/test)
  target_compile_options(${_t}_test PRIVATE -Wall -Wextra)
  add_test(NAME ${_t} COMMAND ${_t}_test)
endforeach()

if(XVM_SANITIZE)
  set(_san -fsanitize=${XVM_SANITIZE} -fno-omit-frame-pointer)
  set(_targets xvm)
  foreach(_t ${XVM_TESTS})
    list(APPEND _targets ${_t}_test)
  endforeach()
  foreach(_t ${_targets})
    target_compile_options(${_t} PRIVATE ${_san})
    target_link_options(${_t} PRIVATE ${_san})
  endforeach()
endif()
