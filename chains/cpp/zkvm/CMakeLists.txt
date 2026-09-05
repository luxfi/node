# Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco

cmake_minimum_required(VERSION 3.20)
project(lux_zkvm LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

include_directories(include)

add_library(lux_zkvm INTERFACE)
target_include_directories(lux_zkvm INTERFACE include)

enable_testing()
add_executable(zkvm_test test/zkvm_test.cpp)
target_link_libraries(zkvm_test PRIVATE lux_zkvm)
add_test(NAME zkvm_test COMMAND zkvm_test)
