# Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco

cmake_minimum_required(VERSION 3.20)
project(lux_quantumvm LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

include_directories(include)

add_library(lux_quantumvm INTERFACE)
target_include_directories(lux_quantumvm INTERFACE include)

enable_testing()
add_executable(quantum_test test/quantum_test.cpp)
target_link_libraries(quantum_test PRIVATE lux_quantumvm)
add_test(NAME quantum_test COMMAND quantum_test)
