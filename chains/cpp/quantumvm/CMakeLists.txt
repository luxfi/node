# Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# quantumvm — the Q-chain, in C++.
#
# Four reused trees, found where they actually sit, matched on a FILE rather
# than a directory name so a same-named sibling cannot be mistaken for one:
#
#   luxcpp/blst      the BLS12-381 backend a validator signature is checked on
#   luxcpp/pqclean   the ML-DSA (FIPS 204) reference body
#   luxcpp/crypto    the first-party ML-DSA surface over it, and SHA-512
#   lux-cpp/node     the VM seam this chain is registered through, and the
#   lux-cpp/consensus id type that seam is written in
#
# Every one of them is REQUIRED. A Q-chain that cannot check a post-quantum
# signature is not a Q-chain, and a chain that cannot check a validator's BLS
# signature finalizes on arithmetic rather than cryptography — so a build that
# cannot do either does not produce this chain at all. There is no mode in which
# a signature check is skipped.

cmake_minimum_required(VERSION 3.20)
project(lux_quantumvm LANGUAGES C CXX ASM)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(QUANTUMVM_SANITIZE "" CACHE STRING "Sanitizer for quantumvm code only (e.g. address,undefined)")

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

# node2 sits at ~/work/lux/node2; this file at chains/cpp/quantumvm. The C++
# node and its consensus checkout live one level up from the lux checkout, under
# lux-cpp, and blst/crypto/pqclean under luxcpp.
set(_up "${CMAKE_CURRENT_SOURCE_DIR}/../../..")

find_checkout(_luxcpp "blst/bindings/blst.h"
  "${_up}/../../luxcpp" "${_up}/../luxcpp" "${_up}/.." "$ENV{HOME}/work/luxcpp")
find_checkout(_node "include/lux/node/vm.hpp"
  "${NODE_DIR}" "${_up}/../../lux-cpp/node" "${_up}/../lux-cpp/node" "${_up}/../node"
  "$ENV{HOME}/work/lux-cpp/node")
find_checkout(_consensus "include/lux/consensus/quorum_cert_engine.hpp"
  "${CONSENSUS_DIR}" "${_up}/../../lux-cpp/consensus" "${_up}/../../luxcpp/consensus"
  "${_up}/../lux-cpp/consensus" "${_up}/../consensus" "$ENV{HOME}/work/lux-cpp/consensus"
  "$ENV{HOME}/work/luxcpp/consensus")

set(LUXCPP_ROOT   "${_luxcpp}"    CACHE PATH "luxcpp checkout root (blst, crypto, pqclean)")
set(NODE_DIR      "${_node}"      CACHE PATH "lux-cpp/node checkout (the VM seam)")
set(CONSENSUS_DIR "${_consensus}" CACHE PATH "lux-cpp/consensus checkout (the id type)")
set(BLST_DIR      "${LUXCPP_ROOT}/blst"   CACHE PATH "blst source tree")
set(CRYPTO_DIR    "${LUXCPP_ROOT}/crypto" CACHE PATH "luxcpp/crypto checkout (ML-DSA, SHA-512)")

find_checkout(_pqclean "crypto_sign/ml-dsa-65/clean/sign.c"
  "${PQCLEAN_DIR}" "${LUXCPP_ROOT}/pqclean" "${CRYPTO_DIR}/build/_deps/luxcpp_pqclean-src")
set(PQCLEAN_DIR "${_pqclean}" CACHE PATH "luxcpp/pqclean checkout (the ML-DSA body)")

foreach(_p "${BLST_DIR}/bindings/blst.h" "${BLST_DIR}/src/server.c" "${BLST_DIR}/build/assembly.S")
  if(NOT EXISTS "${_p}")
    message(FATAL_ERROR "quantumvm: blst is required and was not found: ${_p}\n"
                        "set -DLUXCPP_ROOT=/path/to/luxcpp")
  endif()
endforeach()

foreach(_p "${CRYPTO_DIR}/mldsa/cpp/mldsa.cpp" "${CRYPTO_DIR}/mldsa/cpp/mldsa.hpp"
           "${CRYPTO_DIR}/ed25519/cpp/sha512_minimal.hpp")
  if(NOT EXISTS "${_p}")
    message(FATAL_ERROR "quantumvm: the first-party crypto is required and was not found: ${_p}\n"
                        "set -DCRYPTO_DIR=/path/to/luxcpp/crypto")
  endif()
endforeach()

if(NOT PQCLEAN_DIR OR NOT EXISTS "${PQCLEAN_DIR}/common/fips202.c")
  message(FATAL_ERROR "quantumvm: the ML-DSA body (luxcpp/pqclean) is required and was not found.\n"
                      "  PQCLEAN_DIR=${PQCLEAN_DIR}\n"
                      "set -DPQCLEAN_DIR=/path/to/luxcpp/pqclean")
endif()

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

if(NOT TARGET blst)
  add_library(blst STATIC "${BLST_DIR}/src/server.c" "${BLST_DIR}/build/assembly.S")
  target_compile_options(blst PRIVATE -O2 -fno-builtin ${BLST_ISA_FLAGS})
  target_include_directories(blst PUBLIC "${BLST_DIR}/bindings")
endif()

# ML-DSA. The three parameter sets are compiled as SEPARATE object libraries,
# each with only its own directory on the include path: every set has a
# params.h of its own and they must not see each other's. The SHAKE body and
# the entropy hook are compiled ONCE and shared, because their symbols are not
# namespaced and three copies would not link.
if(NOT TARGET lux_qvm_mldsa)
  add_library(lux_qvm_mldsa_common OBJECT
    "${PQCLEAN_DIR}/common/fips202.c" "${PQCLEAN_DIR}/common/randombytes.c")
  target_include_directories(lux_qvm_mldsa_common PRIVATE "${PQCLEAN_DIR}/common")
  set_target_properties(lux_qvm_mldsa_common PROPERTIES C_STANDARD 99)

  foreach(_set 44 65 87)
    file(GLOB _srcs "${PQCLEAN_DIR}/crypto_sign/ml-dsa-${_set}/clean/*.c")
    add_library(lux_qvm_mldsa_${_set} OBJECT ${_srcs})
    target_include_directories(lux_qvm_mldsa_${_set} PRIVATE
      "${PQCLEAN_DIR}/crypto_sign/ml-dsa-${_set}/clean" "${PQCLEAN_DIR}/common")
    set_target_properties(lux_qvm_mldsa_${_set} PROPERTIES C_STANDARD 99)
  endforeach()

  add_library(lux_qvm_mldsa STATIC "${CRYPTO_DIR}/mldsa/cpp/mldsa.cpp"
    $<TARGET_OBJECTS:lux_qvm_mldsa_common>
    $<TARGET_OBJECTS:lux_qvm_mldsa_44>
    $<TARGET_OBJECTS:lux_qvm_mldsa_65>
    $<TARGET_OBJECTS:lux_qvm_mldsa_87>)
  target_include_directories(lux_qvm_mldsa PUBLIC
    "${CRYPTO_DIR}/mldsa/cpp" "${PQCLEAN_DIR}/crypto_sign")
  target_compile_options(lux_qvm_mldsa PRIVATE -O2)
endif()

add_library(lux_quantumvm STATIC
  src/error.cpp
  src/config.cpp
  src/signer.cpp
  src/wire.cpp
  src/transaction.cpp
  src/block.cpp
  src/store.cpp
  src/quasar.cpp
  src/vm.cpp
  src/service.cpp)
target_include_directories(lux_quantumvm PUBLIC include "${CRYPTO_DIR}/ed25519/cpp")
target_link_libraries(lux_quantumvm PUBLIC blst lux_qvm_mldsa)
target_compile_options(lux_quantumvm PRIVATE -Wall -Wextra)
find_package(Threads REQUIRED)
target_link_libraries(lux_quantumvm PUBLIC Threads::Threads)

# The VM seam is header-only; a chain that registers through it needs both the
# node's include tree and the consensus id it is written in.
if(NODE_DIR AND CONSENSUS_DIR)
  target_include_directories(lux_quantumvm PUBLIC "${NODE_DIR}/include" "${CONSENSUS_DIR}/include")
else()
  message(FATAL_ERROR
    "quantumvm: the node VM seam was not found.\n"
    "  NODE_DIR=${NODE_DIR}\n  CONSENSUS_DIR=${CONSENSUS_DIR}\n"
    "set -DNODE_DIR=/path/to/lux-cpp/node -DCONSENSUS_DIR=/path/to/lux-cpp/consensus")
endif()

if(QUANTUMVM_SANITIZE)
  target_compile_options(lux_quantumvm PRIVATE -fsanitize=${QUANTUMVM_SANITIZE} -fno-omit-frame-pointer)
  target_link_options(lux_quantumvm PUBLIC -fsanitize=${QUANTUMVM_SANITIZE})
endif()

option(QUANTUMVM_BUILD_TESTS "Build the ported Go test suite" ON)
if(QUANTUMVM_BUILD_TESTS)
  enable_testing()
  add_library(qvm_test_main STATIC test/harness.cpp)
  target_include_directories(qvm_test_main PUBLIC test include)
  target_link_libraries(qvm_test_main PUBLIC lux_quantumvm)

  foreach(_t config signer wire store transaction block genesis vm quasar edges parse service)
    add_executable(qvm_${_t}_test test/${_t}_test.cpp)
    target_link_libraries(qvm_${_t}_test PRIVATE qvm_test_main)
    target_compile_options(qvm_${_t}_test PRIVATE -Wall -Wextra)
    add_test(NAME qvm_${_t} COMMAND qvm_${_t}_test)
  endforeach()

  # The differential evaluator `make chains` runs: it reads the shared corpus
  # and PRINTS this chain's answers for every Q vector, and says nothing about
  # whether they are right — deciding that needs every implementation's answer,
  # which is the runner's job.
  #
  # Not a test, and built unconditionally below rather than under this block, so
  # a chain that stopped answering is a build failure rather than a silently
  # missing voice.

  # The OLDER evaluator, against the frozen JSON corpus. It is still here
  # because chains/rust reads that file too; when Rust moves to vectors.tsv both
  # it and the JSON go, and there is one corpus again.
  #
  # It is registered as a TEST as well, because the corpus carries Go's expected
  # id for every vector: an id this implementation derives differently is a
  # failure here rather than a line for a human to notice.
  add_executable(qvm_differential_test test/differential.cpp)
  target_link_libraries(qvm_differential_test PRIVATE lux_quantumvm)
  target_include_directories(qvm_differential_test PRIVATE test)
  target_compile_options(qvm_differential_test PRIVATE -Wall -Wextra)
  add_test(NAME qvm_differential COMMAND qvm_differential_test)

  # Where the corpus is, said once. The evaluator also searches upward from the
  # working directory and takes NODE2_CORPUS_PATH, so this only spares ctest the
  # search; a corpus that is not there is a failure, never a silent pass.
  set(QVM_CORPUS "${CMAKE_CURRENT_SOURCE_DIR}/../../../conformance/corpus/chain_differential.json")
  if(EXISTS "${QVM_CORPUS}")
    set_tests_properties(qvm_differential PROPERTIES ENVIRONMENT "NODE2_CORPUS_PATH=${QVM_CORPUS}")
  endif()
endif()

# ── the differential evaluator ───────────────────────────────────────────────
#
# Outside the tests block: a differential that quietly lost one of its voices
# would report agreement among whoever was left, so this is built whether or
# not tests are.
add_executable(qvm_conformance test/conformance.cpp)
target_link_libraries(qvm_conformance PRIVATE lux_quantumvm)
# The corpus format, the verdict words and the error-word table are shared with
# every other C++ evaluator, because a table that differed between two of them
# reported its own difference as a chain's.
target_include_directories(qvm_conformance PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/../conformance/include)
target_compile_options(qvm_conformance PRIVATE -Wall -Wextra)
