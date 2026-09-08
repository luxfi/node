# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# oraclevm — the Lux O-Chain (the oracle plane) in C++.
#
# The canonical implementation is github.com/luxfi/oracle/vm, 1628 lines of Go;
# chains/oraclevm is a 213-line re-export of it and is not the chain.
#
# WHAT IS COMPILED HERE, and what is REACHED rather than copied:
#
#   ../fhevm/src/id.cpp    sha256, cb58, base64, and the identifier renderings
#   ../fhevm/src/json.cpp  JSON as Go's encoding/json does it
#   ─────────────────────────── NEW in oraclevm ───────────────────────────
#   src/gotime.cpp   an instant read and written the way time.Time is
#   src/chain.cpp    the block wire, the request id, the Merkle root over a
#                    request's records, and the three rules this chain has
#
# The two reused files are chain-neutral — Go's decoder and the cb58 alphabet
# know nothing about F — and they have already been held against the Go
# reference by the F-chain's own rows in this same differential, which is the
# whole reason to reach for them instead of writing a second opinion about what
# encoding/json accepts. That they still sit under the F-chain's name is a
# naming debt, not a dependency: the right home is a shared one and moving them
# is a rename across fifteen of that chain's files.
#
# NOTHING ELSE IS LINKED. No ZAP, no post-quantum library, no external
# checkout: the O-chain's wire is JSON and its only primitive is SHA-256, so
# this target builds from this tree alone.

cmake_minimum_required(VERSION 3.20)
project(oraclevm LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

# oraclevm compiles two of fhevm's sources directly. fhevm lives in the
# compute repository; it is looked for beside this one first, so a tree that
# still carries it in place keeps working, and FHEVM_DIR overrides both.
if(NOT FHEVM_DIR)
  foreach(candidate
      ${CMAKE_CURRENT_SOURCE_DIR}/../fhevm
      ${CMAKE_CURRENT_SOURCE_DIR}/../../../../compute/chains/cpp/fhevm)
    if(EXISTS ${candidate}/src/json.cpp)
      set(FHEVM_DIR ${candidate})
      break()
    endif()
  endforeach()
endif()
if(NOT FHEVM_DIR OR NOT EXISTS ${FHEVM_DIR}/src/json.cpp)
  message(FATAL_ERROR "oraclevm reads Go's JSON through fhevm's src/json.cpp, which was not found beside this directory or in the compute repository; pass -DFHEVM_DIR=<path> to say where it is")
endif()

add_library(oraclevm STATIC
  ${FHEVM_DIR}/src/id.cpp
  ${FHEVM_DIR}/src/json.cpp
  ${CMAKE_CURRENT_SOURCE_DIR}/src/gotime.cpp
  ${CMAKE_CURRENT_SOURCE_DIR}/src/chain.cpp
)
target_include_directories(oraclevm PUBLIC
  ${CMAKE_CURRENT_SOURCE_DIR}/include
  ${FHEVM_DIR}/include
)
target_compile_options(oraclevm PRIVATE -Wall -Wextra)

add_executable(oraclevm_conformance ${CMAKE_CURRENT_SOURCE_DIR}/test/conformance.cpp)
target_link_libraries(oraclevm_conformance PRIVATE oraclevm)
target_compile_options(oraclevm_conformance PRIVATE -Wall -Wextra)
