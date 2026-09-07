// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// uptime.cpp — the tracker, rendered from Go vms/platformvm/uptime_tracker.go.
//
// Every interval here is whole seconds, because that is the resolution the
// measurement is persisted at: state stores last_updated as a unix second, so
// working at one resolution keeps the arithmetic exact and side-steps clock
// skew that a finer clock would smuggle in.

#include "lux/platformvm/uptime.hpp"

namespace lux::platformvm::uptime {

void Tracker::connect(const NodeId& node_id) {
    std::lock_guard<std::mutex> g(mu_);
    if (connections_.find(node_id) != connections_.end()) return;
    connections_[node_id] = now();
}

bool Tracker::is_connected(const NodeId& node_id) const {
    std::lock_guard<std::mutex> g(mu_);
    return connections_.find(node_id) != connections_.end();
}

Result<bool> Tracker::disconnect(const NodeId& node_id) {
    std::lock_guard<std::mutex> g(mu_);
    // The connection is dropped either way — a peer that is gone is gone
    // whether or not this node was measuring it.
    struct Drop {
        std::map<NodeId, std::uint64_t>& m;
        const NodeId& id;
        ~Drop() { m.erase(id); }
    } drop{connections_, node_id};

    if (!started_tracking_) return false;
    return update_locked(node_id);
}

Status Tracker::start_tracking(const std::vector<NodeId>& node_ids) {
    std::lock_guard<std::mutex> g(mu_);
    if (started_tracking_) return fail(Err::AlreadyStartedTracking);
    for (const auto& node_id : node_ids) {
        auto r = update_locked(node_id);
        if (!r) return std::unexpected(r.error());
    }
    started_tracking_ = true;
    return ok();
}

Status Tracker::stop_tracking(const std::vector<NodeId>& node_ids) {
    std::lock_guard<std::mutex> g(mu_);
    if (!started_tracking_) return fail(Err::NotStartedTracking);
    for (const auto& node_id : node_ids) {
        auto r = update_locked(node_id);
        if (!r) return std::unexpected(r.error());
    }
    started_tracking_ = false;
    return ok();
}

bool Tracker::started_tracking() const {
    std::lock_guard<std::mutex> g(mu_);
    return started_tracking_;
}

Result<std::pair<std::uint64_t, std::uint64_t>> Tracker::calculate_locked(const NodeId& node_id) const {
    auto stored = state_.get_uptime(node_id, chain_id_);
    if (!stored) return std::unexpected(stored.error());
    const std::uint64_t up_duration = stored.value().first;
    const std::uint64_t last_updated = stored.value().second;
    const std::uint64_t n = now();

    // Clock skew: never subtract time or double-count.
    if (n < last_updated) return std::make_pair(up_duration, last_updated);

    // Before tracking, assume the node was online since its last update. This
    // is the branch that keeps a validator that has been in the set from being
    // charged for the window nobody was measuring.
    if (!started_tracking_) return std::make_pair(up_duration + (n - last_updated), n);

    // Tracking, but not connected: offline since its last update. The clock
    // moves forward and the interval is NOT credited, which is why a validator
    // whose connection events never reach the VM is pinned at zero.
    const auto it = connections_.find(node_id);
    if (it == connections_.end()) return std::make_pair(up_duration, n);

    // Tracking and connected: credit from the later of (connect, last_updated)
    // so no interval is counted twice.
    std::uint64_t connected_at = it->second;
    if (connected_at < last_updated) connected_at = last_updated;
    if (n < connected_at) return std::make_pair(up_duration, n);
    return std::make_pair(up_duration + (n - connected_at), n);
}

Result<bool> Tracker::update_locked(const NodeId& node_id) {
    auto computed = calculate_locked(node_id);
    if (!computed) {
        // A node with no record is not a validator: tracking it is harmless and
        // must never dirty the state.
        if (computed.error().code == Err::NotFound) return false;
        return std::unexpected(computed.error());
    }
    if (auto st = state_.set_uptime(node_id, chain_id_, computed.value().first, computed.value().second); !st)
        return std::unexpected(st.error());
    return true;
}

Result<std::pair<std::uint64_t, std::uint64_t>> Tracker::uptime(const NodeId& node_id,
                                                                const Id& chain_id) const {
    if (!(chain_id == chain_id_)) return std::make_pair(std::uint64_t{0}, std::uint64_t{0});

    auto start = state_.get_start_time(node_id, chain_id);
    if (!start) return std::unexpected(start.error());

    Result<std::pair<std::uint64_t, std::uint64_t>> computed = [&] {
        std::lock_guard<std::mutex> g(mu_);
        return calculate_locked(node_id);
    }();
    if (!computed) return std::unexpected(computed.error());

    const std::uint64_t n = now();
    const std::uint64_t total = n > start.value() ? n - start.value() : 0;
    return std::make_pair(computed.value().first, total);
}

Result<double> Tracker::percent(const NodeId& node_id, const Id& chain_id) const {
    if (!(chain_id == chain_id_)) return 0.0;
    auto start = state_.get_start_time(node_id, chain_id);
    if (!start) return std::unexpected(start.error());
    return percent_from(node_id, chain_id, start.value());
}

Result<double> Tracker::percent_from(const NodeId& node_id, const Id& chain_id,
                                     std::uint64_t since) const {
    if (!(chain_id == chain_id_)) return 0.0;

    Result<std::pair<std::uint64_t, std::uint64_t>> computed = [&] {
        std::lock_guard<std::mutex> g(mu_);
        return calculate_locked(node_id);
    }();
    if (!computed) return std::unexpected(computed.error());

    const std::uint64_t n = now();
    if (n <= since) return 1.0;  // nothing to have been up for yet

    const double fraction = static_cast<double>(computed.value().first) / static_cast<double>(n - since);
    return fraction > 1.0 ? 1.0 : fraction;
}

}  // namespace lux::platformvm::uptime
