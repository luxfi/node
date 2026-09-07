// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_test.cpp — the transaction wire, byte for byte against the Go reference.
//
// Every case builds the SAME fixture the Go generator built (scratchpad/goldgen
// prints them; golden.hpp pins the output) and asserts the buffers are equal.
// A round-trip test proves an implementation agrees with itself, which is also
// what a fork does; only the pinned bytes tell the two apart.
//
// The second half re-reads each buffer through the parse dispatch and asserts
// every accessor returns the value that was written — the wire is one map, and
// a reader that disagrees with the writer is as much a fork as different bytes.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/txs.hpp"

#include <string>

using namespace lux::platformvm;
using namespace lux::platformvm::txs;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

// Go goldgen base(): the same envelope under every transaction below.
BaseTx base_fixture() {
    const Id asset = id_of(0x10);
    BaseTx b;
    b.network_id = 96369;
    b.blockchain_id = id_of(0x20);
    b.outs = {
        TransferableOutput{asset, 0,
                           TransferOutput{1'000'000, OutputOwners{7, 2, {short_of(0x30), short_of(0x40), short_of(0x50)}}}},
        TransferableOutput{asset, 999, TransferOutput{42, OutputOwners{0, 1, {short_of(0x30)}}}},
    };
    b.ins = {
        TransferableInput{UtxoId{id_of(0x60), 1}, asset, 999, TransferInput{1'000'042, {0, 1}}},
    };
    const std::string memo = "native-zap";
    b.memo.assign(memo.begin(), memo.end());
    return b;
}

Owner rewards_owner_fixture() { return Owner{3, 1, {short_of(0x30)}}; }

std::vector<TransferableOutput> stake_fixture() {
    return {TransferableOutput{id_of(0x10), 0, TransferOutput{2'000'000, OutputOwners{0, 1, {short_of(0x40)}}}}};
}

signer::ProofOfPossession pop_fixture() {
    signer::ProofOfPossession p{};
    for (std::size_t i = 0; i < signer::kPublicKeyLen; ++i) p.public_key[i] = static_cast<std::uint8_t>(i);
    for (std::size_t i = 0; i < signer::kSignatureLen; ++i)
        p.proof[i] = static_cast<std::uint8_t>(255 - static_cast<int>(i));
    return p;
}

NetworkValidator network_validator_fixture() {
    NetworkValidator nv;
    const NodeId n = node_of(0x96);
    nv.node_id.assign(n.b.begin(), n.b.end());
    nv.weight = 1000;
    nv.balance = 500;
    nv.pop = pop_fixture();
    nv.remaining_balance_owner = PChainOwner{1, {short_of(0x30)}};
    nv.deactivation_owner = PChainOwner{2, {short_of(0x40), short_of(0x50)}};
    return nv;
}

std::vector<std::uint8_t> bytes_of(const std::string& s) { return {s.begin(), s.end()}; }

}  // namespace

// ── the wire: each constructor against the bytes Go printed

TEST(BaseTxWire) {
    auto tx = BaseTxUnsigned::create(base_fixture());
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::base_tx), hex(tx.value()->bytes()));
}

TEST(ImportTxWire) {
    std::vector<TransferableInput> imported = {
        TransferableInput{UtxoId{id_of(0x80), 3}, id_of(0x10), 0, TransferInput{500, {0}}}};
    auto tx = ImportTx::create(base_fixture(), id_of(0x70), imported);
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::import_tx), hex(tx.value()->bytes()));
}

TEST(ExportTxWire) {
    std::vector<TransferableOutput> exported = {
        TransferableOutput{id_of(0x10), 0, TransferOutput{900, OutputOwners{0, 1, {short_of(0x30)}}}}};
    auto tx = ExportTx::create(base_fixture(), id_of(0x71), exported);
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::export_tx), hex(tx.value()->bytes()));
}

TEST(CreateChainTxWire) {
    auto tx = CreateChainTx::create(base_fixture(), id_of(0x72), "My Chain 7", id_of(0x73),
                                    {id_of(0x01), id_of(0x02)}, bytes_of("genesis-bytes"), Auth{0, 2});
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::create_chain_tx), hex(tx.value()->bytes()));
}

TEST(AddValidatorTxWire) {
    auto tx = AddValidatorTx::create(base_fixture(), Validator{node_of(0x90), 100, 200, 2'000'000},
                                     stake_fixture(), rewards_owner_fixture(), 20000);
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::add_validator_tx), hex(tx.value()->bytes()));
}

TEST(AddDelegatorTxWire) {
    auto tx = AddDelegatorTx::create(base_fixture(), Validator{node_of(0x91), 101, 201, 2'000'000},
                                     stake_fixture(), rewards_owner_fixture());
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::add_delegator_tx), hex(tx.value()->bytes()));
}

TEST(AddChainValidatorTxWire) {
    auto tx = AddChainValidatorTx::create(base_fixture(), Validator{node_of(0x92), 102, 202, 7}, id_of(0x74),
                                          Auth{1});
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::add_chain_validator_tx), hex(tx.value()->bytes()));
}

TEST(AddPermissionlessValidatorTxWire) {
    const signer::Signer sig = pop_fixture();
    auto tx = AddPermissionlessValidatorTx::create(base_fixture(), Validator{node_of(0x93), 103, 203, 2'000'000},
                                                   kEmptyId, sig, stake_fixture(), rewards_owner_fixture(),
                                                   rewards_owner_fixture(), 20000);
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::add_permissionless_validator_tx), hex(tx.value()->bytes()));
}

TEST(AddPermissionlessDelegatorTxWire) {
    auto tx = AddPermissionlessDelegatorTx::create(base_fixture(), Validator{node_of(0x94), 104, 204, 2'000'000},
                                                    kEmptyId, stake_fixture(), rewards_owner_fixture());
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::add_permissionless_delegator_tx), hex(tx.value()->bytes()));
}

TEST(RemoveChainValidatorTxWire) {
    auto tx = RemoveChainValidatorTx::create(base_fixture(), node_of(0x95), id_of(0x75), Auth{0});
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::remove_chain_validator_tx), hex(tx.value()->bytes()));
}

TEST(TransferChainOwnershipTxWire) {
    auto tx = TransferChainOwnershipTx::create(base_fixture(), id_of(0x76), Auth{0}, rewards_owner_fixture());
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::transfer_chain_ownership_tx), hex(tx.value()->bytes()));
}

TEST(TransformChainTxWire) {
    TransformChainTx::Params p;
    p.chain = id_of(0x77);
    p.asset_id = id_of(0x78);
    p.initial_supply = 1000;
    p.maximum_supply = 2000;
    p.min_consumption_rate = 10;
    p.max_consumption_rate = 20;
    p.min_validator_stake = 5;
    p.max_validator_stake = 100;
    p.min_stake_duration = 60;
    p.max_stake_duration = 600;
    p.min_delegation_fee = 1000;
    p.min_delegator_stake = 3;
    p.max_validator_weight_factor = 7;
    p.uptime_requirement = 900000;
    auto tx = TransformChainTx::create(base_fixture(), p, Auth{0});
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::transform_chain_tx), hex(tx.value()->bytes()));
}

TEST(RegisterL1ValidatorTxWire) {
    signer::SignatureBytes pop{};
    for (std::size_t i = 0; i < pop.size(); ++i) pop[i] = static_cast<std::uint8_t>(i);
    auto tx = RegisterL1ValidatorTx::create(base_fixture(), 555, pop, bytes_of("warp-message"));
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::register_l1_validator_tx), hex(tx.value()->bytes()));
}

TEST(SetL1ValidatorWeightTxWire) {
    auto tx = SetL1ValidatorWeightTx::create(base_fixture(), bytes_of("weight-message"));
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::set_l1_validator_weight_tx), hex(tx.value()->bytes()));
}

TEST(IncreaseL1ValidatorBalanceTxWire) {
    auto tx = IncreaseL1ValidatorBalanceTx::create(base_fixture(), id_of(0x79), 4242);
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::increase_l1_validator_balance_tx), hex(tx.value()->bytes()));
}

TEST(DisableL1ValidatorTxWire) {
    auto tx = DisableL1ValidatorTx::create(base_fixture(), id_of(0x7a), Auth{3});
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::disable_l1_validator_tx), hex(tx.value()->bytes()));
}

TEST(RewardValidatorTxWire) {
    auto tx = RewardValidatorTx::create(id_of(0x7b));
    REQUIRE_EQ(std::string(pvmgold::reward_validator_tx), hex(tx->bytes()));
}

TEST(CreateNetworkTxWire) {
    const security::Mode sec{false, security::Admission::Open, 2000, security::Manager::Contract};
    auto tx = CreateNetworkTx::create(base_fixture(), kEmptyId, rewards_owner_fixture(), sec,
                                      {network_validator_fixture()}, id_of(0x7c), bytes_of("mgr"));
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::create_network_tx), hex(tx.value()->bytes()));
}

TEST(ConvertNetworkTxWire) {
    const security::Mode sec{false, security::Admission::Open, 1000, security::Manager::Contract};
    auto tx = ConvertNetworkTx::create(base_fixture(), id_of(0x7d), kEmptyId, id_of(0x7e), sec,
                                       bytes_of("0xmgr"), {network_validator_fixture()}, Auth{0});
    REQUIRE_OK(tx);
    REQUIRE_EQ(std::string(pvmgold::convert_network_tx), hex(tx.value()->bytes()));
}

// Go: (*Tx).Initialize — signed bytes are unsigned ‖ creds, and the id is
// sha256 of the whole thing.
TEST(SignedTxWireAndID) {
    auto unsigned_tx = BaseTxUnsigned::create(base_fixture());
    REQUIRE_OK(unsigned_tx);

    std::array<std::uint8_t, kSigLen> s0{}, s1{};
    for (std::size_t i = 0; i < kSigLen; ++i) {
        s0[i] = static_cast<std::uint8_t>(i);
        s1[i] = static_cast<std::uint8_t>(255 - static_cast<int>(i));
    }
    Tx tx;
    tx.unsigned_tx = unsigned_tx.value();
    tx.creds = {Credential{{s0, s1}}, Credential{{s1}}};
    REQUIRE_OK(tx.initialize());

    REQUIRE_EQ(std::string(pvmgold::signed_base_tx), hex(tx.bytes));
    REQUIRE_EQ(std::string(pvmgold::signed_base_tx_id), hex(tx.tx_id));

    // The unsigned buffer is a genuine byte PREFIX of the signed buffer: that
    // is what removes the "which spelling did we sign" question.
    const auto u = unsigned_tx.value()->bytes();
    REQUIRE(tx.bytes.size() > u.size());
    REQUIRE(std::equal(u.begin(), u.end(), tx.bytes.begin()));
}

// ── the reader: parse each buffer back and check every field

TEST(ParseDispatchAndAccessors) {
    // BaseTx envelope, read back through the dispatch.
    {
        auto built = BaseTxUnsigned::create(base_fixture());
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto& tx = parsed.value();
        REQUIRE(tx.unsigned_tx->kind() == Kind::Base);
        REQUIRE(tx.creds.empty());
        const auto* spend = dynamic_cast<const SpendingTx*>(tx.unsigned_tx.get());
        REQUIRE(spend != nullptr);
        const BaseTx want = base_fixture();
        REQUIRE_EQ_NUM(want.network_id, spend->network_id());
        REQUIRE_EQ(want.blockchain_id, spend->blockchain_id());
        REQUIRE_EQ(want.outs, spend->outputs());
        REQUIRE_EQ(want.ins, spend->inputs());
        REQUIRE_EQ(want.memo, spend->memo());
    }
    // Import: source chain and imported inputs.
    {
        std::vector<TransferableInput> imported = {
            TransferableInput{UtxoId{id_of(0x80), 3}, id_of(0x10), 0, TransferInput{500, {0}}}};
        auto built = ImportTx::create(base_fixture(), id_of(0x70), imported);
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const ImportTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x70), tx->source_chain());
        REQUIRE_EQ(imported, tx->imported_inputs());
    }
    // Export: destination chain and exported outputs.
    {
        std::vector<TransferableOutput> exported = {
            TransferableOutput{id_of(0x10), 0, TransferOutput{900, OutputOwners{0, 1, {short_of(0x30)}}}}};
        auto built = ExportTx::create(base_fixture(), id_of(0x71), exported);
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const ExportTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x71), tx->destination_chain());
        REQUIRE_EQ(exported, tx->exported_outputs());
    }
    // CreateChain: every delta field.
    {
        auto built = CreateChainTx::create(base_fixture(), id_of(0x72), "My Chain 7", id_of(0x73),
                                            {id_of(0x01), id_of(0x02)}, bytes_of("genesis-bytes"), Auth{0, 2});
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const CreateChainTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x72), tx->chain_id());
        REQUIRE_EQ(id_of(0x73), tx->vm_id());
        REQUIRE_EQ(std::string("My Chain 7"), tx->blockchain_name());
        REQUIRE_EQ(std::vector<Id>({id_of(0x01), id_of(0x02)}), tx->fx_ids());
        REQUIRE_EQ(bytes_of("genesis-bytes"), tx->genesis_data());
        REQUIRE_EQ(Auth({0, 2}), tx->chain_auth());
    }
    // AddPermissionlessValidator: the staking claim, the key, the two owners.
    {
        const signer::Signer sig = pop_fixture();
        auto built = AddPermissionlessValidatorTx::create(base_fixture(),
                                                          Validator{node_of(0x93), 103, 203, 2'000'000},
                                                          kEmptyId, sig, stake_fixture(),
                                                          rewards_owner_fixture(), rewards_owner_fixture(), 20000);
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const AddPermissionlessValidatorTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(Validator({node_of(0x93), 103, 203, 2'000'000}), tx->validator());
        REQUIRE_EQ(kEmptyId, tx->chain());
        REQUIRE(tx->signer_value() == sig);
        REQUIRE_EQ(stake_fixture(), tx->stake_outs());
        REQUIRE_EQ(rewards_owner_fixture(), tx->validator_rewards_owner());
        REQUIRE_EQ(rewards_owner_fixture(), tx->delegator_rewards_owner());
        REQUIRE_EQ_NUM(20000u, tx->delegation_shares());
    }
    // TransformChain: fifteen fields, all read back.
    {
        TransformChainTx::Params p;
        p.chain = id_of(0x77);
        p.asset_id = id_of(0x78);
        p.initial_supply = 1000;
        p.maximum_supply = 2000;
        p.min_consumption_rate = 10;
        p.max_consumption_rate = 20;
        p.min_validator_stake = 5;
        p.max_validator_stake = 100;
        p.min_stake_duration = 60;
        p.max_stake_duration = 600;
        p.min_delegation_fee = 1000;
        p.min_delegator_stake = 3;
        p.max_validator_weight_factor = 7;
        p.uptime_requirement = 900000;
        auto built = TransformChainTx::create(base_fixture(), p, Auth{0});
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const TransformChainTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(p.chain, tx->chain());
        REQUIRE_EQ(p.asset_id, tx->asset_id());
        REQUIRE_U64(p.initial_supply, tx->initial_supply());
        REQUIRE_U64(p.maximum_supply, tx->maximum_supply());
        REQUIRE_U64(p.min_consumption_rate, tx->min_consumption_rate());
        REQUIRE_U64(p.max_consumption_rate, tx->max_consumption_rate());
        REQUIRE_U64(p.min_validator_stake, tx->min_validator_stake());
        REQUIRE_U64(p.max_validator_stake, tx->max_validator_stake());
        REQUIRE_EQ_NUM(p.min_stake_duration, tx->min_stake_duration());
        REQUIRE_EQ_NUM(p.max_stake_duration, tx->max_stake_duration());
        REQUIRE_EQ_NUM(p.min_delegation_fee, tx->min_delegation_fee());
        REQUIRE_U64(p.min_delegator_stake, tx->min_delegator_stake());
        REQUIRE_EQ_NUM(p.max_validator_weight_factor, tx->max_validator_weight_factor());
        REQUIRE_EQ_NUM(p.uptime_requirement, tx->uptime_requirement());
        REQUIRE_EQ(Auth({0}), tx->chain_auth());
    }
    // CreateNetwork: the security mode and the genesis validator survive.
    {
        const security::Mode sec{false, security::Admission::Open, 2000, security::Manager::Contract};
        auto built = CreateNetworkTx::create(base_fixture(), kEmptyId, rewards_owner_fixture(), sec,
                                              {network_validator_fixture()}, id_of(0x7c), bytes_of("mgr"));
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const CreateNetworkTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(kEmptyId, tx->parent());
        REQUIRE_EQ(rewards_owner_fixture(), tx->owner());
        REQUIRE(tx->security_mode() == sec);
        REQUIRE(tx->sovereign());
        REQUIRE_EQ(std::vector<NetworkValidator>({network_validator_fixture()}), tx->validators());
        REQUIRE_EQ(id_of(0x7c), tx->manager_chain_id());
        REQUIRE_EQ(bytes_of("mgr"), tx->manager_address());
    }
    // ConvertNetwork: the promotion.
    {
        const security::Mode sec{false, security::Admission::Open, 1000, security::Manager::Contract};
        auto built = ConvertNetworkTx::create(base_fixture(), id_of(0x7d), kEmptyId, id_of(0x7e), sec,
                                               bytes_of("0xmgr"), {network_validator_fixture()}, Auth{0});
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const ConvertNetworkTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x7d), tx->network());
        REQUIRE_EQ(kEmptyId, tx->parent());
        REQUIRE_EQ(id_of(0x7e), tx->manager_chain_id());
        REQUIRE_EQ(bytes_of("0xmgr"), tx->manager_address());
        REQUIRE(tx->security_mode() == sec);
        REQUIRE_EQ(std::vector<NetworkValidator>({network_validator_fixture()}), tx->validators());
        REQUIRE_EQ(Auth({0}), tx->auth());
    }
    // The L1 balance transactions and the reward proposal.
    {
        auto built = IncreaseL1ValidatorBalanceTx::create(base_fixture(), id_of(0x79), 4242);
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const IncreaseL1ValidatorBalanceTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x79), tx->validation_id());
        REQUIRE_U64(4242u, tx->balance());
    }
    {
        auto built = DisableL1ValidatorTx::create(base_fixture(), id_of(0x7a), Auth{3});
        REQUIRE_OK(built);
        auto parsed = parse(built.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const DisableL1ValidatorTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x7a), tx->validation_id());
        REQUIRE_EQ(Auth({3}), tx->disable_auth());
    }
    {
        auto built = RegisterL1ValidatorTx::create(base_fixture(), 555, pop_fixture().proof,
                                                    bytes_of("warp-message"));
        // goldgen used popSig[i] = i, not the pop fixture's descending proof.
        signer::SignatureBytes want{};
        for (std::size_t i = 0; i < want.size(); ++i) want[i] = static_cast<std::uint8_t>(i);
        auto built2 = RegisterL1ValidatorTx::create(base_fixture(), 555, want, bytes_of("warp-message"));
        REQUIRE_OK(built);
        REQUIRE_OK(built2);
        auto parsed = parse(built2.value()->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const RegisterL1ValidatorTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_U64(555u, tx->balance());
        REQUIRE(tx->proof_of_possession() == want);
        REQUIRE_EQ(bytes_of("warp-message"), tx->message());
    }
    {
        auto built = RewardValidatorTx::create(id_of(0x7b));
        auto parsed = parse(built->bytes());
        REQUIRE_OK(parsed);
        const auto* tx = dynamic_cast<const RewardValidatorTx*>(parsed.value().unsigned_tx.get());
        REQUIRE(tx != nullptr);
        REQUIRE_EQ(id_of(0x7b), tx->tx_id());
    }
}

// Go: txs.Parse over a signed buffer — the credentials come back exactly.
TEST(ParseSignedRoundTrip) {
    auto unsigned_tx = BaseTxUnsigned::create(base_fixture());
    REQUIRE_OK(unsigned_tx);
    std::array<std::uint8_t, kSigLen> s0{}, s1{};
    for (std::size_t i = 0; i < kSigLen; ++i) {
        s0[i] = static_cast<std::uint8_t>(i);
        s1[i] = static_cast<std::uint8_t>(255 - static_cast<int>(i));
    }
    Tx tx;
    tx.unsigned_tx = unsigned_tx.value();
    tx.creds = {Credential{{s0, s1}}, Credential{{s1}}};
    REQUIRE_OK(tx.initialize());

    auto parsed = parse(tx.bytes);
    REQUIRE_OK(parsed);
    REQUIRE_EQ(tx.tx_id, parsed.value().tx_id);
    REQUIRE_EQ(tx.creds, parsed.value().creds);
    REQUIRE_EQ(hex(tx.unsigned_tx->bytes()), hex(parsed.value().unsigned_tx->bytes()));
}

// Go: txs.parseUnsigned default arm — an unknown kind is refused, not guessed.
TEST(ParseRejectsUnknownKind) {
    auto built = BaseTxUnsigned::create(base_fixture());
    REQUIRE_OK(built);
    std::vector<std::uint8_t> b(built.value()->bytes().begin(), built.value()->bytes().end());
    // The root object's first byte is the kind; 200 names nothing.
    const std::uint32_t root = static_cast<std::uint32_t>(b[8]) | (static_cast<std::uint32_t>(b[9]) << 8) |
                               (static_cast<std::uint32_t>(b[10]) << 16) |
                               (static_cast<std::uint32_t>(b[11]) << 24);
    b[root] = 200;
    REQUIRE_ERR(parse(b), Err::UnknownTxKind);

    // Slot 0 and slot 1 are deliberate holes: a zeroed buffer must not decode.
    b[root] = 0;
    REQUIRE_ERR(parse(b), Err::UnknownTxKind);
    b[root] = 1;
    REQUIRE_ERR(parse(b), Err::UnknownTxKind);
}

// Go: errCredSigsOutOfRange — a credential may not name signatures the buffer
// does not carry.
TEST(ParseRejectsCredRangeOutsideSignatureArray) {
    std::array<std::uint8_t, kSigLen> s0{};
    for (std::size_t i = 0; i < kSigLen; ++i) s0[i] = static_cast<std::uint8_t>(i);
    auto creds = write_creds({Credential{{s0}}});
    REQUIRE_OK(creds);
    auto buf = creds.value();

    // The credential entry is {sigStart u32, sigCount u32}; inflate the count.
    const std::uint32_t root = static_cast<std::uint32_t>(buf[8]) | (static_cast<std::uint32_t>(buf[9]) << 8) |
                               (static_cast<std::uint32_t>(buf[10]) << 16) |
                               (static_cast<std::uint32_t>(buf[11]) << 24);
    const std::int32_t rel = static_cast<std::int32_t>(
        static_cast<std::uint32_t>(buf[root]) | (static_cast<std::uint32_t>(buf[root + 1]) << 8) |
        (static_cast<std::uint32_t>(buf[root + 2]) << 16) | (static_cast<std::uint32_t>(buf[root + 3]) << 24));
    const std::size_t entry = static_cast<std::size_t>(static_cast<std::int64_t>(root) + rel);
    buf[entry + 4] = 0xff;  // sigCount = huge
    buf[entry + 5] = 0xff;
    REQUIRE_ERR(parse_creds(buf), Err::CredSigsOutOfRange);
}

// Go: MarshalOwner / UnmarshalOwner — the one canonical owner encoding.
TEST(MarshalOwnerRoundTrip) {
    const Owner o{11, 2, {short_of(0x30), short_of(0x40)}};
    const auto b = marshal_owner(o);
    REQUIRE_EQ(std::string(pvmgold::marshal_owner), hex(b));
    auto back = unmarshal_owner(b);
    REQUIRE_OK(back);
    REQUIRE_EQ(o, back.value());
}
