# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# core — what every chain in this node shares, and shares ONE of.
#
#   id      the fixed-width names, and the hashes that derive them
#   zap     the zero-copy structural codec: the only serialization here
#   store   the durable byte-keyed map the state rests on
#
# A chain adds this directory and links `lux_core`. Nothing in it knows what a
# chain is, which is the test of whether it belongs here: a P-chain concept
# that leaked in would make the X-chain link the P-chain's opinions.
#
# It carries the two hashes a NAME is made of (sha256, ripemd160) so that a
# binary compiles them once. A chain that also needs signature recovery
# compiles that itself — it is a different question, asked by fewer callers.

cmake_minimum_required(VERSION 3.20)

if(NOT TARGET lux_core)
  project(lux_core LANGUAGES C CXX)

  set(CMAKE_CXX_STANDARD 23)
  set(CMAKE_CXX_STANDARD_REQUIRED ON)
  set(CMAKE_CXX_EXTENSIONS OFF)
  set(CMAKE_POSITION_INDEPENDENT_CODE ON)

  # find_checkout(VAR MARKER candidate...) — first candidate holding MARKER
  # wins. The marker is a FILE, not a directory name, because names are
  # ambiguous here and only the header tells two same-named trees apart.
  function(lux_core_find out marker)
    foreach(_root ${ARGN})
      get_filename_component(_abs "${_root}" ABSOLUTE)
      if(EXISTS "${_abs}/${marker}")
        set(${out} "${_abs}" PARENT_SCOPE)
        return()
      endif()
    endforeach()
    set(${out} "${out}-NOTFOUND" PARENT_SCOPE)
  endfunction()

  lux_core_find(_crypto "include/lux/crypto/secp256k1.h"
    "${LUXCPP_CRYPTO_DIR}"
    "${CMAKE_CURRENT_LIST_DIR}/../../../../luxcpp/crypto"
    "$ENV{HOME}/work/luxcpp/crypto")
  if(NOT _crypto)
    message(FATAL_ERROR
      "core: luxcpp/crypto not found (looked for include/lux/crypto/secp256k1.h). "
      "Pass -DLUXCPP_CRYPTO_DIR=/path/to/luxcpp/crypto.")
  endif()
  message(STATUS "core: hashes from ${_crypto}")

  add_library(lux_core STATIC
    ${CMAKE_CURRENT_LIST_DIR}/src/id.cpp
    ${CMAKE_CURRENT_LIST_DIR}/src/store.cpp
    ${_crypto}/sha256/cpp/sha256.cpp
    ${_crypto}/ripemd160/cpp/ripemd160.cpp
  )
  target_include_directories(lux_core PUBLIC
    ${CMAKE_CURRENT_LIST_DIR}/include
    ${_crypto}/include
    ${_crypto}/sha256/cpp
    ${_crypto}/ripemd160/cpp
  )
  target_compile_options(lux_core PRIVATE -Wall -Wextra)
  set(LUX_CORE_CRYPTO_DIR "${_crypto}" CACHE INTERNAL "luxcpp/crypto used by lux_core")

  # The store's own suite, built once and registered with whichever chain build
  # added this directory. The store is the thing a chain trusts to remember
  # what it accepted, so it is proven wherever it is compiled rather than in
  # one place a chain build could skip.
  option(LUX_CORE_TESTS "Build core's own tests" ON)
  if(LUX_CORE_TESTS)
    enable_testing()
    foreach(_t store id mempool)
      add_executable(core_${_t}_test ${CMAKE_CURRENT_LIST_DIR}/test/${_t}_test.cpp)
      target_link_libraries(core_${_t}_test PRIVATE lux_core)
      target_compile_options(core_${_t}_test PRIVATE -Wall -Wextra)
      add_test(NAME core_${_t} COMMAND core_${_t}_test)
    endforeach()

    # The durability proof. It is a separate target because it is not a test of
    # a function: it writes, has the kernel KILL the process mid-life, starts a
    # new one, and demands the same bytes back. See the source.
    add_executable(core_durability_test ${CMAKE_CURRENT_LIST_DIR}/test/durability_test.cpp)
    target_link_libraries(core_durability_test PRIVATE lux_core)
    target_compile_options(core_durability_test PRIVATE -Wall -Wextra)
    add_test(NAME core_durability COMMAND core_durability_test)
  endif()
endif()
