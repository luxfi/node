# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# core — what every chain in this node shares, and shares ONE of.
#
#   id      the fixed-width names, and the hashes that derive them
#   store   the durable byte-keyed map the state rests on
#
# The codec is NOT here. ZAP is reached as the published package every chain in
# this tree already links (../zap.cmake), so the store writes its rows with the
# same reader a chain parses a block with. A copy kept here would be a second
# opinion about what a frame is, inside one process.
#
# A chain adds this directory and links `lux_core`. Nothing in it knows what a
# chain is, which is the test of whether it belongs here: a P-chain concept
# that leaked in would make the X-chain link the P-chain's opinions.
#
# The hashes a NAME is made of are not compiled here either. Every primitive
# comes from the one seam a chain asks through (gpu/cpp), which is also where
# the choice between computing one and handing it to an installed kernel
# library is made. Core links it, so a chain that links core has the seam.

cmake_minimum_required(VERSION 3.20)

if(NOT TARGET lux_core)
  project(lux_core LANGUAGES C CXX)

  set(CMAKE_CXX_STANDARD 23)
  set(CMAKE_CXX_STANDARD_REQUIRED ON)
  set(CMAKE_CXX_EXTENSIONS OFF)
  set(CMAKE_POSITION_INDEPENDENT_CODE ON)

  # The primitive seam. LUXCPP_CRYPTO_DIR, if a chain set it, is what the seam
  # uses to find the reused first-party crypto it compiles once.
  include(${CMAKE_CURRENT_LIST_DIR}/../zap.cmake)
  # The kernels are in luxfi/compute, which gpu/README.md names. LUX_GPU_DIR is
  # where that checkout is; a build that was not told fails here, with the
  # reason, rather than three errors further in.
  if(NOT LUX_GPU_DIR)
    set(LUX_GPU_DIR "$ENV{LUX_GPU_DIR}")
  endif()
  if(NOT LUX_GPU_DIR OR NOT EXISTS "${LUX_GPU_DIR}/CMakeLists.txt")
    message(FATAL_ERROR
      "no primitive seam: set LUX_GPU_DIR to luxfi/compute's gpu/cpp (see gpu/README.md)")
  endif()
  add_subdirectory(${LUX_GPU_DIR} ${CMAKE_CURRENT_BINARY_DIR}/lux_gpu)

  add_library(lux_core STATIC
    ${CMAKE_CURRENT_LIST_DIR}/src/id.cpp
    ${CMAKE_CURRENT_LIST_DIR}/src/store.cpp
  )
  target_link_libraries(lux_core PUBLIC zap::zap lux_gpu)
  target_include_directories(lux_core PUBLIC ${CMAKE_CURRENT_LIST_DIR}/include)
  target_compile_options(lux_core PRIVATE -Wall -Wextra)

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
