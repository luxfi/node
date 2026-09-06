// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// conformance.cpp — the C++ EVM's answers to the shared precompile corpus.
//
// One of the evaluators — Go, C++ — that read the same
// conformance/corpus/precompile_vectors.tsv and print the same five fields per
// vector. The runner compares them; this program never sees another
// implementation's answer and has nothing to agree with.
//
// The subject is cevm's own precompile seam: cevm::state::is_precompile and
// cevm::state::call_precompile. That pair is what the C++ node's EVM host
// calls (lib/evm/state/evmc_host.hpp, lib/evm/state/processor.cpp), so it is
// the price and the bytes a C++ validator would put in a state root. The GPU
// dispatcher beside it (lib/evm/gpu/precompiles) is an accelerator keyed on a
// sixteen-bit address that documents itself as matching this one; asking it
// would be asking the same table through a narrower window.
//
// Usage: evm_conformance <precompile_vectors.tsv>

#include <test/state/precompiles.hpp>
#include <test/state/precompiles_internal.hpp>

#include <evmc/evmc.hpp>

#include <array>
#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <string>
#include <vector>

using namespace cevm::state;
using namespace evmc::literals;  // the _address literal, for whole addresses

namespace {

// The revision. p256verify at 0x…0100 is live from Osaka, and the Go reference
// reads geth's Osaka table, so this is the revision at which the two are asked
// the same question.
constexpr evmc_revision kRevision = EVMC_OSAKA;

constexpr const char* kNone = "-";

// The verdict vocabulary, shared with the Go evaluator. There is no STATE here:
// no precompile cevm serves reads a chain.
constexpr const char* kOk = "OK";
constexpr const char* kFailed = "FAILED";
constexpr const char* kOog = "OOG";
constexpr const char* kAbsent = "ABSENT";

// ── the price of a call ──────────────────────────────────────────────────────
//
// GAS IS WHAT WAS CHARGED, NEVER WHAT WAS LEFT, and cevm's result carries what
// was left: call_precompile deducts analyze()'s gas_cost up front, hands back
// msg.gas - gas_cost on success, and zeroes even that on any non-success. The
// charge on a REFUSED call therefore survives nowhere in the result, and the
// only place it still exists is the analyze function that computed it.
//
// So this table is here — the same table precompiles.cpp dispatches on, read
// for the number rather than for the handler. A second copy of a table is a
// second thing to keep in step, and this one is kept in step by the compiler
// and by check_table() below rather than by whoever edits cevm next.
struct Price {
    // The WHOLE address. cevm's dispatcher keys the stock range on the last two
    // bytes and matches everything else on all twenty, and a Lux precompile is
    // everything else: inference is at 0x0300…0003, whose last two bytes are
    // ripemd160's. Keying on two bytes here priced a call to that address at
    // ripemd160's 720 gas and named it ripemd160, with no sign anything was
    // wrong.
    evmc::address address;
    const char* name;
    decltype(identity_analyze)* analyze;
};

constexpr auto kPrices = std::to_array<Price>({
    {0x0000000000000000000000000000000000000001_address, "ecrecover", ecrecover_analyze},
    {0x0000000000000000000000000000000000000002_address, "sha256", sha256_analyze},
    {0x0000000000000000000000000000000000000003_address, "ripemd160", ripemd160_analyze},
    {0x0000000000000000000000000000000000000004_address, "identity", identity_analyze},
    {0x0000000000000000000000000000000000000005_address, "expmod", expmod_analyze},
    {0x0000000000000000000000000000000000000006_address, "ecadd", ecadd_analyze},
    {0x0000000000000000000000000000000000000007_address, "ecmul", ecmul_analyze},
    {0x0000000000000000000000000000000000000008_address, "ecpairing", ecpairing_analyze},
    {0x0000000000000000000000000000000000000009_address, "blake2bf", blake2bf_analyze},
    {0x000000000000000000000000000000000000000a_address, "point_evaluation", point_evaluation_analyze},
    // BLS12-381, on the layout the C-Chain runs — EIP-2537's draft, where
    // multiplication has its own precompile and the field maps have no address.
    // cevm serves these seven from its Lux table, which shadows the stock one.
    {0x000000000000000000000000000000000000000b_address, "bls12_g1add", bls12_g1add_analyze},
    {0x000000000000000000000000000000000000000c_address, "bls12_g1mul", bls12_g1mul_analyze},
    {0x000000000000000000000000000000000000000d_address, "bls12_g1msm", bls12_g1msm_analyze},
    {0x000000000000000000000000000000000000000e_address, "bls12_g2add", bls12_g2add_analyze},
    {0x000000000000000000000000000000000000000f_address, "bls12_g2mul", bls12_g2mul_analyze},
    {0x0000000000000000000000000000000000000010_address, "bls12_g2msm", bls12_g2msm_analyze},
    {0x0000000000000000000000000000000000000011_address, "bls12_pairing_check", bls12_pairing_check_analyze},
    {0x0000000000000000000000000000000000000100_address, "p256verify", p256verify_analyze},
    // Lux's, outside the stock range. Go serves inference here
    // (luxfi/precompile/inference) and cevm does now too.
    {0x0300000000000000000000000000000000000003_address, "aivm", aivm_analyze},
});

// The widest address cevm's availability table can hold, so the scan below
// covers every address the seam could possibly answer for.
constexpr std::uint32_t kLastIndex = 0x0100;

evmc::address address_of(std::uint32_t index) noexcept
{
    evmc::address a{};
    a.bytes[18] = static_cast<std::uint8_t>(index >> 8);
    a.bytes[19] = static_cast<std::uint8_t>(index);
    return a;
}

const Price* price_of(const evmc::address& addr) noexcept
{
    for (const auto& p : kPrices)
        if (p.address == addr) return &p;
    return nullptr;
}

// The two tables agree or this program does not run.
//
// An address cevm starts serving that has no price here would be priced by
// nothing on every refusal at it — a wrong number printed with the same
// confidence as a right one. An address priced here that cevm does not serve is
// the same mistake pointing the other way. Both are silent, and both stop
// being silent here.
bool check_table()
{
    for (std::uint32_t i = 0; i <= kLastIndex; ++i) {
        const auto addr = address_of(i);
        const bool served = is_precompile(kRevision, addr);
        const bool priced = price_of(addr) != nullptr;
        if (served != priced) {
            std::fprintf(stderr,
                "evm_conformance: cevm %s a precompile at %04x and this evaluator %s it\n",
                served ? "serves" : "does not serve", i, priced ? "prices" : "does not price");
            return false;
        }
    }
    // And every address this evaluator prices that the scan above cannot
    // reach. A price for an address cevm does not serve is the same silent
    // mistake as the reverse, and the loop's range is the stock one.
    for (const auto& p : kPrices) {
        if (!is_precompile(kRevision, p.address)) {
            std::fprintf(stderr,
                "evm_conformance: this evaluator prices %s at an address cevm does not serve\n",
                p.name);
            return false;
        }
    }
    return true;
}

// ── the corpus ───────────────────────────────────────────────────────────────

struct Vector {
    std::string id;
    evmc::address address{};
    std::uint64_t gas = 0;
    std::vector<std::uint8_t> input;
};

int nibble(char c) noexcept
{
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

bool unhex(const std::string& s, std::vector<std::uint8_t>& out)
{
    if (s == kNone) return true;
    if (s.size() % 2 != 0) return false;
    out.reserve(s.size() / 2);
    for (std::size_t i = 0; i < s.size(); i += 2) {
        const int hi = nibble(s[i]), lo = nibble(s[i + 1]);
        if (hi < 0 || lo < 0) return false;
        out.push_back(static_cast<std::uint8_t>((hi << 4) | lo));
    }
    return true;
}

std::string hex(const std::uint8_t* b, std::size_t n)
{
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(n * 2);
    for (std::size_t i = 0; i < n; ++i) {
        out.push_back(d[b[i] >> 4]);
        out.push_back(d[b[i] & 0xF]);
    }
    return out;
}

// ── the answer ───────────────────────────────────────────────────────────────

struct Row {
    std::string id;
    const char* status = kAbsent;
    std::uint64_t gas = 0;
    std::string output = kNone;
    std::string note;
};

void print(const Row& r)
{
    std::string note = r.note;
    for (auto& c : note)
        if (c == '\t' || c == '\n') c = ' ';
    if (note.empty()) note = kNone;
    std::printf("R\t%s\t%s\t%llu\t%s\t%s\n", r.id.c_str(), r.status,
        static_cast<unsigned long long>(r.gas), r.output.c_str(), note.c_str());
}

Row run(const Vector& v)
{
    Row r;
    r.id = v.id;

    if (!is_precompile(kRevision, v.address)) {
        r.note = "no precompile at this address";
        return r;
    }
    const Price* price = price_of(v.address);  // non-null: check_table() said so

    // A vector offering more gas than the seam can hold is a corpus this
    // program cannot answer, and saying so beats truncating it into an answer.
    if (v.gas > static_cast<std::uint64_t>(INT64_MAX)) {
        std::fprintf(stderr, "evm_conformance: %s: gas %llu does not fit an int64\n", v.id.c_str(),
            static_cast<unsigned long long>(v.gas));
        std::exit(1);
    }

    // An empty input is a pointer nothing may be read through, not a null one.
    static constexpr std::uint8_t kNoBytes[1]{};

    evmc_message msg{};
    msg.kind = EVMC_CALL;
    msg.gas = static_cast<std::int64_t>(v.gas);
    msg.recipient = v.address;
    msg.code_address = v.address;
    msg.input_data = v.input.empty() ? kNoBytes : v.input.data();
    msg.input_size = v.input.size();

    const auto charge = price->analyze({msg.input_data, msg.input_size}, kRevision).gas_cost;
    const auto result = call_precompile(kRevision, msg);
    r.note = std::string("cevm:") + price->name;

    switch (result.status_code) {
    case EVMC_OUT_OF_GAS:
        // No charge to report: the call never happened, and a number nobody
        // agrees to means the column stops comparing.
        r.status = kOog;
        r.note += ": out of gas";
        return r;

    case EVMC_SUCCESS:
        // The seam's own arithmetic, which is the ground truth wherever the
        // seam still has it.
        r.status = kOk;
        r.gas = v.gas - static_cast<std::uint64_t>(result.gas_left);
        if (result.output_size > 0) r.output = hex(result.output_data, result.output_size);
        // And where the seam still has it, it is what the price table says. A
        // price wired to the wrong precompile passes the presence check above
        // and prices every refusal at this address wrongly; here it does not
        // survive the first call that succeeds.
        if (r.gas != static_cast<std::uint64_t>(charge)) {
            std::fprintf(stderr,
                "evm_conformance: %s: the seam charged %llu and %s_analyze says %lld\n",
                v.id.c_str(), static_cast<unsigned long long>(r.gas), price->name,
                static_cast<long long>(charge));
            std::exit(1);
        }
        return r;

    default:
        // The charge stands. A precompile that read the input and refused it
        // did the work of reading it, and the result no longer carries what
        // that cost, so it comes from the analyze that computed it.
        r.status = kFailed;
        r.gas = static_cast<std::uint64_t>(charge);
        r.note += std::string(": ") + evmc::to_string(result.status_code);
        return r;
    }
}

}  // namespace

int main(int argc, char** argv)
{
    if (argc < 2) {
        std::fprintf(stderr, "usage: evm_conformance <precompile_vectors.tsv>\n");
        return 2;
    }
    if (!check_table()) return 1;

    std::ifstream in(argv[1]);
    if (!in) {
        std::fprintf(stderr, "evm_conformance: cannot read %s\n", argv[1]);
        return 1;
    }

    std::string line;
    while (std::getline(in, line)) {
        if (line.empty() || line[0] == '#') continue;

        std::vector<std::string> f;
        std::size_t start = 0;
        for (;;) {
            const std::size_t tab = line.find('\t', start);
            if (tab == std::string::npos) {
                f.push_back(line.substr(start));
                break;
            }
            f.push_back(line.substr(start, tab - start));
            start = tab + 1;
        }
        if (f.size() != 5 || f[0] != "V") {
            std::fprintf(stderr, "evm_conformance: not a vector line: %s\n", line.c_str());
            return 1;
        }

        Vector v;
        v.id = f[1];
        std::vector<std::uint8_t> addr;
        if (!unhex(f[2], addr) || addr.size() != sizeof(v.address.bytes)) {
            std::fprintf(stderr, "evm_conformance: %s: not a 20-byte address: %s\n", v.id.c_str(),
                f[2].c_str());
            return 1;
        }
        for (std::size_t i = 0; i < addr.size(); ++i) v.address.bytes[i] = addr[i];
        v.gas = std::strtoull(f[3].c_str(), nullptr, 10);
        if (!unhex(f[4], v.input)) {
            std::fprintf(stderr, "evm_conformance: %s: input is not hex\n", v.id.c_str());
            return 1;
        }
        print(run(v));
    }
    return 0;
}
