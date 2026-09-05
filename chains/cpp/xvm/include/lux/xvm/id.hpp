// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the X-Chain does not have names of its own.
//
// Id, ShortId, NodeId, Bytes, ByteView and the hashes that derive them are the
// NODE's, defined once in lux/core/id.hpp and shared with every other chain
// here. This file declares nothing; it adopts them, so that code written inside
// `namespace lux::xvm` reads `Id` and means the same thirty-two bytes the
// P-chain and the consensus seam mean.
//
// Anything an X-Chain name needs that a name in general does not would go
// below. Nothing does.

#pragma once

#include "lux/core/id.hpp"

namespace lux::xvm {

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

}  // namespace lux::xvm
