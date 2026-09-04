// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// complexity.cpp — the fee schedule, and the price curve it feeds.
//
// Rendered from Go vms/platformvm/txs/fee/complexity.go and
// vms/components/gas/gas.go.

#include "lux/platformvm/complexity.hpp"

namespace lux::platformvm::gas {

Price calculate_price(Price min_price, Gas excess, Gas excess_conversion_constant) {
    // The EIP-4844 fake exponential, term by term:
    //   accum = minPrice * K; while accum > 0: out += accum; accum = accum*excess/K/i
    // Every intermediate stays under 2^193, so a 512-bit integer never runs out.
    if (excess_conversion_constant == 0) return min_price;

    const BigUint numerator = BigUint::from_u64(excess);
    const BigUint denominator = BigUint::from_u64(excess_conversion_constant);

    BigUint out;
    BigUint accum = BigUint::from_u64(min_price);
    accum.mul(denominator);

    // The output is divided by the denominator at the end, so anything that
    // reaches denominator * MaxUint64 is already the largest price there is.
    BigUint max_output = denominator;
    max_output.mul_u64(UINT64_MAX);

    std::uint64_t i = 1;
    while (!accum.is_zero()) {
        out.add(accum);
        if (out.cmp(max_output) >= 0) return UINT64_MAX;
        accum.mul(numerator);
        accum.div(denominator);
        accum.div_u64(i);
        ++i;
    }
    out.div(denominator);
    return out.to_u64();
}

}  // namespace lux::platformvm::gas

namespace lux::platformvm::fee {
namespace {

gas::Dimensions dims(std::uint64_t bandwidth, std::uint64_t db_read, std::uint64_t db_write,
                     std::uint64_t compute) {
    gas::Dimensions d;
    d[gas::Bandwidth] = bandwidth;
    d[gas::DBRead] = db_read;
    d[gas::DBWrite] = db_write;
    d[gas::Compute] = compute;
    return d;
}

Result<gas::Dimensions> sum(std::initializer_list<gas::Dimensions> parts) {
    gas::Dimensions total;
    for (const auto& p : parts) {
        auto s = total.add(p);
        if (!s) return std::unexpected(s.error());
        total = s.value();
    }
    return total;
}

// The spending envelope every non-proposal transaction carries.
Result<gas::Dimensions> base_tx_complexity(const txs::SpendingTx& tx) {
    auto outs = output_complexity(tx.outputs());
    if (!outs) return outs;
    auto ins = input_complexity(tx.inputs());
    if (!ins) return ins;
    auto total = outs.value().add(ins.value());
    if (!total) return total;
    auto bandwidth = add64(total.value()[gas::Bandwidth], tx.memo().size());
    if (!bandwidth) return std::unexpected(bandwidth.error());
    auto out = total.value();
    out[gas::Bandwidth] = bandwidth.value();
    return out;
}

}  // namespace

gas::Dimensions intrinsic_base_tx() {
    return dims(kWireVersion + kWireInt + kWireInt + kIdLen + kWireInt + kWireInt + kWireInt + kWireInt, 0, 0,
                0);
}

gas::Dimensions intrinsic_add_chain_validator_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIntrinsicNetValidatorBandwidth + kWireInt + kWireInt, 3,
                3, 0);
}

gas::Dimensions intrinsic_create_chain_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIdLen + kWireShort + kIdLen + kWireInt + kWireInt +
                    kWireInt + kWireInt,
                3, 1, 0);
}

gas::Dimensions intrinsic_create_network_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kWireInt, 0, 1, 0);
}

gas::Dimensions intrinsic_import_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIdLen + kWireInt, 0, 0, 0);
}

gas::Dimensions intrinsic_export_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIdLen + kWireInt, 0, 0, 0);
}

gas::Dimensions intrinsic_remove_chain_validator_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kNodeIdLen + kIdLen + kWireInt + kWireInt, 1, 3, 0);
}

gas::Dimensions intrinsic_add_permissionless_validator_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIntrinsicValidatorBandwidth + kIdLen + kWireInt +
                    kWireInt + kWireInt + kWireInt + kWireInt,
                1, 3, 0);
}

gas::Dimensions intrinsic_add_permissionless_delegator_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIntrinsicValidatorBandwidth + kIdLen + kWireInt +
                    kWireInt,
                1, 2, 0);
}

gas::Dimensions intrinsic_transfer_chain_ownership_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIdLen + kWireInt + kWireInt + kWireInt, 1, 1, 0);
}

gas::Dimensions intrinsic_register_l1_validator_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kWireLong + signer::kSignatureLen + kWireInt, 5, 6,
                kBlsPopVerifyCompute);
}

gas::Dimensions intrinsic_set_l1_validator_weight_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kWireInt, 3, 5, 0);
}

gas::Dimensions intrinsic_increase_l1_validator_balance_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIdLen + kWireLong, 1, 5, 0);
}

gas::Dimensions intrinsic_disable_l1_validator_tx() {
    return dims(intrinsic_base_tx()[gas::Bandwidth] + kIdLen + kWireInt + kWireInt, 1, 6, 0);
}

Result<gas::Dimensions> output_complexity(const std::vector<TransferableOutput>& outs) {
    gas::Dimensions total;
    for (const auto& out : outs) {
        gas::Dimensions c = dims(kIntrinsicOutputBandwidth + kIntrinsicSecpOutputBandwidth, 0,
                                 kIntrinsicOutputDBWrite, 0);
        // A lock is a wrapper on the wire the schedule prices, whether or not
        // this port models it as a field.
        if (out.stake_lock != 0) c[gas::Bandwidth] += kIntrinsicStakeableLockedOutputBandwidth;
        auto addr_bandwidth = mul64(out.out.owners.addrs.size(), kShortIdLen);
        if (!addr_bandwidth) return std::unexpected(addr_bandwidth.error());
        auto b = add64(c[gas::Bandwidth], addr_bandwidth.value());
        if (!b) return std::unexpected(b.error());
        c[gas::Bandwidth] = b.value();
        auto s = total.add(c);
        if (!s) return s;
        total = s.value();
    }
    return total;
}

Result<gas::Dimensions> input_complexity(const std::vector<TransferableInput>& ins) {
    gas::Dimensions total;
    for (const auto& in : ins) {
        gas::Dimensions c = dims(kIntrinsicInputBandwidth + kIntrinsicSecpTransferableInputBandwidth,
                                 kIntrinsicInputDBRead, kIntrinsicInputDBWrite, 0);
        if (in.stake_lock != 0) c[gas::Bandwidth] += kIntrinsicStakeableLockedInputBandwidth;

        const std::uint64_t n = in.in.sig_indices.size();
        auto sig_bandwidth = mul64(n, kIntrinsicSignatureBandwidth);
        if (!sig_bandwidth) return std::unexpected(sig_bandwidth.error());
        auto b = add64(c[gas::Bandwidth], sig_bandwidth.value());
        if (!b) return std::unexpected(b.error());
        c[gas::Bandwidth] = b.value();

        auto compute = mul64(n, kSignatureCompute);
        if (!compute) return std::unexpected(compute.error());
        c[gas::Compute] = compute.value();

        auto s = total.add(c);
        if (!s) return s;
        total = s.value();
    }
    return total;
}

Result<gas::Dimensions> owner_complexity(const txs::Owner& owner) {
    auto addr_bandwidth = mul64(owner.addrs.size(), kShortIdLen);
    if (!addr_bandwidth) return std::unexpected(addr_bandwidth.error());
    auto bandwidth = add64(addr_bandwidth.value(), kIntrinsicOwnersBandwidth);
    if (!bandwidth) return std::unexpected(bandwidth.error());
    return dims(bandwidth.value(), 0, 0, 0);
}

Result<gas::Dimensions> auth_complexity(const txs::Auth& auth) {
    const std::uint64_t n = auth.size();
    auto sig_bandwidth = mul64(n, kIntrinsicSignatureBandwidth);
    if (!sig_bandwidth) return std::unexpected(sig_bandwidth.error());
    auto bandwidth = add64(sig_bandwidth.value(), kIntrinsicSecpInputBandwidth);
    if (!bandwidth) return std::unexpected(bandwidth.error());
    auto compute = mul64(n, kSignatureCompute);
    if (!compute) return std::unexpected(compute.error());
    return dims(bandwidth.value(), 0, 0, compute.value());
}

Result<gas::Dimensions> signer_complexity(const signer::Signer& s) {
    if (signer::has_key(s)) return dims(kIntrinsicPopBandwidth, 0, 0, kBlsPopVerifyCompute);
    return gas::Dimensions{};
}

Result<gas::Dimensions> warp_complexity(std::span<const std::uint8_t> message) {
    auto parsed = warp::Message::parse(message);
    if (!parsed) return std::unexpected(parsed.error());
    auto signers = parsed.value().signature.num_signers();
    if (!signers) return std::unexpected(signers.error());
    auto aggregation = mul64(static_cast<std::uint64_t>(signers.value()), kBlsAggregateCompute);
    if (!aggregation) return std::unexpected(aggregation.error());
    auto compute = add64(aggregation.value(), kBlsVerifyCompute);
    if (!compute) return std::unexpected(compute.error());
    return dims(message.size(), kIntrinsicWarpDBReads, 0, compute.value());
}

Result<gas::Dimensions> tx_complexity(const txs::UnsignedTx& tx) {
    switch (tx.kind()) {
        case txs::Kind::Base: {
            const auto& t = static_cast<const txs::BaseTxUnsigned&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            return sum({intrinsic_base_tx(), base.value()});
        }
        case txs::Kind::Import: {
            const auto& t = static_cast<const txs::ImportTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto imported = input_complexity(t.imported_inputs());
            if (!imported) return imported;
            return sum({intrinsic_import_tx(), base.value(), imported.value()});
        }
        case txs::Kind::Export: {
            const auto& t = static_cast<const txs::ExportTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto exported = output_complexity(t.exported_outputs());
            if (!exported) return exported;
            return sum({intrinsic_export_tx(), base.value(), exported.value()});
        }
        case txs::Kind::CreateChain: {
            const auto& t = static_cast<const txs::CreateChainTx&>(tx);
            auto fx_bandwidth = mul64(t.fx_ids().size(), kIdLen);
            if (!fx_bandwidth) return std::unexpected(fx_bandwidth.error());
            auto b = add64(fx_bandwidth.value(), t.blockchain_name().size());
            if (!b) return std::unexpected(b.error());
            b = add64(b.value(), t.genesis_data().size());
            if (!b) return std::unexpected(b.error());
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto auth = auth_complexity(t.chain_auth());
            if (!auth) return auth;
            return sum({intrinsic_create_chain_tx(), dims(b.value(), 0, 0, 0), base.value(), auth.value()});
        }
        case txs::Kind::CreateNetwork: {
            const auto& t = static_cast<const txs::CreateNetworkTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto owner = owner_complexity(t.owner());
            if (!owner) return owner;
            return sum({intrinsic_create_network_tx(), base.value(), owner.value()});
        }
        case txs::Kind::ConvertNetwork: {
            // No invented intrinsic table: the bandwidth is the byte count of the
            // variable payload, which is its definitional lower bound, plus one
            // staker write per validator.
            const auto& t = static_cast<const txs::ConvertNetworkTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto auth = auth_complexity(t.auth());
            if (!auth) return auth;
            auto total = base.value().add(auth.value());
            if (!total) return total;
            auto out = total.value();
            auto b = add64(out[gas::Bandwidth], t.manager_address().size());
            if (!b) return std::unexpected(b.error());
            out[gas::Bandwidth] = b.value();
            for (const auto& v : t.validators()) {
                const std::uint64_t vb = v.node_id.size() + 2 * kWireLong + signer::kPublicKeyLen +
                                         signer::kSignatureLen +
                                         (v.remaining_balance_owner.addresses.size() +
                                          v.deactivation_owner.addresses.size()) *
                                             kShortIdLen;
                auto nb = add64(out[gas::Bandwidth], vb);
                if (!nb) return std::unexpected(nb.error());
                out[gas::Bandwidth] = nb.value();
                auto nw = add64(out[gas::DBWrite], 1);
                if (!nw) return std::unexpected(nw.error());
                out[gas::DBWrite] = nw.value();
            }
            return out;
        }
        case txs::Kind::AddChainValidator: {
            const auto& t = static_cast<const txs::AddChainValidatorTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto auth = auth_complexity(t.chain_auth());
            if (!auth) return auth;
            return sum({intrinsic_add_chain_validator_tx(), base.value(), auth.value()});
        }
        case txs::Kind::RemoveChainValidator: {
            const auto& t = static_cast<const txs::RemoveChainValidatorTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto auth = auth_complexity(t.chain_auth());
            if (!auth) return auth;
            return sum({intrinsic_remove_chain_validator_tx(), base.value(), auth.value()});
        }
        case txs::Kind::TransferChainOwnership: {
            const auto& t = static_cast<const txs::TransferChainOwnershipTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto auth = auth_complexity(t.chain_auth());
            if (!auth) return auth;
            auto owner = owner_complexity(t.owner());
            if (!owner) return owner;
            return sum({intrinsic_transfer_chain_ownership_tx(), base.value(), auth.value(), owner.value()});
        }
        case txs::Kind::AddPermissionlessValidator: {
            const auto& t = static_cast<const txs::AddPermissionlessValidatorTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto sig = signer_complexity(t.signer_value());
            if (!sig) return sig;
            auto stake = output_complexity(t.stake_outs());
            if (!stake) return stake;
            auto val_owner = owner_complexity(t.validator_rewards_owner());
            if (!val_owner) return val_owner;
            auto del_owner = owner_complexity(t.delegator_rewards_owner());
            if (!del_owner) return del_owner;
            return sum({intrinsic_add_permissionless_validator_tx(), base.value(), sig.value(), stake.value(),
                        val_owner.value(), del_owner.value()});
        }
        case txs::Kind::AddPermissionlessDelegator: {
            const auto& t = static_cast<const txs::AddPermissionlessDelegatorTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto owner = owner_complexity(t.delegation_rewards_owner());
            if (!owner) return owner;
            auto stake = output_complexity(t.stake_outs());
            if (!stake) return stake;
            return sum({intrinsic_add_permissionless_delegator_tx(), base.value(), owner.value(),
                        stake.value()});
        }
        case txs::Kind::IncreaseL1ValidatorBalance: {
            const auto& t = static_cast<const txs::IncreaseL1ValidatorBalanceTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            return sum({intrinsic_increase_l1_validator_balance_tx(), base.value()});
        }
        case txs::Kind::DisableL1Validator: {
            const auto& t = static_cast<const txs::DisableL1ValidatorTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto auth = auth_complexity(t.disable_auth());
            if (!auth) return auth;
            return sum({intrinsic_disable_l1_validator_tx(), base.value(), auth.value()});
        }

        // The two warp-carrying transactions price the message they carry, and
        // pricing it means parsing it: every signer is an aggregation every node
        // performs, so a fee that does not count them does not count the work.
        case txs::Kind::RegisterL1Validator: {
            const auto& t = static_cast<const txs::RegisterL1ValidatorTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto w = warp_complexity(t.message());
            if (!w) return w;
            return sum({intrinsic_register_l1_validator_tx(), base.value(), w.value()});
        }
        case txs::Kind::SetL1ValidatorWeight: {
            const auto& t = static_cast<const txs::SetL1ValidatorWeightTx&>(tx);
            auto base = base_tx_complexity(t);
            if (!base) return base;
            auto w = warp_complexity(t.message());
            if (!w) return w;
            return sum({intrinsic_set_l1_validator_weight_tx(), base.value(), w.value()});
        }

        // The legacy staker transactions and the chain's own transaction carry
        // no price: nobody submitted them, so nobody pays.
        case txs::Kind::AddValidator:
        case txs::Kind::AddDelegator:
        case txs::Kind::TransformChain:
        case txs::Kind::RewardValidator:
            return fail(Err::UnsupportedTx);
    }
    return fail(Err::UnsupportedTx);
}

Result<gas::Dimensions> tx_complexity(const std::vector<const txs::UnsignedTx*>& list) {
    gas::Dimensions total;
    for (const auto* tx : list) {
        auto c = tx_complexity(*tx);
        if (!c) return c;
        auto s = total.add(c.value());
        if (!s) return s;
        total = s.value();
    }
    return total;
}

}  // namespace lux::platformvm::fee
