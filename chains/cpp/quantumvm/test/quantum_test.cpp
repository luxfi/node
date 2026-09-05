// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/quantumvm.hpp"

#include <cassert>
#include <fstream>
#include <iostream>
#include <sstream>

using namespace lux::quantumvm;

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

    // Simple search for fields in the json content
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

    std::string q_tx_hex = parse_field("Q_BASE_TX", "wire_hex");
    assert(!q_tx_hex.empty());
    auto tx_bytes = unhex(q_tx_hex);
    auto tx = Transaction::parse(tx_bytes);
    if (tx) {
        std::cout << "RESULT id=Q_BASE_TX status=ACCEPTED detail=parsed quantum transaction" << std::endl;
    } else {
        std::cerr << "RESULT id=Q_BASE_TX status=REJECTED detail=" << tx.error() << std::endl;
        return 1;
    }

    std::string q_blk_hex = parse_field("Q_BLOCK", "wire_hex");
    assert(!q_blk_hex.empty());
    auto blk_bytes = unhex(q_blk_hex);
    auto blk = Block::parse(blk_bytes);
    if (blk) {
        std::cout << "RESULT id=Q_BLOCK status=ACCEPTED detail=parsed quantum block height=" << blk->height << std::endl;
    } else {
        std::cerr << "RESULT id=Q_BLOCK status=REJECTED detail=" << blk.error() << std::endl;
        return 1;
    }

    return 0;
}
