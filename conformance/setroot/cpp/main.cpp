// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// The C++ node's answer to the validator-set-root corpus.
//
// It calls `lux::node::validator_set_root` and nothing else. A second
// implementation of the encoding in this file would make the program agree with
// itself, and the differential would report that as agreement.

#include "lux/node/validators.hpp"

#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#include <vector>

using lux::node::Id;
using lux::node::SetMember;
using lux::node::validator_set_root;

namespace {

std::vector<std::string> split(const std::string& s, char sep) {
    std::vector<std::string> out;
    std::string cur;
    for (char c : s) {
        if (c == sep) {
            out.push_back(cur);
            cur.clear();
        } else {
            cur.push_back(c);
        }
    }
    out.push_back(cur);
    return out;
}

bool unhex(const std::string& s, std::vector<std::uint8_t>& out) {
    if (s.size() % 2 != 0) return false;
    out.clear();
    out.reserve(s.size() / 2);
    for (std::size_t i = 0; i < s.size(); i += 2) {
        const std::string byte = s.substr(i, 2);
        char* end = nullptr;
        const long v = std::strtol(byte.c_str(), &end, 16);
        if (end != byte.c_str() + 2) return false;
        out.push_back(static_cast<std::uint8_t>(v));
    }
    return true;
}

std::string hex(const Id& id) {
    static const char* d = "0123456789abcdef";
    std::string s;
    for (auto c : id) {
        s.push_back(d[c >> 4]);
        s.push_back(d[c & 0x0f]);
    }
    return s;
}

}  // namespace

int main(int argc, char** argv) {
    if (argc < 2) {
        std::fprintf(stderr, "setroot: name the corpus to answer\n");
        return 2;
    }
    std::ifstream in(argv[1]);
    if (!in) {
        std::fprintf(stderr, "setroot: %s: cannot read\n", argv[1]);
        return 1;
    }

    std::string line;
    while (std::getline(in, line)) {
        if (line.empty() || line[0] == '#') continue;
        const auto cols = split(line, '\t');
        if (cols.size() < 3 || cols[0] != "V") {
            std::fprintf(stderr, "setroot: not a vector line: %s\n", line.c_str());
            return 1;
        }
        std::vector<SetMember> set;
        for (std::size_t i = 3; i < cols.size(); ++i) {
            const auto f = split(cols[i], ':');
            if (f.size() != 3) {
                std::fprintf(stderr, "setroot: %s: a member is nodeID:weight:key\n", cols[1].c_str());
                return 1;
            }
            std::vector<std::uint8_t> node;
            SetMember m;
            if (!unhex(f[0], node) || node.size() != m.node_id.size()) {
                std::fprintf(stderr, "setroot: %s: a node id is %zu bytes\n", cols[1].c_str(),
                             m.node_id.size());
                return 1;
            }
            std::copy(node.begin(), node.end(), m.node_id.begin());
            m.weight = std::strtoull(f[1].c_str(), nullptr, 10);
            if (!unhex(f[2], m.pubkey)) {
                std::fprintf(stderr, "setroot: %s: a key is hex\n", cols[1].c_str());
                return 1;
            }
            set.push_back(std::move(m));
        }
        std::printf("R\t%s\t%s\t\n", cols[1].c_str(), hex(validator_set_root(set)).c_str());
    }
    return 0;
}
