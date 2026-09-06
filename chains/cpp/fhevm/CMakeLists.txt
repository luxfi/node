# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# fhevm — the Lux F-Chain (the coordination plane for confidential compute) in
# C++, behind the node's VM seam.
#
# Layering, each layer below REUSED unmodified:
#
#   luxcpp/pqclean       the FIPS 204 ML-DSA reference body (CC0)
#   luxcpp/crypto/mldsa  the C++ surface over it — how F knows who asked
#   lux-cpp/consensus    the Id type the seam speaks (header-only here)
#   lux-cpp/node         include/lux/node/vm.hpp — the VM seam itself
#   chains/cpp/zap       the zero-copy structural codec (header-only)
#   ─────────────────────────── NEW in fhevm ───────────────────────────
#   id          identifiers, sha256, and the cb58/base64 renderings
#   json        JSON as Go's encoding/json does it — a consensus question here
#   error       the refusals, one enumerated value each
#   records     what the chain persists, and the derivations that name it
#   fee         metered gas, a balance ledger, and the burn
#   gas         what an operation costs, by operation and by FHE scheme
#   auth        ML-DSA-65 payer authentication
#   transaction the six operations and every rule each has to satisfy
#   wire        struct-is-wire over ZAP, canonical in both directions
#   store       where the state rests, and the one durability point
#   block       verify, accept, reject
#   vm          the seam implementation, the mempool, and the batch
#   service     the read-only public views
#
# Reused, NOT vendored. The checkouts are found where they actually sit;
# -DLUXCPP_CRYPTO_DIR / -DPQCLEAN_DIR / -DNODE_DIR / -DCONSENSUS_DIR override.

cmake_minimum_required(VERSION 3.20)
project(fhevm LANGUAGES C CXX)

set(CMAKE_C_STANDARD 99)
set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(FHEVM_SANITIZE "" CACHE STRING "Sanitizer for fhevm's own code (e.g. address,undefined)")

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

find_checkout(_crypto "mldsa/cpp/mldsa.hpp"
  "${LUXCPP_CRYPTO_DIR}"
  "$ENV{HOME}/work/luxcpp/crypto"
  "${CMAKE_SOURCE_DIR}/../../../../luxcpp/crypto")
find_checkout(_pqclean "crypto_sign/ml-dsa-65/clean/api.h"
  "${PQCLEAN_DIR}"
  "$ENV{HOME}/work/luxcpp/pqclean"
  "${CMAKE_SOURCE_DIR}/../../../../luxcpp/pqclean")
if(NOT _crypto OR NOT _pqclean)
  message(FATAL_ERROR
    "ML-DSA not found — crypto=${_crypto} pqclean=${_pqclean}. "
    "Pass -DLUXCPP_CRYPTO_DIR=/path/to/luxcpp/crypto -DPQCLEAN_DIR=/path/to/luxcpp/pqclean.")
endif()
message(STATUS "fhevm: ML-DSA from ${_crypto} over ${_pqclean}")

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
message(STATUS "fhevm: VM seam at ${_node}, Id from ${_consensus}")

# ── the reused ML-DSA, compiled in place (no vendoring, no submodule) ────────
file(GLOB _mldsa65_srcs ${_pqclean}/crypto_sign/ml-dsa-65/clean/*.c)
add_library(fhevm_mldsa STATIC
  ${_mldsa65_srcs}
  ${_pqclean}/common/fips202.c
  ${_pqclean}/common/randombytes.c
  ${_crypto}/mldsa/cpp/mldsa.cpp
)
target_include_directories(fhevm_mldsa PUBLIC
  ${_crypto}/mldsa/cpp
)
target_include_directories(fhevm_mldsa PRIVATE
  ${_pqclean}/crypto_sign
  ${_pqclean}/crypto_sign/ml-dsa-65/clean
  ${_pqclean}/common
)
# PQClean's ml-dsa-44/87 headers are included by the shared C++ surface, so the
# two other parameter sets come along; F itself only ever calls the 65 pair.
file(GLOB _mldsa44_srcs ${_pqclean}/crypto_sign/ml-dsa-44/clean/*.c)
file(GLOB _mldsa87_srcs ${_pqclean}/crypto_sign/ml-dsa-87/clean/*.c)
target_sources(fhevm_mldsa PRIVATE ${_mldsa44_srcs} ${_mldsa87_srcs})

# ── fhevm itself ─────────────────────────────────────────────────────────────
add_library(fhevm STATIC
  src/id.cpp
  src/error.cpp
  src/json.cpp
  src/records.cpp
  src/fee.cpp
  src/gas.cpp
  src/auth.cpp
  src/transaction.cpp
  src/wire.cpp
  src/store.cpp
  src/block.cpp
  src/vm.cpp
  src/service.cpp
)
target_include_directories(fhevm PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}/include
  ${CMAKE_CURRENT_SOURCE_DIR}/../zap/include
  ${_node}/include
  ${_consensus}/include
)
target_link_libraries(fhevm PUBLIC fhevm_mldsa)
target_compile_options(fhevm PRIVATE -Wall -Wextra)

# ── tests ────────────────────────────────────────────────────────────────────
#   id          the identifiers and every rendering, against Go's own strings
#   json        Go's decoder semantics: unknown fields, trailing content, arrays
#   wire        the bytes, the ids and the canonical-form refusals, against Go
#   records     what the chain persists, byte for byte against Go
#   gas         the schedule, and its relation to the admission floor
#   tx          syntactic verification, committee validation, effects
#   fhe         the operations F exposes and the threshold rules that guard them
#   vm          lifecycle, mempool, block build/verify/accept/reject
#   settlement  metering, burning, and what a revert still costs
#   fault       what a chain does when its own store fails
#   invariant   the ciphertext-body invariant, pinned field by field
#   replay      two independently-driven nodes, and the databases they agree on
#   service     the read-only views
#   differential every golden value the Go chain produced
enable_testing()
set(FHEVM_TESTS id json wire records gas tx fhe vm settlement fault invariant replay
                service differential)
foreach(_t ${FHEVM_TESTS})
  add_executable(${_t}_test test/${_t}_test.cpp)
  target_link_libraries(${_t}_test PRIVATE fhevm)
  target_include_directories(${_t}_test PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/test)
  # The invariant suite reads the package's own sources, so it is told where
  # they are rather than guessing from the working directory.
  target_compile_definitions(${_t}_test PRIVATE
    FHEVM_SOURCE_DIR="${CMAKE_CURRENT_SOURCE_DIR}")
  target_compile_options(${_t}_test PRIVATE -Wall -Wextra)
  add_test(NAME ${_t} COMMAND ${_t}_test)
endforeach()

# ── the differential evaluator ───────────────────────────────────────────────
#
# Not a test: it reads the shared corpus and PRINTS this chain's answers, and
# `make chains` compares them against the Go F-chain's. Built unconditionally,
# so a chain that stopped answering is a build failure rather than a silently
# missing voice.
add_executable(fhevm_conformance test/conformance.cpp)
target_link_libraries(fhevm_conformance PRIVATE fhevm)
# The corpus format, the verdict words and the error-word table are shared with
# every other C++ evaluator, because a table that differed between two of them
# reported its own difference as a chain's.
target_include_directories(fhevm_conformance PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/../conformance/include)
target_compile_options(fhevm_conformance PRIVATE -Wall -Wextra)

if(FHEVM_SANITIZE)
  set(_san -fsanitize=${FHEVM_SANITIZE} -fno-omit-frame-pointer)
  set(_targets fhevm fhevm_conformance)
  foreach(_t ${FHEVM_TESTS})
    list(APPEND _targets ${_t}_test)
  endforeach()
  foreach(_t ${_targets})
    target_compile_options(${_t} PRIVATE ${_san})
    target_link_options(${_t} PRIVATE ${_san})
  endforeach()
endif()
