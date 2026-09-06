# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# dexvm — the Lux D-Chain (the real-assets-only admission layer) in C++, behind
# the node's VM seam.
#
# Layering, each layer below REUSED unmodified:
#
#   luxcpp/crypto        sha256
#   lux-cpp/consensus    the Id type the seam speaks (header-only here)
#   lux-cpp/node         include/lux/node/vm.hpp — the VM seam itself
#   ─────────────────────────── NEW in dexvm ───────────────────────────
#   id         the identifier, its two renderings, and the error identity
#   asset      the AssetID and MarketID derivations — the consensus bytes
#   registry   the admitted set: what enters, and through which door
#   forbidden  the deny half of admission
#   consensus  under which posture value may activate, and what it must say
#   gate       the single fail-closed boot gate
#   json       the manifest's encoding, Go-byte-identical
#   manifest   the per-network declaration, and how it becomes a live registry
#   embedded   the committed manifests, carried in the binary
#   runtime    the node-side verifier: manifest ids bound to running ids
#   zap        the zero-copy structural codec (header-only)
#   store      the durable log the set rests on
#   tx/state   what a block carries, and what executing it produces
#   vm         the seam implementation
#
# Reused, NOT vendored. The checkouts are found where they actually sit;
# -DLUXCPP_CRYPTO_DIR / -DNODE_DIR / -DCONSENSUS_DIR override.

cmake_minimum_required(VERSION 3.20)
project(dexvm LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(DEXVM_SANITIZE "" CACHE STRING "Sanitizer for dexvm's own code (e.g. address,undefined)")

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
message(STATUS "dexvm: reusing crypto at ${_crypto}")

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
message(STATUS "dexvm: VM seam at ${_node}, Id from ${_consensus}")

# ── the committed manifests, carried into the binary ─────────────────────────
# The .json files stay the single source of truth: the build reads their EXACT
# bytes and writes them out as hex, so the binary cannot paraphrase what CI
# approved and no second copy exists to drift.
set(_gen "${CMAKE_CURRENT_BINARY_DIR}/generated")
file(MAKE_DIRECTORY "${_gen}")
set(_manifest_header "${_gen}/manifest_bytes.hpp")
# Accumulated as ONE string, never a list: a C++ statement ends in a semicolon,
# and a semicolon is CMake's list separator, so a list would silently split each
# declaration in half.
set(_manifest_text
  "// Generated from manifests/*.json at configure time. Do not edit.\n#pragma once\n\nnamespace lux::dexvm::generated {\n")
foreach(_net mainnet testnet devnet)
  set(_src "${CMAKE_CURRENT_SOURCE_DIR}/manifests/assets.${_net}.json")
  if(NOT EXISTS "${_src}")
    message(FATAL_ERROR "dexvm: the committed manifest ${_src} is missing.")
  endif()
  file(READ "${_src}" _hex HEX)
  string(TOUPPER "${_net}" _NET)
  string(SUBSTRING "${_NET}" 0 1 _first)
  string(SUBSTRING "${_net}" 1 -1 _rest)
  string(APPEND _manifest_text
    "\ninline constexpr const char* k${_first}${_rest}ManifestHex =\n    \"${_hex}\";\n")
  set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS "${_src}")
endforeach()
string(APPEND _manifest_text "\n}  // namespace lux::dexvm::generated\n")
file(WRITE "${_manifest_header}" "${_manifest_text}")

# ── the reused crypto, compiled in place (no vendoring, no submodule) ────────
add_library(dexvm_crypto STATIC ${_crypto}/sha256/cpp/sha256.cpp)
target_include_directories(dexvm_crypto PUBLIC
  ${_crypto}/include
  ${_crypto}/sha256/cpp
)

# ── dexvm itself ─────────────────────────────────────────────────────────────
add_library(dexvm STATIC
  src/id.cpp
  src/asset.cpp
  src/forbidden.cpp
  src/registry.cpp
  src/consensus_mode.cpp
  src/gate.cpp
  src/json.cpp
  src/manifest.cpp
  src/embedded.cpp
  src/runtime_verifier.cpp
  src/store.cpp
  src/tx.cpp
  src/state.cpp
  src/vm.cpp
)
target_include_directories(dexvm PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}/include
  ${_gen}
  ${_node}/include
  ${_consensus}/include
)
target_link_libraries(dexvm PUBLIC dexvm_crypto)
target_compile_options(dexvm PRIVATE -Wall -Wextra)

# ── tests ────────────────────────────────────────────────────────────────────
#   golden      the AssetID/MarketID bytes, against Go's own and dex's KAT
#   registry    the four admission properties, ported case for case
#   gate        the value guard and every boot-gate refusal
#   manifest    load, shape, unknown field, and the wrong-chain refusal
#   pin         the content-hash binding, and what an edited file does
#   runtime     the node-side verifier's identity binding
#   json        the encoding, byte for byte against Go's MarshalIndent
#   embedded    the committed manifests, and localnet's synthesised one
#   store       the durable log, and what survives a restart
#   vm          build → verify → accept → REJECT over the node's seam
enable_testing()
set(DEXVM_TESTS golden registry gate manifest pin runtime json embedded store vm)
foreach(_t ${DEXVM_TESTS})
  add_executable(${_t}_test test/${_t}_test.cpp)
  target_link_libraries(${_t}_test PRIVATE dexvm)
  target_include_directories(${_t}_test PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/test)
  target_compile_options(${_t}_test PRIVATE -Wall -Wextra)
  add_test(NAME ${_t} COMMAND ${_t}_test)
endforeach()

# ── the differential evaluator ───────────────────────────────────────────────
#
# Not a test: it reads the shared corpus and PRINTS this chain's answers, and
# `make chains` compares them against the Go D-chain's. Built unconditionally,
# so a chain that stopped answering is a build failure rather than a silently
# missing voice.
add_executable(dexvm_conformance test/conformance.cpp)
target_link_libraries(dexvm_conformance PRIVATE dexvm)
# The corpus format, the verdict words and the error-word table are shared with
# every other C++ evaluator, because a table that differed between two of them
# reported its own difference as a chain's.
target_include_directories(dexvm_conformance PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/../conformance/include)
target_compile_options(dexvm_conformance PRIVATE -Wall -Wextra)

if(DEXVM_SANITIZE)
  set(_san -fsanitize=${DEXVM_SANITIZE} -fno-omit-frame-pointer)
  set(_targets dexvm dexvm_conformance)
  foreach(_t ${DEXVM_TESTS})
    list(APPEND _targets ${_t}_test)
  endforeach()
  foreach(_t ${_targets})
    target_compile_options(${_t} PRIVATE ${_san})
    target_link_options(${_t} PRIVATE ${_san})
  endforeach()
endif()
