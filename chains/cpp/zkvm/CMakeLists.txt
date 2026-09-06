# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# zkvm — the Lux Z-Chain (shielded UTXO settlement) in C++, behind the node's VM
# seam.
#
# Layering, each layer below REUSED unmodified:
#
#   luxcpp/crypto        sha256, and the first-party bn254 tower + optimal-ate
#                        pairing the classical proof path is judged by
#   lux-cpp/consensus    the Id type the seam speaks (header-only here)
#   lux-cpp/node         include/lux/node/vm.hpp — the VM seam itself
#   ─────────────────────────── NEW in zkvm ────────────────────────────
#   zap-proto/cpp  the ZAP protocol SDK (published, pinned in zap.cmake)
#   wire       the canonical/agreement rules every frame is held to
#   txs        the shielded transaction and the identity it is decided by
#   store      the durable map, and the view a decision writes through
#   utxo       the shielded outputs
#   nullifier  the spent set
#   root       the state root a validator signs
#   mempool    what is pending, and the order a proposer takes it in
#   starkfri   the strict-PQ STARK/FRI verification seam
#   groth16    the classical proof system's encoding and point discipline
#   verifier   the profile gate and the proof cache
#   chainstore how a decision becomes fact
#   block      the linear block
#   vertex     the DAG shape
#   vm         the seam implementation
#
# Reused, NOT vendored. The checkouts are found where they actually sit;
# -DLUXCPP_CRYPTO_DIR / -DNODE_DIR / -DCONSENSUS_DIR override.

cmake_minimum_required(VERSION 3.20)
project(zkvm LANGUAGES CXX)


# The wire this chain speaks comes from the published SDK, not from here.
include(${CMAKE_CURRENT_SOURCE_DIR}/../zap.cmake)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(ZKVM_SANITIZE "" CACHE STRING "Sanitizer for zkvm's own code (e.g. address,undefined)")

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

find_checkout(_crypto "bn254/cpp/bn254_pairing.hpp"
  "${LUXCPP_CRYPTO_DIR}"
  "$ENV{HOME}/work/luxcpp/crypto"
  "${CMAKE_SOURCE_DIR}/../../../../luxcpp/crypto")
if(NOT _crypto)
  message(FATAL_ERROR
    "luxcpp/crypto not found (looked for bn254/cpp/bn254_pairing.hpp). "
    "Pass -DLUXCPP_CRYPTO_DIR=/path/to/luxcpp/crypto.")
endif()
message(STATUS "zkvm: reusing crypto at ${_crypto}")

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
message(STATUS "zkvm: VM seam at ${_node}, Id from ${_consensus}")

# ── the reused crypto, compiled in place (no vendoring, no submodule) ────────
#
# sha256 is the chain's ONE hash. bn254 is the curve the classical proof path is
# judged on — and the ONLY compiled unit it needs, the rest of the tower being
# header-only.
add_library(zkvm_crypto STATIC
  ${_crypto}/sha256/cpp/sha256.cpp
  ${_crypto}/bn254/cpp/bn254_pairing.cpp
)
target_include_directories(zkvm_crypto PUBLIC
  ${_crypto}/include
  ${_crypto}/sha256/cpp
  ${_crypto}/bn254/cpp
)

# ── zkvm itself ──────────────────────────────────────────────────────────────
add_library(zkvm STATIC
  src/id.cpp
  src/txs.cpp
  src/store.cpp
  src/utxo.cpp
  src/nullifier.cpp
  src/root.cpp
  src/mempool.cpp
  src/starkfri.cpp
  src/groth16.cpp
  src/verifier.cpp
  src/chainstore.cpp
  src/block.cpp
  src/vertex.cpp
  src/vm.cpp
)
target_include_directories(zkvm PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}/include
  ${_node}/include
  ${_consensus}/include
)
target_link_libraries(zkvm PUBLIC zap::zap zkvm_crypto)
target_compile_options(zkvm PRIVATE -Wall -Wextra)
find_package(Threads REQUIRED)
target_link_libraries(zkvm PUBLIC Threads::Threads)

# ── tests ────────────────────────────────────────────────────────────────────
#   golden      the bytes, the ids and the roots, against the GO Z-Chain's own
#   wire        canonicality and the length/blob agreement rule
#   txs         the identity, and every field it commits to
#   groth16     point encodings, the field, and the pairing verdict
#   verifier    the classical path's refusals and its one acceptance
#   tamper      proofs that DECODE and are still not proofs of this statement —
#               the verdicts only the pairing can reach
#   strictpq    the profile gate: what a strict-PQ chain will and will not judge
#   starkfri    the strict-PQ seam: unbound refuses, bound decides
#   store       the durable log, the view, and what survives a restart
#   state       the spent set, the UTXO set and the root
#   mempool     admission, eviction, ordering and expiry
#   block       verify → accept → reject over the node's seam
#   vertex      the DAG shape, its conflicts and its wire
#   vm          the whole chain: genesis, build, verify, accept, restart
enable_testing()
set(ZKVM_TESTS golden wire txs groth16 verifier tamper strictpq starkfri store state mempool block
               vertex vm)
foreach(_t ${ZKVM_TESTS})
  add_executable(${_t}_test test/${_t}_test.cpp)
  target_link_libraries(${_t}_test PRIVATE zkvm)
  target_include_directories(${_t}_test PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/test)
  target_compile_options(${_t}_test PRIVATE -Wall -Wextra)
  add_test(NAME ${_t} COMMAND ${_t}_test)
endforeach()

# ── the differential evaluator ───────────────────────────────────────────────
#
# Not a test: it reads the shared corpus and PRINTS this chain's answers, and
# `make chains` compares them against the Go Z-chain's. It is built here so a
# chain that stopped answering is a build failure rather than a silently
# missing voice.
add_executable(zkvm_conformance test/conformance.cpp)
target_link_libraries(zkvm_conformance PRIVATE zkvm)
# The corpus format, the verdict words and the error-word table are shared with
# every other C++ evaluator, because a table that differed between two of them
# reported its own difference as a chain's.
target_include_directories(zkvm_conformance PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/../conformance/include)
target_compile_options(zkvm_conformance PRIVATE -Wall -Wextra)

if(ZKVM_SANITIZE)
  set(_san -fsanitize=${ZKVM_SANITIZE} -fno-omit-frame-pointer)
  set(_targets zkvm zkvm_conformance)
  foreach(_t ${ZKVM_TESTS})
    list(APPEND _targets ${_t}_test)
  endforeach()
  foreach(_t ${_targets})
    target_compile_options(${_t} PRIVATE ${_san})
    target_link_options(${_t} PRIVATE ${_san})
  endforeach()
endif()
