// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// gotime.hpp — an instant, read and written the way the Go chain reads and
// writes one.
//
// `time.Time` marshals as RFC 3339 with the fraction trimmed of trailing zeros
// and dropped entirely when it is zero, and with the OFFSET it was parsed
// under — NOT normalised to UTC. Both halves are the wire: this chain's block
// id is the SHA-256 of the block marshalled again, so a timestamp written as Z
// where it arrived as +05:30 names a different block.
//
// The instant is kept as the offset-local civil fields it arrived in, because
// that is what survives a round trip exactly; the epoch second is derived when
// a rule needs one.

#pragma once

#include <cstdint>
#include <string>
#include <string_view>

namespace lux::oraclevm {

struct Time {
    std::int64_t year = 1;
    std::uint32_t month = 1;
    std::uint32_t day = 1;
    std::uint32_t hour = 0;
    std::uint32_t minute = 0;
    std::uint32_t second = 0;
    std::uint32_t nanos = 0;
    // Seconds east of UTC. Zero is written Z.
    std::int32_t offset = 0;

    std::int64_t unix() const;
    std::string render() const;
};

// parse reads RFC 3339 strictly: a four-digit year, a literal T, and either Z
// or ±HH:MM. Anything else is refused with a reason.
bool parse_time(std::string_view s, Time* out, std::string* err);

}  // namespace lux::oraclevm
