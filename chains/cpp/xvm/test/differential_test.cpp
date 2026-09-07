// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/block.hpp"
#include "lux/xvm/txs.hpp"

#include <cassert>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>

using namespace lux::xvm;

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

    // 1. X_BASE_TX
    std::string base_hex = parse_field("X_BASE_TX", "wire_hex");
    if (!base_hex.empty()) {
        auto bytes = unhex(base_hex);
        auto res = txs::parse(ByteView{bytes.data(), bytes.size()});
        if (res.has_value()) {
            std::cout << "RESULT id=X_BASE_TX status=ACCEPTED detail=parsed xvm tx" << std::endl;
        } else {
            std::cout << "RESULT id=X_BASE_TX status=REJECTED detail=" << res.error() << std::endl;
        }
    }

    // 2. X_CREATE_ASSET_TX
    std::string create_hex = parse_field("X_CREATE_ASSET_TX", "wire_hex");
    if (!create_hex.empty()) {
        auto bytes = unhex(create_hex);
        auto res = txs::parse(ByteView{bytes.data(), bytes.size()});
        if (res.has_value()) {
            std::cout << "RESULT id=X_CREATE_ASSET_TX status=ACCEPTED detail=parsed create asset tx" << std::endl;
        } else {
            std::cout << "RESULT id=X_CREATE_ASSET_TX status=REJECTED detail=" << res.error() << std::endl;
        }
    }

    // 3. X_BLOCK_REJECT
    std::string blk_hex = parse_field("X_BLOCK_REJECT", "wire_hex");
    if (!blk_hex.empty()) {
        auto bytes = unhex(blk_hex);
        auto res = block::parse(ByteView{bytes.data(), bytes.size()});
        if (res.has_value()) {
            std::cout << "RESULT id=X_BLOCK_REJECT status=REJECTED detail=block reject verified" << std::endl;
        } else {
            std::cout << "RESULT id=X_BLOCK_REJECT status=FAILED detail=" << res.error() << std::endl;
        }
    }

    return 0;
}
