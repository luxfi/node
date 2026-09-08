// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/oraclevm/gotime.hpp"

#include <cstdio>

namespace lux::oraclevm {
namespace {

std::int64_t days_from_civil(std::int64_t y, std::uint32_t m, std::uint32_t d) {
    y -= m <= 2;
    const std::int64_t era = (y >= 0 ? y : y - 399) / 400;
    const std::int64_t yoe = y - era * 400;
    const std::int64_t mp = (static_cast<std::int64_t>(m) + 9) % 12;
    const std::int64_t doy = (153 * mp + 2) / 5 + static_cast<std::int64_t>(d) - 1;
    const std::int64_t doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    return era * 146097 + doe - 719468;
}

std::uint32_t days_in_month(std::int64_t y, std::uint32_t m) {
    switch (m) {
        case 1: case 3: case 5: case 7: case 8: case 10: case 12: return 31;
        case 4: case 6: case 9: case 11: return 30;
        case 2: return ((y % 4 == 0 && y % 100 != 0) || y % 400 == 0) ? 29 : 28;
        default: return 0;
    }
}

bool digits(std::string_view s, std::size_t from, std::size_t to, std::int64_t* out) {
    std::int64_t v = 0;
    for (std::size_t i = from; i < to; i++) {
        if (s[i] < '0' || s[i] > '9') return false;
        v = v * 10 + (s[i] - '0');
    }
    *out = v;
    return true;
}

}  // namespace

std::int64_t Time::unix() const {
    return days_from_civil(year, month, day) * 86400 + static_cast<std::int64_t>(hour) * 3600 +
           static_cast<std::int64_t>(minute) * 60 + static_cast<std::int64_t>(second) -
           static_cast<std::int64_t>(offset);
}

std::string Time::render() const {
    char buf[64];
    std::snprintf(buf, sizeof(buf), "%04lld-%02u-%02uT%02u:%02u:%02u",
                  static_cast<long long>(year), month, day, hour, minute, second);
    std::string s(buf);
    if (nanos != 0) {
        char frac[16];
        std::snprintf(frac, sizeof(frac), "%09u", nanos);
        std::string f(frac);
        while (!f.empty() && f.back() == '0') f.pop_back();
        s.push_back('.');
        s += f;
    }
    if (offset == 0) {
        s.push_back('Z');
    } else {
        const std::int32_t off = offset < 0 ? -offset : offset;
        char zone[16];
        std::snprintf(zone, sizeof(zone), "%c%02d:%02d", offset < 0 ? '-' : '+', off / 3600,
                      (off % 3600) / 60);
        s += zone;
    }
    return s;
}

bool parse_time(std::string_view s, Time* out, std::string* err) {
    const auto bad = [&]() {
        *err = "parsing time \"" + std::string(s) + "\" as RFC 3339: cannot parse";
        return false;
    };
    if (s.size() < 20) return bad();
    if (s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':') return bad();

    Time t;
    std::int64_t v = 0;
    if (!digits(s, 0, 4, &v)) return bad();
    t.year = v;
    if (!digits(s, 5, 7, &v)) return bad();
    t.month = static_cast<std::uint32_t>(v);
    if (!digits(s, 8, 10, &v)) return bad();
    t.day = static_cast<std::uint32_t>(v);
    if (!digits(s, 11, 13, &v)) return bad();
    t.hour = static_cast<std::uint32_t>(v);
    if (!digits(s, 14, 16, &v)) return bad();
    t.minute = static_cast<std::uint32_t>(v);
    if (!digits(s, 17, 19, &v)) return bad();
    t.second = static_cast<std::uint32_t>(v);

    std::size_t i = 19;
    if (i < s.size() && s[i] == '.') {
        i++;
        const std::size_t start = i;
        while (i < s.size() && s[i] >= '0' && s[i] <= '9') i++;
        if (i == start) return bad();
        std::uint64_t nanos = 0;
        const std::size_t n = i - start;
        for (std::size_t k = 0; k < n && k < 9; k++) nanos = nanos * 10 + (s[start + k] - '0');
        for (std::size_t k = n; k < 9; k++) nanos *= 10;
        t.nanos = static_cast<std::uint32_t>(nanos);
    }
    if (i >= s.size()) return bad();
    if (s[i] == 'Z') {
        i++;
    } else if (s[i] == '+' || s[i] == '-') {
        if (i + 6 > s.size() || s[i + 3] != ':') return bad();
        std::int64_t hh = 0, mm = 0;
        if (!digits(s, i + 1, i + 3, &hh) || !digits(s, i + 4, i + 6, &mm)) return bad();
        if (hh > 23 || mm > 59) return bad();
        const std::int32_t off = static_cast<std::int32_t>(hh * 3600 + mm * 60);
        t.offset = s[i] == '-' ? -off : off;
        i += 6;
    } else {
        return bad();
    }
    if (i != s.size()) return bad();
    if (t.month < 1 || t.month > 12 || t.day < 1 || t.day > days_in_month(t.year, t.month)) {
        *err = "parsing time \"" + std::string(s) + "\": day out of range";
        return false;
    }
    if (t.hour > 23 || t.minute > 59 || t.second > 59) return bad();
    *out = t;
    return true;
}

}  // namespace lux::oraclevm
