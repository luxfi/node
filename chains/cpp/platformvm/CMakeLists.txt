# Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# platformvm — the P-chain, in C++.
#
# Two reused trees, found where they actually sit, matched on a FILE rather than
# a directory name so a same-named sibling cannot be mistaken for one:
#
#   luxcpp/blst        the BLS12-381 backend a proof of possession is checked on
#   lux-cpp/node       the VM seam this chain is registered through, and the
#   lux-cpp/consensus  id type that seam is written in
#
# blst is REQUIRED. A validator set with no admission control is what makes this
# chain a public good, and admission is a real pairing — so a build that cannot
# perform one does not produce a P-chain at all. There is no mode in which the
# proof check is skipped.

cmake_minimum_required(VERSION 3.20)
project(lux_platformvm LANGUAGES C CXX ASM)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(PLATFORMVM_SANITIZE "" CACHE STRING "Sanitizer for platformvm code only (e.g. address,undefined)")

# find_checkout(VAR MARKER candidate...) — first candidate holding MARKER wins.
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

# node2 sits at ~/work/lux/node2; this file at chains/cpp/platformvm. The C++
# node and its consensus checkout live one level up from the lux checkout, under
# lux-cpp, and blst under luxcpp.
set(_up "${CMAKE_CURRENT_SOURCE_DIR}/../../..")

find_checkout(_luxcpp "blst/bindings/blst.h"
  "${_up}/../../luxcpp" "${_up}/../luxcpp" "${_up}/..")
find_checkout(_node "include/lux/node/vm.hpp"
  "${_up}/../../lux-cpp/node" "${_up}/../lux-cpp/node" "${_up}/../node")
find_checkout(_consensus "include/lux/consensus/quorum_cert_engine.hpp"
  "${_up}/../../lux-cpp/consensus" "${_up}/../../luxcpp/consensus" "${_up}/../lux-cpp/consensus"
  "${_up}/../consensus")

set(LUXCPP_ROOT   "${_luxcpp}"    CACHE PATH "luxcpp checkout root (blst)")
set(NODE_DIR      "${_node}"      CACHE PATH "lux-cpp/node checkout (the VM seam)")
set(CONSENSUS_DIR "${_consensus}" CACHE PATH "lux-cpp/consensus checkout (the id type)")
set(BLST_DIR      "${LUXCPP_ROOT}/blst" CACHE PATH "blst source tree")

# The first-party secp256k1 recovery and ripemd160 live in the same luxcpp
# checkout as blst. They are what makes the fx signature check real: an address
# is ripemd160(sha256(compressed recovered key)), so both are REQUIRED for the
# same reason blst is — a chain that cannot check who signed does not have a
# spending rule.
set(CRYPTO_DIR "${LUXCPP_ROOT}/crypto" CACHE PATH "luxcpp/crypto checkout (secp256k1, ripemd160)")

foreach(_p "${BLST_DIR}/bindings/blst.h" "${BLST_DIR}/src/server.c" "${BLST_DIR}/build/assembly.S")
  if(NOT EXISTS "${_p}")
    message(FATAL_ERROR "platformvm: blst is required and was not found: ${_p}\n"
                        "set -DLUXCPP_ROOT=/path/to/luxcpp")
  endif()
endforeach()

# blst ISA selection, mirroring blst/build.sh: without -D__ADX__ every field
# multiplication takes the slower generic path on a host that has adx.
set(BLST_ISA "auto" CACHE STRING "blst ISA: auto | adx | portable | generic")
set(BLST_ISA_FLAGS "")
if(BLST_ISA STREQUAL "portable")
  set(BLST_ISA_FLAGS -D__BLST_PORTABLE__)
elseif(BLST_ISA STREQUAL "adx")
  set(BLST_ISA_FLAGS -D__ADX__)
elseif(BLST_ISA STREQUAL "auto")
  if(EXISTS /proc/cpuinfo)
    file(READ /proc/cpuinfo _cpuinfo)
    if(_cpuinfo MATCHES "[ \t]adx[ \t\n]")
      set(BLST_ISA_FLAGS -D__ADX__)
    endif()
  endif()
endif()

# keccak comes along because the recovery's batch pipeline composes an EVM
# address from a recovered key; the P-chain never asks for that address, but the
# symbol is in the same translation unit.
foreach(_p "${CRYPTO_DIR}/secp256k1/cpp/ecrecover.cpp" "${CRYPTO_DIR}/ripemd160/cpp/ripemd160.cpp"
           "${CRYPTO_DIR}/keccak/cpp/keccak.cpp" "${CRYPTO_DIR}/include/lux/crypto/secp256k1.h")
  if(NOT EXISTS "${_p}")
    message(FATAL_ERROR "platformvm: the first-party crypto is required and was not found: ${_p}\n"
                        "set -DCRYPTO_DIR=/path/to/luxcpp/crypto")
  endif()
endforeach()

if(NOT TARGET lux_pvm_crypto)
  add_library(lux_pvm_crypto STATIC
    "${CRYPTO_DIR}/secp256k1/cpp/ecrecover.cpp"
    "${CRYPTO_DIR}/secp256k1/cpp/batch_inv.cpp"
    "${CRYPTO_DIR}/secp256k1/cpp/ecdsa.cpp"
    "${CRYPTO_DIR}/ripemd160/cpp/ripemd160.cpp"
    "${CRYPTO_DIR}/keccak/cpp/keccak.cpp")
  target_include_directories(lux_pvm_crypto PUBLIC
    "${CRYPTO_DIR}/include" "${CRYPTO_DIR}/secp256k1/cpp" "${CRYPTO_DIR}/ripemd160/cpp"
    "${CRYPTO_DIR}/keccak/cpp")
  target_compile_options(lux_pvm_crypto PRIVATE -O2)
endif()

if(NOT TARGET blst)
  add_library(blst STATIC "${BLST_DIR}/src/server.c" "${BLST_DIR}/build/assembly.S")
  target_compile_options(blst PRIVATE -O2 -fno-builtin ${BLST_ISA_FLAGS})
  target_include_directories(blst PUBLIC "${BLST_DIR}/bindings")
endif()

add_library(lux_platformvm STATIC
  src/error.cpp
  src/signer.cpp
  src/components.cpp
  src/txs_wire.cpp
  src/txs.cpp
  src/txs_parse.cpp
  src/block.cpp
  src/state.cpp
  src/fx.cpp
  src/flow.cpp
  src/executor.cpp
  src/vm.cpp
  src/complexity.cpp
  src/validators.cpp
  src/warp.cpp
  src/warpmsg.cpp
  src/l1.cpp
  src/genesis.cpp
  src/staking.cpp)
target_include_directories(lux_platformvm PUBLIC include)
target_link_libraries(lux_platformvm PUBLIC blst lux_pvm_crypto)
# -Wpedantic is deliberately absent: the wide integer the reward curve needs is
# __int128, a compiler extension every target this ships on has.
target_compile_options(lux_platformvm PRIVATE -Wall -Wextra)

# The VM seam is header-only; a chain that registers through it needs both the
# node's include tree and the consensus id it is written in.
if(NODE_DIR AND CONSENSUS_DIR)
  target_include_directories(lux_platformvm PUBLIC "${NODE_DIR}/include" "${CONSENSUS_DIR}/include")
  target_compile_definitions(lux_platformvm PUBLIC LUX_PLATFORMVM_HAS_NODE_SEAM=1)
else()
  message(FATAL_ERROR
    "platformvm: the node VM seam was not found.\n"
    "  NODE_DIR=${NODE_DIR}\n  CONSENSUS_DIR=${CONSENSUS_DIR}\n"
    "set -DNODE_DIR=/path/to/lux-cpp/node -DCONSENSUS_DIR=/path/to/lux-cpp/consensus")
endif()

if(PLATFORMVM_SANITIZE)
  target_compile_options(lux_platformvm PRIVATE -fsanitize=${PLATFORMVM_SANITIZE} -fno-omit-frame-pointer)
  target_link_options(lux_platformvm PUBLIC -fsanitize=${PLATFORMVM_SANITIZE})
endif()

# The node's own validator-set commitment, compiled here so the test suite can
# check this chain's answer against the NODE's rather than against itself. Two
# implementations of one hash that only agree with themselves is exactly the
# failure the commitment exists to prevent.
if(NOT TARGET lux_node_validators AND EXISTS "${NODE_DIR}/src/validators.cpp"
   AND EXISTS "${CRYPTO_DIR}/sha256/cpp/sha256.cpp")
  add_library(lux_node_validators STATIC
    "${NODE_DIR}/src/validators.cpp" "${CRYPTO_DIR}/sha256/cpp/sha256.cpp")
  target_include_directories(lux_node_validators PUBLIC
    "${NODE_DIR}/include" "${CONSENSUS_DIR}/include" "${CRYPTO_DIR}")
  target_compile_options(lux_node_validators PRIVATE -O2)
endif()

option(PLATFORMVM_BUILD_TESTS "Build the ported Go test suite" ON)
if(PLATFORMVM_BUILD_TESTS)
  enable_testing()
  add_library(pvm_test_main STATIC test/harness.cpp)
  target_include_directories(pvm_test_main PUBLIC test include "${CRYPTO_DIR}/secp256k1/cpp" "${BLST_DIR}/bindings")
  target_link_libraries(pvm_test_main PUBLIC lux_platformvm)
  if(TARGET lux_node_validators)
    target_link_libraries(pvm_test_main PUBLIC lux_node_validators)
    target_compile_definitions(pvm_test_main PUBLIC LUX_PLATFORMVM_HAS_NODE_SET_ROOT=1)
  endif()

  foreach(_t zap sha256 reward components txs syntactic block state flow fee warp genesis staking executor l1 validators vm)
    add_executable(pvm_${_t}_test test/${_t}_test.cpp)
    target_link_libraries(pvm_${_t}_test PRIVATE pvm_test_main)
    target_compile_options(pvm_${_t}_test PRIVATE -Wall -Wextra)
    add_test(NAME pvm_${_t} COMMAND pvm_${_t}_test)
  endforeach()
endif()
