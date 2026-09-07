// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// ids.hpp — the P-chain does not have names of its own.
//
// Id, ShortId, NodeId and the hashes that derive them are the NODE's, defined
// once in lux/core/id.hpp and shared with every other chain here. Two chains in
// one binary with two ideas of what a thirty-two-byte name IS have to convert
// between them, and a conversion is a thing that can be got wrong.
//
// This file declares no type and no hash. It adopts them, and adds the one
// name that IS a P-chain fact rather than a naming fact: the primary network.

#pragma once

#include "lux/core/id.hpp"

namespace lux::platformvm {

using core::Bytes;
using core::ByteView;
using core::Id;
using core::NodeId;
using core::ShortId;

using core::kEmptyId;
using core::kEmptyNodeId;
using core::kEmptyShortId;
using core::kIdLen;
using core::kNodeIdLen;
using core::kShortIdLen;

using core::bytes_from;
using core::id_from;
using core::node_id_from;
using core::short_id_from;
using core::view;

using core::append_id;
using core::hex;
using core::prefix_id;
using core::pubkey_to_address;
using core::sha256;

// The primary network is the identity element of the network hierarchy: an
// L1's parent, and the network every P-chain validator validates. It is the
// zero id — which is a fact about this chain, not about names, so it lives
// here and not in core.
inline constexpr Id kPrimaryNetworkId{};

}  // namespace lux::platformvm
