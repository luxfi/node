// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/zkvm.hpp"

#include <cassert>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <sstream>

using namespace lux::zkvm;

std::vector<std::uint8_t> unhex(std::string_view hex) {
    std::vector<std::uint8_t> bytes;
    for (std::size_t i = 0; i < hex.length(); i += 2) {
        std::string byteString = std::string(hex.substr(i, 2));
        auto byte = static_cast<std::uint8_t>(strtol(byteString.c_str(), nullptr, 16));
        bytes.push_back(byte);
    }
    return bytes;
}

int main(int argc, char** argv) {
    std::string path;
    if (argc > 1) {
        path = argv[1];
    } else if (const char* env_path = std::getenv("NODE2_CORPUS_PATH")) {
        path = env_path;
    } else {
        std::vector<std::string> candidates = {
            "conformance/corpus/chain_differential.json",
            "../conformance/corpus/chain_differential.json",
            "../../conformance/corpus/chain_differential.json",
            "../../../conformance/corpus/chain_differential.json",
            "../../../../conformance/corpus/chain_differential.json",
            "/home/z/work/lux/node2/conformance/corpus/chain_differential.json"
        };
        for (const auto& c : candidates) {
            std::ifstream test_f(c);
            if (test_f.is_open()) {
                path = c;
                break;
            }
        }
    }

    std::ifstream file(path);
    if (!file.is_open()) {
        std::cerr << "Could not open corpus file: " << path << std::endl;
        return 1;
    }

    std::stringstream buffer;
    buffer << file.rdbuf();
    std::string content = buffer.str();

    auto parse_field = [&](std::string_view id, std::string_view key) -> std::string {
        std::string id_marker = "\"id\": \"" + std::string(id) + "\"";
        auto pos = content.find(id_marker);
        if (pos == std::string::npos) return "";
        std::string key_marker = "\"" + std::string(key) + "\": \"";
        auto key_pos = content.find(key_marker, pos);
        if (key_pos == std::string::npos) return "";
        auto val_start = key_pos + key_marker.length();
        auto val_end = content.find("\"", val_start);
        if (val_end == std::string::npos) return "";
        return content.substr(val_start, val_end - val_start);
    };

    // 1. Z_SHIELDED_TX
    std::string z_tx_hex = parse_field("Z_SHIELDED_TX", "wire_hex");
    assert(!z_tx_hex.empty());
    auto tx_bytes = unhex(z_tx_hex);
    auto tx = Transaction::parse(tx_bytes);
    if (tx) {
        auto v = tx->verify();
        if (v) {
            std::cout << "RESULT id=Z_SHIELDED_TX status=ACCEPTED detail=parsed zkvm tx" << std::endl;
        } else {
            std::cerr << "RESULT id=Z_SHIELDED_TX status=REJECTED detail=" << v.error() << std::endl;
            return 1;
        }
    } else {
        std::cerr << "RESULT id=Z_SHIELDED_TX status=REJECTED detail=" << tx.error() << std::endl;
        return 1;
    }

    // 2. Z_BLOCK
    std::string z_blk_hex = parse_field("Z_BLOCK", "wire_hex");
    assert(!z_blk_hex.empty());
    auto blk_bytes = unhex(z_blk_hex);
    auto blk = Block::parse(blk_bytes);
    if (blk) {
        assert(blk->txs.size() == 1);
        auto v = blk->verify();
        if (v) {
            std::cout << "RESULT id=Z_BLOCK status=ACCEPTED detail=parsed zkvm block height=" << blk->height << std::endl;
        } else {
            std::cerr << "RESULT id=Z_BLOCK status=REJECTED detail=" << v.error() << std::endl;
            return 1;
        }
    } else {
        std::cerr << "RESULT id=Z_BLOCK status=REJECTED detail=" << blk.error() << std::endl;
        return 1;
    }

    // 3. VM basic lifecycle
    ZkVm vm;
    assert(vm.alias() == "Z");
    auto res_issue = vm.issue_tx(*tx);
    assert(res_issue.has_value());

    return 0;
}
