# Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# lux_gpu — the one place a C++ Lux chain asks for a primitive.
#
# This target compiles TWO things and no more:
#
#   - the seam itself: the CPU backend, the RFC 6962 fold, and a dlopen of an
#     installed kernel library. No kernel source is here, and none ever will be.
#   - the reused lux-private/crypto bodies the CPU backend delegates to. They were
#     compiled twice before this target existed, once into xvm and once into
#     platformvm; a primitive with two builds is a primitive with two places to
#     drift.
#
# Reused, NOT vendored. -DLUXCPP_CRYPTO_DIR overrides where the checkout sits.

cmake_minimum_required(VERSION 3.20)

# Usable two ways with one file: `add_subdirectory` from a chain, or configured
# on its own to build and run the seam's test.
if(CMAKE_CURRENT_SOURCE_DIR STREQUAL CMAKE_SOURCE_DIR)
  project(lux_gpu LANGUAGES C CXX)
  set(CMAKE_CXX_STANDARD 23)
  set(CMAKE_CXX_STANDARD_REQUIRED ON)
  set(CMAKE_CXX_EXTENSIONS OFF)
  if(NOT CMAKE_BUILD_TYPE)
    set(CMAKE_BUILD_TYPE Release)
  endif()
  enable_testing()
endif()

if(NOT TARGET lux_gpu)

  # find_checkout(VAR MARKER candidate...) — first candidate holding MARKER wins.
  # The marker is a FILE, not a directory name: names are ambiguous in a tree
  # that holds one implementation of a thing per language.
  if(NOT COMMAND lux_gpu_find_checkout)
    function(lux_gpu_find_checkout out marker)
      foreach(_root ${ARGN})
        get_filename_component(_abs "${_root}" ABSOLUTE)
        if(EXISTS "${_abs}/${marker}")
          set(${out} "${_abs}" PARENT_SCOPE)
          return()
        endif()
      endforeach()
      set(${out} "${out}-NOTFOUND" PARENT_SCOPE)
    endfunction()
  endif()

  lux_gpu_find_checkout(_lux_gpu_crypto "include/lux/crypto/secp256k1.h"
    "${LUXCPP_CRYPTO_DIR}"
    "$ENV{HOME}/work/lux-private/crypto"
    "$ENV{HOME}/work/luxcpp/crypto"
    "${CMAKE_CURRENT_LIST_DIR}/../../../../lux-private/crypto")
  if(NOT _lux_gpu_crypto)
    message(FATAL_ERROR
      "lux-private/crypto not found (looked for include/lux/crypto/secp256k1.h). "
      "Pass -DLUXCPP_CRYPTO_DIR=/path/to/lux-private/crypto.")
  endif()
  message(STATUS "lux_gpu: CPU backend reuses crypto at ${_lux_gpu_crypto}")

  add_library(lux_gpu STATIC
    ${CMAKE_CURRENT_LIST_DIR}/src/gpu.cpp
    ${CMAKE_CURRENT_LIST_DIR}/src/plugin.cpp
    ${_lux_gpu_crypto}/sha256/cpp/sha256.cpp
    ${_lux_gpu_crypto}/ripemd160/cpp/ripemd160.cpp
    ${_lux_gpu_crypto}/keccak/cpp/keccak.cpp
    ${_lux_gpu_crypto}/secp256k1/cpp/ecrecover.cpp
    ${_lux_gpu_crypto}/secp256k1/cpp/batch_inv.cpp
    ${_lux_gpu_crypto}/secp256k1/cpp/ecdsa.cpp
  )
  target_include_directories(lux_gpu PUBLIC
    ${CMAKE_CURRENT_LIST_DIR}/include
    ${_lux_gpu_crypto}/include
    ${_lux_gpu_crypto}/sha256/cpp
    ${_lux_gpu_crypto}/ripemd160/cpp
    ${_lux_gpu_crypto}/secp256k1/cpp
  )
  target_include_directories(lux_gpu PRIVATE ${CMAKE_CURRENT_LIST_DIR}/src)
  target_compile_features(lux_gpu PUBLIC cxx_std_23)
  target_compile_options(lux_gpu PRIVATE -Wall -Wextra)
  set_property(TARGET lux_gpu PROPERTY POSITION_INDEPENDENT_CODE ON)

  # dlopen. On glibc 2.34+ it is in libc and ${CMAKE_DL_LIBS} is empty, which is
  # why this is a variable and not a literal -ldl.
  target_link_libraries(lux_gpu PUBLIC ${CMAKE_DL_LIBS})

  # The seam's own test. Built whenever the seam is: a differential that is only
  # built when someone remembers to build it is a differential nobody runs.
  add_executable(lux_gpu_test ${CMAKE_CURRENT_LIST_DIR}/test/gpu_test.cpp)
  target_link_libraries(lux_gpu_test PRIVATE lux_gpu)
  target_compile_options(lux_gpu_test PRIVATE -Wall -Wextra)

endif()
