// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// The optional backend: the kernel library, opened at run time if it is there.
// No kernel source lives in this repository and none ever will — what is
// declared here is four symbol names and their C signatures, which is the whole
// point of having a plugin ABI.

#pragma once

#include "lux/gpu/gpu.hpp"

#include <optional>
#include <string>
#include <vector>

namespace lux::gpu::plugin {

// The live backend's own name ("cpu", "cuda", "metal", ...), or nothing when no
// library is installed.
std::optional<std::string> backend_name();

// One Keccak-256 per input, through the plugin. Nothing means "ask the CPU" —
// no library, or a backend that declined the batch. A device's bad day must
// never propagate into consensus.
std::optional<std::vector<Digest>> keccak256_batch(std::span<const Bytes> inputs);

}  // namespace lux::gpu::plugin
