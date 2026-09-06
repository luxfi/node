# Copyright (C) 2026, Lux Industries Inc. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause-Eco
#
# The one ZAP implementation the C++ chains speak.
#
# ZAP is a bidirectional binary protocol with pipelining, and its C++ SDK is a
# published artifact — github.com/zap-proto/cpp, pinned to a tag below. It is
# reached as a package, never as a path into a checkout and never vendored: a
# chain that carries its own copy of the wire is a chain that can drift from
# it, which is exactly what happened when both of these chains hand-wrote
# frame handling and the two copies stopped agreeing.
#
# An installed package wins, so a machine that has one does not re-fetch. Set
# -DCMAKE_PREFIX_PATH=<install> to use it.

# The runtime the chains link, and the generator that prints what calls it.
# Two repositories, two versions: a tag from one does not exist in the other,
# and naming one tag for both is how the generator target came to point at a
# version that was never published.
set(ZAP_TAG v0.1.1)      # github.com/zap-proto/cpp — the runtime
set(ZAPGEN_TAG v1.9.0)   # github.com/zap-proto/go  — the generator

find_package(Zap QUIET)
if(NOT Zap_FOUND)
  include(FetchContent)
  FetchContent_Declare(Zap
    GIT_REPOSITORY https://github.com/zap-proto/cpp.git
    GIT_TAG        ${ZAP_TAG}
    GIT_SHALLOW    TRUE)
  FetchContent_MakeAvailable(Zap)
  message(STATUS "zap: ${ZAP_TAG} from github.com/zap-proto/cpp")
else()
  message(STATUS "zap: installed package ${Zap_VERSION}")
endif()

# zap_schema(NAME SCHEMA OUTDIR) declares the target that regenerates a chain's
# wire accessors from its schema.
#
# The output is committed beside the schema, so a build needs neither Go nor the
# generator; this target is for after an edit to the schema:
#
#   cmake --build build --target <name>
#
# The generator is fetched at its own published tag. Its output is checked in,
# so what a build compiles is what someone read; running this target is how a
# schema edit reaches the header beside it.
function(zap_schema name schema outdir)
  add_custom_target(${name}
    COMMAND ${CMAKE_COMMAND} -E env GOFLAGS=-mod=mod
            go run github.com/zap-proto/go/cmd/zapgen@${ZAPGEN_TAG}
            -lang cpp -single -out ${outdir} ${schema}
    COMMENT "zapgen ${ZAPGEN_TAG}: ${schema} -> ${outdir}")
endfunction()
