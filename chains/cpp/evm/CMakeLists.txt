# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# evm — the C++ EVM's evaluator for the cross-language precompile differential.
#
# There is no new implementation here. The subject is cevm's precompile seam,
# cevm::state::{is_precompile, call_precompile}, which is what the C++ node's
# EVM host calls; this directory builds one program that asks it the corpus's
# questions and prints its answers. See conformance/PRECOMPILE.md.
#
# Reused, NOT vendored, exactly as chains/cpp/xvm reuses its dependencies. The
# checkouts are found where they actually sit; -DCEVM_DIR overrides.
#
# cevm's own dependencies come from Conan (lux-crypto, intx and the blst that
# lux-crypto pulls behind it), so configure with the toolchain a `conan install`
# of the conanfile.txt beside this file generates. `make precompiles-build`
# does both.

cmake_minimum_required(VERSION 3.20)
project(lux_evm_conformance LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

# find_checkout(VAR MARKER candidate...) — first candidate holding MARKER wins.
# The marker is a FILE, not a directory name: there are two cevm checkouts on a
# developer's machine and more than one directory called `crypto`, and only a
# header tells any of them apart.
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

find_checkout(_cevm "test/state/precompiles.hpp"
  "${CEVM_DIR}"
  "$ENV{HOME}/work/lux-cpp/cevm"
  "${CMAKE_SOURCE_DIR}/../../../../../lux-cpp/cevm")
if(NOT _cevm)
  message(FATAL_ERROR
    "cevm not found (looked for test/state/precompiles.hpp). "
    "Pass -DCEVM_DIR=/path/to/cevm.")
endif()
message(STATUS "evm: precompile seam at ${_cevm}")

# precompiles.cpp includes the header-only aivm oracle from lux-private. cevm's
# own build reaches it by a path relative to its checkout, so this does too: an
# overridden -DCEVM_DIR moves both together.
find_checkout(_aivm "tiny_llm/tiny_llm.hpp"
  "${AIVM_DIR}"
  "${_cevm}/../../lux-private/gpu-kernels/ops/aivm"
  "$ENV{HOME}/work/lux-private/gpu-kernels/ops/aivm")
if(NOT _aivm)
  message(FATAL_ERROR
    "the aivm oracle headers were not found (looked for tiny_llm/tiny_llm.hpp). "
    "Pass -DAIVM_DIR=/path/to/lux-private/gpu-kernels/ops/aivm.")
endif()
message(STATUS "evm: aivm headers at ${_aivm}")

find_package(intx CONFIG REQUIRED)

# evmc — the VM interface the seam speaks, a submodule of the cevm checkout.
# EXCLUDE_FROM_ALL because only the header-only evmc_cpp is linked; the loader,
# the instruction tables and the tooling are built by nothing here.
set(EVMC_INSTALL OFF CACHE BOOL "" FORCE)
set(EVMC_TOOLS OFF CACHE BOOL "" FORCE)
add_subdirectory(${_cevm}/evmc evmc EXCLUDE_FROM_ALL)

# cevm::evmmax — the header-only modular arithmetic cevm_precompiles re-exports
# for its include path. cevm's lib/evmmax/CMakeLists.txt names that directory as
# ${PROJECT_SOURCE_DIR}/include, which resolves against whichever project() ran
# last; adding it from here would aim it at this evaluator. It declares three
# lines, so those three lines are declared here, against the checkout that was
# actually found.
add_library(evmmax INTERFACE)
add_library(cevm::evmmax ALIAS evmmax)
target_include_directories(evmmax INTERFACE ${_cevm}/include)
target_link_libraries(evmmax INTERFACE intx::intx)

# The algorithm bodies, built by cevm's own CMakeLists so that what links what
# stays cevm's answer rather than a copy of it here.
add_subdirectory(${_cevm}/lib/cevm_precompiles cevm_precompiles)

# The seam itself: one translation unit out of cevm-state, the one that holds
# the dispatch table, the analyze functions and call_precompile. The rest of
# cevm-state is the host and the state, which a precompile call does not touch.
add_library(cevm_state_precompiles STATIC ${_cevm}/test/state/precompiles.cpp)
target_include_directories(cevm_state_precompiles PUBLIC ${_cevm} PRIVATE ${_aivm})
target_link_libraries(cevm_state_precompiles PUBLIC cevm::precompiles evmc::evmc_cpp intx::intx)

# The evaluator. It is not a test: it prints cevm's answer for every vector in
# conformance/corpus/precompile_vectors.tsv and says nothing about whether that
# answer is right. Deciding that is the runner's job, and it needs every
# implementation's answer to do it.
add_executable(evm_conformance test/conformance.cpp)
target_link_libraries(evm_conformance PRIVATE cevm_state_precompiles)
target_compile_options(evm_conformance PRIVATE -Wall -Wextra)
