// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// clock.hpp — what time it is, said once.
//
// Two units, because the chain uses two and confusing them is a fork: a block
// and a transaction are stamped in Unix SECONDS (that is what the wire carries,
// so two nodes a millisecond apart agree), and a quantum stamp is made in Unix
// NANOSECONDS (it is a freshness window measured in tens of milliseconds).
//
// Clock is settable, rendering Go's timer/mockable.Clock: a test states what
// time it is rather than asking the machine, so a block's timestamp is
// something the test decided. Set() is what makes the skew rules testable at
// all — they are about the difference between two clocks.

#pragma once

#include <atomic>
#include <chrono>
#include <cstdint>

namespace lux::quantumvm {

using Nanos = std::int64_t;
using Seconds = std::int64_t;

// A span of time, in the same unit the stamps are made in.
using Duration = std::chrono::nanoseconds;

// What an ABSENT time is on the wire.
//
// Go's zero time.Time is midnight of January 1st, year 1 — not the Unix epoch —
// and UnixNano() renders that as a fixed negative count, wrapping the int64 on
// the way. So a Go struct nobody set a time on does not serialize as 0; it
// serializes as this. Anywhere a field carries an unset Go timestamp, this is
// the number that has to be written, or the same value has two spellings and
// the two implementations hash it to two different ids.
inline constexpr Nanos kZeroTimeNanos = -6795364578871345152;

inline Nanos wall_nanos() {
    return std::chrono::duration_cast<std::chrono::nanoseconds>(
               std::chrono::system_clock::now().time_since_epoch())
        .count();
}

// Unix seconds from Unix nanoseconds, flooring — the direction Go's
// time.Time.Unix() rounds, so a timestamp does not gain a second in
// conversion.
inline Seconds to_seconds(Nanos ns) {
    return ns >= 0 ? ns / 1'000'000'000 : -(((-ns) + 999'999'999) / 1'000'000'000);
}

inline Nanos from_seconds(Seconds s) { return s * 1'000'000'000; }

class Clock {
  public:
    // Unset, this reads the machine. Set, it reads whatever it was told, which
    // is what lets a test place a block in time.
    Nanos nanos() const {
        const Nanos faked = faked_.load(std::memory_order_relaxed);
        return set_.load(std::memory_order_relaxed) ? faked : wall_nanos();
    }
    Seconds seconds() const { return to_seconds(nanos()); }

    void set(Nanos ns) {
        faked_.store(ns, std::memory_order_relaxed);
        set_.store(true, std::memory_order_relaxed);
    }
    void unset() { set_.store(false, std::memory_order_relaxed); }

  private:
    std::atomic<Nanos> faked_{0};
    std::atomic<bool> set_{false};
};

}  // namespace lux::quantumvm
