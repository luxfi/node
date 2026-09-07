// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/platformvm/block.hpp"
#include "lux/platformvm/txs.hpp"
#include "lux/platformvm/warp.hpp"

#include <cassert>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>
#include <vector>

using namespace lux::platformvm;

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

    std::vector<std::string> p_txs = {
        "P_BASE_TX",
        "P_ADD_VALIDATOR_TX",
        "P_CREATE_NETWORK_TX",
        "P_REGISTER_L1_VALIDATOR_TX",
        "P_SET_L1_VALIDATOR_WEIGHT_TX",
        "P_INCREASE_L1_VALIDATOR_BALANCE_TX",
        "P_DISABLE_L1_VALIDATOR_TX"
    };

    for (const auto& id : p_txs) {
        std::string hex_str = parse_field(id, "wire_hex");
        if (hex_str.empty()) continue;
        auto bytes = unhex(hex_str);
        auto res = txs::parse(bytes);
        if (res.has_value()) {
            std::cout << "RESULT id=" << id << " status=ACCEPTED detail=parsed p-chain tx" << std::endl;
        } else {
            std::cout << "RESULT id=" << id << " status=REJECTED detail=" << res.error().message() << std::endl;
        }
    }

    // Warp message
    std::string warp_hex = parse_field("P_WARP_MESSAGE_VERIFY", "wire_hex");
    if (!warp_hex.empty()) {
        auto bytes = unhex(warp_hex);
        auto res = warp::UnsignedMessage::parse(bytes);
        if (res.has_value()) {
            std::cout << "RESULT id=P_WARP_MESSAGE_VERIFY status=ACCEPTED detail=parsed unsigned warp message" << std::endl;
        } else {
            std::cout << "RESULT id=P_WARP_MESSAGE_VERIFY status=REJECTED detail=" << res.error().message() << std::endl;
        }
    }

    // Block
    std::string blk_hex = parse_field("P_STANDARD_BLOCK", "wire_hex");
    if (!blk_hex.empty()) {
        auto bytes = unhex(blk_hex);
        auto res = block::parse(bytes);
        if (res.has_value()) {
            std::cout << "RESULT id=P_STANDARD_BLOCK status=ACCEPTED detail=parsed standard block" << std::endl;
        } else {
            std::cout << "RESULT id=P_STANDARD_BLOCK status=REJECTED detail=" << res.error().message() << std::endl;
        }
    }

    return 0;
}
