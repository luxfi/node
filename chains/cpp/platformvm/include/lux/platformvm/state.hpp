// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state.hpp — the validator set, and everything the chain remembers about it.
//
// Rendered from Go vms/platformvm/state (staker.go, stakers.go,
// staker_diff_iterator.go, diff.go, state.go). This is where the P-chain stops
// being a transaction format and becomes a set of validators, so it is the part
// whose ORDER is consensus: two nodes that walk the staker set differently
// disagree about who validates, which is a fork with no bytes to blame.
//
// A staker is ordered by when it next moves, then by priority, then by the id of
// the transaction that created it. The priority order is the whole reason the
// second key exists: permissioned stakers leave by the clock and permissionless
// ones leave by being paid, so they must never interleave.
//
// TIME IS SECONDS. Go carries a time.Time and compares it; the wire has always
// carried a Unix second, so that is what a staker holds here. There is one
// representation, so there is nothing to convert and nothing to round.
//
// DECOMPLECTED FROM THE REFERENCE: Go's iterators are lazy trees merged with
// filters. Every one of them is collected into a slice before it is used
// (`iterator.ToSlice`, or a copy taken because "it is not safe to modify the
// state while iterating over it"). So an iterator here IS the ordered slice: the
// same order, the same values, and no way to hold one open across a mutation.

#pragma once

#include "lux/platformvm/block.hpp"
#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/gas.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/l1.hpp"
#include "lux/platformvm/priority.hpp"
#include "lux/platformvm/signer.hpp"
#include "lux/platformvm/status.hpp"
#include "lux/platformvm/txs.hpp"
#include "lux/core/store.hpp"

#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <set>
#include <unordered_map>
#include <vector>

namespace lux::platformvm {
// The store belongs to the node: one durable map, shared with every chain.
namespace store = lux::core::store;
}  // namespace lux::platformvm

namespace lux::platformvm::state {

// Priority lives with the transactions that assign it; a staker only carries one.
using txs::Priority;

// ── the staker

// Everything needed to represent one validator or delegator in a staker set.
struct Staker {
    Id tx_id{};
    NodeId node_id{};
    std::optional<signer::PublicKeyBytes> public_key;
    Id chain_id{};
    std::uint64_t weight = 0;
    std::uint64_t start_time = 0;
    std::uint64_t end_time = 0;
    std::uint64_t potential_reward = 0;

    // When this staker next moves between sets: its start time while pending,
    // its end time while current.
    std::uint64_t next_time = 0;

    // How ties at the same next_time break. The groups and their order are in
    // priority.hpp; the ordering is consensus.
    Priority priority = Priority::PrimaryNetworkValidatorCurrent;

    friend bool operator==(const Staker&, const Staker&) = default;

    // Go: (*Staker).Less — next_time, then priority, then tx id.
    bool less(const Staker& than) const {
        if (next_time != than.next_time) return next_time < than.next_time;
        if (priority != than.priority)
            return static_cast<std::uint8_t>(priority) < static_cast<std::uint8_t>(than.priority);
        return tx_id < than.tx_id;
    }
};

struct StakerLess {
    bool operator()(const Staker& a, const Staker& b) const { return a.less(b); }
};

// An ordered staker set. std::set under StakerLess is the same total order the
// reference's btree keys on.
using StakerTree = std::set<Staker, StakerLess>;

// The ordered walk of a staker set. It is a value: the order is fixed when it is
// made, so nothing can mutate the set underneath it.
using StakerList = std::vector<Staker>;

// Go: state.NewCurrentStaker — a staker entering the current set.
Result<Staker> new_current_staker(const Id& tx_id, const txs::StakerView& view, std::uint64_t start_time,
                                  std::uint64_t potential_reward);

// Go: state.NewPendingStaker — a staker entering the pending set. Only a
// scheduled staker (one with a start time of its own) can be pending.
Result<Staker> new_pending_staker(const Id& tx_id, const txs::StakerView& view);

// ── the staker-diff walk
//
// Go: state.StakerDiffIterator. Walks the events that will happen to the current
// staker set as the clock advances: a pending staker is added, a current staker
// is removed. Order: by next_time; ties add before they remove; further ties by
// Staker::less. The added staker is immediately re-queued for its own removal at
// its end time, which is why the walk is over a MUTABLE current queue.
// Go: state.mutableStakerIterator. An ordered walk over a staker list that can
// have elements PUSHED INTO IT mid-walk and still deliver them in order. The
// diff walk needs exactly this, because adding a pending staker to the current
// set immediately schedules its own later removal.
class MutableStakerWalk {
  public:
    explicit MutableStakerWalk(StakerList source);

    // Must be called once before add().
    bool next();
    const Staker& value() const { return heap_.front(); }
    void add(const Staker& s);
    void release();

  private:
    // A min-heap under Staker::less, so the comparator is the reverse.
    struct Greater {
        bool operator()(const Staker& a, const Staker& b) const { return b.less(a); }
    };
    StakerList source_;
    std::size_t at_ = 0;
    std::vector<Staker> heap_;
};

class StakerDiffWalk {
  public:
    StakerDiffWalk(StakerList current, StakerList pending);

    bool next();
    // The staker that changes, and whether it is being ADDED to the current set.
    const Staker& value() const { return modified_; }
    bool is_added() const { return is_added_; }
    void release();

  private:
    void advance_current();
    void advance_pending();

    bool current_exhausted_ = false;
    MutableStakerWalk current_;

    bool pending_exhausted_ = false;
    StakerList pending_;
    std::size_t pending_at_ = 0;

    Staker modified_{};
    bool is_added_ = false;
};

// ── the base staker set

// Whether a validator changed in a diff. Delegator changes do not move this:
// "unmodified" means the VALIDATOR is unchanged, not that nothing happened.
enum class DiffStatus : std::uint8_t { Unmodified = 0, Added = 1, Deleted = 2 };

// The weight one node's validator entry moved by, over one diff.
struct WeightDiff {
    bool decrease = false;
    std::uint64_t amount = 0;

    Status add(std::uint64_t w);
    Status sub(std::uint64_t w);
    Status add_or_sub(bool sub, std::uint64_t amount);
    friend bool operator==(const WeightDiff&, const WeightDiff&) = default;
};

struct ValidatorDiff {
    DiffStatus status = DiffStatus::Unmodified;
    std::optional<Staker> validator;
    StakerTree added_delegators;
    std::map<Id, Staker> deleted_delegators;

    Result<WeightDiff> weight_diff() const;
};

// The materialised staker set: who validates what, and the ordered walk of every
// staker in it.
class BaseStakers {
  public:
    Result<Staker> get_validator(const Id& chain_id, const NodeId& node_id) const;
    void put_validator(const Staker& s);
    void delete_validator(const Staker& s);

    StakerList delegator_list(const Id& chain_id, const NodeId& node_id) const;
    void put_delegator(const Staker& s);
    void delete_delegator(const Staker& s);

    StakerList staker_list() const;

    // Loading from disk records no diff: a diff describes a CHANGE, and reading
    // what is already there is not one.
    void load_validator(const Staker& s);
    void load_delegator(const Staker& s);

    const std::map<Id, std::map<NodeId, ValidatorDiff>>& validator_diffs() const { return diffs_; }
    void clear_diffs() { diffs_.clear(); }
    bool empty() const { return validators_.empty(); }

  private:
    struct Entry {
        std::optional<Staker> validator;
        StakerTree delegators;
    };
    ValidatorDiff& diff_for(const Id& chain_id, const NodeId& node_id);
    Entry& entry_for(const Id& chain_id, const NodeId& node_id);
    void prune(const Id& chain_id, const NodeId& node_id);

    std::map<Id, std::map<NodeId, Entry>> validators_;
    StakerTree stakers_;
    std::map<Id, std::map<NodeId, ValidatorDiff>> diffs_;
};

// The staker changes one diff layer holds, over a parent set.
class DiffStakers {
  public:
    // The validator this layer says, and whether the layer says anything.
    std::pair<std::optional<Staker>, DiffStatus> get_validator(const Id& chain_id, const NodeId& node_id) const;
    Status put_validator(const Staker& s);
    void delete_validator(const Staker& s);

    StakerList delegator_list(StakerList parent, const Id& chain_id, const NodeId& node_id) const;
    void put_delegator(const Staker& s);
    void delete_delegator(const Staker& s);

    StakerList staker_list(StakerList parent) const;

    const std::map<Id, std::map<NodeId, ValidatorDiff>>& validator_diffs() const { return diffs_; }

  private:
    ValidatorDiff& diff_for(const Id& chain_id, const NodeId& node_id);

    std::map<Id, std::map<NodeId, ValidatorDiff>> diffs_;
    StakerTree added_;
    std::map<Id, Staker> deleted_;
};

// ── what a network became, once it was promoted

struct NetToL1Conversion {
    Id chain_id{};
    std::vector<std::uint8_t> addr;
    Id validation_id{};
};

// ── the chain's view of its own state

// The read-and-write surface every executor works against. A Diff and the
// materialised state both answer it, which is what lets a block be verified
// against a layer that is thrown away if the block is not accepted.
class Chain {
  public:
    virtual ~Chain() = default;

    // time and the fee state
    virtual std::uint64_t timestamp() const = 0;
    virtual void set_timestamp(std::uint64_t t) = 0;
    virtual std::uint64_t accrued_fees() const = 0;
    virtual void set_accrued_fees(std::uint64_t f) = 0;
    // The chain's LP-103 position: what it may still spend, and how far above
    // target it has been running. The price follows the excess.
    virtual gas::State fee_state() const = 0;
    virtual void set_fee_state(const gas::State& f) = 0;
    // The LP-77 continuous-fee position for L1 validators: how far above target
    // their number has been running, and what has accrued so far.
    virtual std::uint64_t l1_validator_excess() const = 0;
    virtual void set_l1_validator_excess(std::uint64_t e) = 0;

    // supply, per network
    virtual Result<std::uint64_t> current_supply(const Id& chain_id) const = 0;
    virtual void set_current_supply(const Id& chain_id, std::uint64_t s) = 0;

    // UTXOs
    virtual Result<UTXO> get_utxo(const Id& utxo_id) const = 0;
    virtual void add_utxo(const UTXO& u) = 0;
    virtual void delete_utxo(const Id& utxo_id) = 0;
    virtual void add_reward_utxo(const Id& tx_id, const UTXO& u) = 0;
    virtual std::vector<UTXO> reward_utxos(const Id& tx_id) const = 0;

    // Every unspent output, in id order. The whole set, because a commitment to
    // "the state this block produced" that omits the money is a commitment a
    // node can forge balances underneath.
    virtual std::vector<UTXO> utxos() const = 0;

    // the current staker set
    virtual Result<Staker> get_current_validator(const Id& chain_id, const NodeId& node_id) const = 0;
    virtual Status put_current_validator(const Staker& s) = 0;
    virtual void delete_current_validator(const Staker& s) = 0;
    virtual void put_current_delegator(const Staker& s) = 0;
    virtual void delete_current_delegator(const Staker& s) = 0;
    virtual StakerList current_delegators(const Id& chain_id, const NodeId& node_id) const = 0;
    virtual StakerList current_stakers() const = 0;

    // the pending staker set
    virtual Result<Staker> get_pending_validator(const Id& chain_id, const NodeId& node_id) const = 0;
    virtual Status put_pending_validator(const Staker& s) = 0;
    virtual void delete_pending_validator(const Staker& s) = 0;
    virtual void put_pending_delegator(const Staker& s) = 0;
    virtual void delete_pending_delegator(const Staker& s) = 0;
    virtual StakerList pending_delegators(const Id& chain_id, const NodeId& node_id) const = 0;
    virtual StakerList pending_stakers() const = 0;

    // rewards a validator has accrued from its delegators, paid when it leaves
    virtual Result<std::uint64_t> delegatee_reward(const Id& chain_id, const NodeId& node_id) const = 0;
    virtual Status set_delegatee_reward(const Id& chain_id, const NodeId& node_id, std::uint64_t amount) = 0;

    // networks and chains
    virtual void add_network(const Id& network_id) = 0;
    virtual bool has_network(const Id& network_id) const = 0;
    virtual Result<txs::Owner> network_owner(const Id& network_id) const = 0;
    virtual void set_network_owner(const Id& network_id, const txs::Owner& o) = 0;
    virtual Result<NetToL1Conversion> network_conversion(const Id& network_id) const = 0;
    virtual void set_network_conversion(const Id& network_id, const NetToL1Conversion& c) = 0;
    virtual Result<txs::Tx> network_transformation(const Id& network_id) const = 0;
    virtual void add_network_transformation(const txs::Tx& tx) = 0;
    virtual void add_chain(const txs::Tx& create_chain_tx) = 0;
    virtual std::vector<txs::Tx> chains(const Id& network_id) const = 0;
    virtual std::vector<Id> networks() const = 0;
    virtual bool chain_name_taken(const std::string& name) const = 0;

    // transactions, by id
    virtual Result<std::pair<txs::Tx, status::Status>> get_tx(const Id& tx_id) const = 0;
    virtual void add_tx(const txs::Tx& tx, status::Status s) = 0;

    // ── L1 validators (Go: state.L1Validators)
    //
    // A weight of zero REMOVES; an EndAccumulatedFee of zero deactivates. The
    // active walk is in increasing EndAccumulatedFee, so advancing the clock
    // deactivates exactly the prefix that can no longer pay.
    virtual Result<l1::Validator> get_l1_validator(const Id& validation_id) const = 0;
    virtual bool has_l1_validator(const Id& chain_id, const NodeId& node_id) const = 0;
    virtual Status put_l1_validator(const l1::Validator& v) = 0;
    virtual std::vector<l1::Validator> active_l1_validators() const = 0;
    // Every L1 validator of one network, active and inactive alike, in name
    // order. An inactive one holds weight it cannot vote with, which is exactly
    // why the set has to name it: weight nobody can vote with still sits in the
    // denominator of every quorum.
    virtual std::vector<l1::Validator> l1_validators(const Id& chain_id) const = 0;
    virtual std::size_t num_active_l1_validators() const = 0;
    virtual Result<std::uint64_t> weight_of_l1_validators(const Id& chain_id) const = 0;

    // ── expiries (Go: state.Expiry)
    //
    // Registration messages that may still be issued, and the moment after
    // which they may not. Walked in time order, so the clock drops a prefix.
    virtual std::vector<l1::ExpiryEntry> expiries() const = 0;
    virtual bool has_expiry(const l1::ExpiryEntry& e) const = 0;
    virtual void put_expiry(const l1::ExpiryEntry& e) = 0;
    virtual void delete_expiry(const l1::ExpiryEntry& e) = 0;
};

// The materialised state: what the chain remembers once a block is accepted,
// and — through the store beneath it — what it still remembers after the
// process that accepted it is gone.
//
// The store is the node's one store (lux/core/store.hpp). A State built with no
// store gets a Memory of its OWN, not a shared one: two chains that
// accidentally share a store are one chain with two opinions.
//
// The maps here are the whole state. `load` reads them out of the store on
// boot; `commit` writes them back, and is the ONE durability point — a block is
// accepted when its changes are in the store, not when they are in these maps,
// because only the store survives a restart.
class State final : public Chain {
  public:
    State() : own_(std::make_unique<store::Memory>()), store_(own_.get()) {}
    explicit State(store::Store& store) : store_(&store) {}

    // load rebuilds this state from the store. It is what a boot does, and the
    // only read of the store's whole contents.
    Status load();

    // commit makes everything accepted since the last commit durable.
    //
    // It writes the WHOLE state and erases whatever is no longer in it, rather
    // than a set of deltas gathered by each mutator. That is deliberate: a
    // delta scheme is only as durable as its least-remembered mutator, and a
    // forgotten one is a row that silently stops surviving. It costs a walk of
    // the state per commit, which is the walk state_root already does on every
    // block — so the order of growth is one the chain was paying anyway. Only
    // the rows that actually differ reach the disk.
    Status commit();

    std::uint64_t timestamp() const override { return timestamp_; }
    void set_timestamp(std::uint64_t t) override { timestamp_ = t; }
    std::uint64_t accrued_fees() const override { return accrued_fees_; }
    void set_accrued_fees(std::uint64_t f) override { accrued_fees_ = f; }
    gas::State fee_state() const override { return fee_state_; }
    void set_fee_state(const gas::State& f) override { fee_state_ = f; }
    std::uint64_t l1_validator_excess() const override { return l1_excess_; }
    void set_l1_validator_excess(std::uint64_t e) override { l1_excess_ = e; }

    Result<std::uint64_t> current_supply(const Id& chain_id) const override;
    void set_current_supply(const Id& chain_id, std::uint64_t s) override { supply_[chain_id] = s; }

    Result<UTXO> get_utxo(const Id& utxo_id) const override;
    void add_utxo(const UTXO& u) override { utxos_[u.id()] = u; }
    void delete_utxo(const Id& utxo_id) override { utxos_.erase(utxo_id); }
    void add_reward_utxo(const Id& tx_id, const UTXO& u) override { reward_utxos_[tx_id].push_back(u); }
    std::vector<UTXO> reward_utxos(const Id& tx_id) const override;
    std::vector<UTXO> utxos() const override;

    Result<Staker> get_current_validator(const Id& chain_id, const NodeId& node_id) const override {
        return current_.get_validator(chain_id, node_id);
    }
    // Putting a current validator is what CREATES its metadata: the delegatee
    // reward ledger exists for a validator that is in the set, and for no one
    // else. Go writes the metadata record here for the same reason.
    Status put_current_validator(const Staker& s) override {
        current_.put_validator(s);
        auto& nodes = delegatee_rewards_[s.chain_id];
        nodes.emplace(s.node_id, std::uint64_t{0});
        return ok();
    }
    void delete_current_validator(const Staker& s) override {
        current_.delete_validator(s);
        auto chain = delegatee_rewards_.find(s.chain_id);
        if (chain != delegatee_rewards_.end()) {
            chain->second.erase(s.node_id);
            if (chain->second.empty()) delegatee_rewards_.erase(chain);
        }
    }
    void put_current_delegator(const Staker& s) override { current_.put_delegator(s); }
    void delete_current_delegator(const Staker& s) override { current_.delete_delegator(s); }
    StakerList current_delegators(const Id& c, const NodeId& n) const override {
        return current_.delegator_list(c, n);
    }
    StakerList current_stakers() const override { return current_.staker_list(); }

    Result<Staker> get_pending_validator(const Id& chain_id, const NodeId& node_id) const override {
        return pending_.get_validator(chain_id, node_id);
    }
    Status put_pending_validator(const Staker& s) override {
        pending_.put_validator(s);
        return ok();
    }
    void delete_pending_validator(const Staker& s) override { pending_.delete_validator(s); }
    void put_pending_delegator(const Staker& s) override { pending_.put_delegator(s); }
    void delete_pending_delegator(const Staker& s) override { pending_.delete_delegator(s); }
    StakerList pending_delegators(const Id& c, const NodeId& n) const override {
        return pending_.delegator_list(c, n);
    }
    StakerList pending_stakers() const override { return pending_.staker_list(); }

    Result<std::uint64_t> delegatee_reward(const Id& chain_id, const NodeId& node_id) const override;
    Status set_delegatee_reward(const Id& chain_id, const NodeId& node_id, std::uint64_t amount) override;

    void add_network(const Id& network_id) override { networks_.insert(network_id); }
    bool has_network(const Id& network_id) const override { return networks_.count(network_id) != 0; }
    Result<txs::Owner> network_owner(const Id& network_id) const override;
    void set_network_owner(const Id& network_id, const txs::Owner& o) override { net_owners_[network_id] = o; }
    Result<NetToL1Conversion> network_conversion(const Id& network_id) const override;
    void set_network_conversion(const Id& network_id, const NetToL1Conversion& c) override {
        conversions_[network_id] = c;
    }
    Result<txs::Tx> network_transformation(const Id& network_id) const override;
    void add_network_transformation(const txs::Tx& tx) override;
    void add_chain(const txs::Tx& create_chain_tx) override;
    std::vector<txs::Tx> chains(const Id& network_id) const override;
    std::vector<Id> networks() const override { return {networks_.begin(), networks_.end()}; }
    bool chain_name_taken(const std::string& name) const override { return chain_names_.count(name) != 0; }

    Result<std::pair<txs::Tx, status::Status>> get_tx(const Id& tx_id) const override;
    void add_tx(const txs::Tx& tx, status::Status s) override { txs_.insert_or_assign(tx.tx_id, std::make_pair(tx, s)); }

    Result<l1::Validator> get_l1_validator(const Id& validation_id) const override;
    bool has_l1_validator(const Id& chain_id, const NodeId& node_id) const override;
    Status put_l1_validator(const l1::Validator& v) override;
    std::vector<l1::Validator> active_l1_validators() const override;
    std::vector<l1::Validator> l1_validators(const Id& chain_id) const override;
    std::size_t num_active_l1_validators() const override;
    Result<std::uint64_t> weight_of_l1_validators(const Id& chain_id) const override;

    std::vector<l1::ExpiryEntry> expiries() const override { return {expiries_.begin(), expiries_.end()}; }
    bool has_expiry(const l1::ExpiryEntry& e) const override { return expiries_.count(e) != 0; }
    void put_expiry(const l1::ExpiryEntry& e) override { expiries_.insert(e); }
    void delete_expiry(const l1::ExpiryEntry& e) override { expiries_.erase(e); }

    // Loading a genesis or a snapshot: no diff is recorded.
    void load_current_validator(const Staker& s) {
        current_.load_validator(s);
        delegatee_rewards_[s.chain_id].emplace(s.node_id, std::uint64_t{0});
    }
    void load_current_delegator(const Staker& s) { current_.load_delegator(s); }
    void load_pending_validator(const Staker& s) { pending_.load_validator(s); }
    void load_pending_delegator(const Staker& s) { pending_.load_delegator(s); }

  private:
    // rows renders the whole state as the store's byte-keyed map. It is the one
    // place the on-disk shape is spelled, and `load` is its inverse.
    std::map<Bytes, Bytes> rows() const;

    std::unique_ptr<store::Memory> own_;
    store::Store* store_ = nullptr;

    std::uint64_t timestamp_ = 0;
    std::uint64_t accrued_fees_ = 0;
    gas::State fee_state_{};
    std::uint64_t l1_excess_ = 0;
    std::map<Id, std::uint64_t> supply_;
    std::map<Id, UTXO> utxos_;
    std::map<Id, std::vector<UTXO>> reward_utxos_;
    BaseStakers current_;
    BaseStakers pending_;
    std::map<Id, l1::Validator> l1_validators_;
    std::set<l1::ExpiryEntry, l1::ExpiryLess> expiries_;
    std::map<Id, std::map<NodeId, std::uint64_t>> delegatee_rewards_;
    std::set<Id> networks_;
    std::map<Id, txs::Owner> net_owners_;
    std::map<Id, NetToL1Conversion> conversions_;
    std::map<Id, txs::Tx> transformations_;
    std::map<Id, std::vector<txs::Tx>> chains_;
    std::set<std::string> chain_names_;
    std::map<Id, std::pair<txs::Tx, status::Status>> txs_;
};

// A layer of pending changes over a parent Chain. A block is verified against
// one of these; if the block is not accepted, the layer is dropped and the
// parent never knew.
class Diff final : public Chain {
  public:
    explicit Diff(Chain* parent)
        : parent_(parent), timestamp_(parent->timestamp()), accrued_fees_(parent->accrued_fees()),
          fee_state_(parent->fee_state()), l1_excess_(parent->l1_validator_excess()) {}

    // Write everything this layer holds into the target. Go: Diff.Apply.
    Status apply(Chain& target) const;

    std::uint64_t timestamp() const override { return timestamp_; }
    void set_timestamp(std::uint64_t t) override { timestamp_ = t; }
    std::uint64_t accrued_fees() const override { return accrued_fees_; }
    void set_accrued_fees(std::uint64_t f) override { accrued_fees_ = f; }
    gas::State fee_state() const override { return fee_state_; }
    void set_fee_state(const gas::State& f) override { fee_state_ = f; }
    std::uint64_t l1_validator_excess() const override { return l1_excess_; }
    void set_l1_validator_excess(std::uint64_t e) override { l1_excess_ = e; }

    Result<std::uint64_t> current_supply(const Id& chain_id) const override;
    void set_current_supply(const Id& chain_id, std::uint64_t s) override { supply_[chain_id] = s; }

    Result<UTXO> get_utxo(const Id& utxo_id) const override;
    void add_utxo(const UTXO& u) override;
    void delete_utxo(const Id& utxo_id) override;
    void add_reward_utxo(const Id& tx_id, const UTXO& u) override { reward_utxos_[tx_id].push_back(u); }
    std::vector<UTXO> reward_utxos(const Id& tx_id) const override;
    std::vector<UTXO> utxos() const override;

    Result<Staker> get_current_validator(const Id& chain_id, const NodeId& node_id) const override;
    Status put_current_validator(const Staker& s) override { return current_.put_validator(s); }
    void delete_current_validator(const Staker& s) override { current_.delete_validator(s); }
    void put_current_delegator(const Staker& s) override { current_.put_delegator(s); }
    void delete_current_delegator(const Staker& s) override { current_.delete_delegator(s); }
    StakerList current_delegators(const Id& c, const NodeId& n) const override;
    StakerList current_stakers() const override;

    Result<Staker> get_pending_validator(const Id& chain_id, const NodeId& node_id) const override;
    Status put_pending_validator(const Staker& s) override { return pending_.put_validator(s); }
    void delete_pending_validator(const Staker& s) override { pending_.delete_validator(s); }
    void put_pending_delegator(const Staker& s) override { pending_.put_delegator(s); }
    void delete_pending_delegator(const Staker& s) override { pending_.delete_delegator(s); }
    StakerList pending_delegators(const Id& c, const NodeId& n) const override;
    StakerList pending_stakers() const override;

    Result<std::uint64_t> delegatee_reward(const Id& chain_id, const NodeId& node_id) const override;
    Status set_delegatee_reward(const Id& chain_id, const NodeId& node_id, std::uint64_t amount) override;

    void add_network(const Id& network_id) override { added_networks_.insert(network_id); }
    bool has_network(const Id& network_id) const override;
    Result<txs::Owner> network_owner(const Id& network_id) const override;
    void set_network_owner(const Id& network_id, const txs::Owner& o) override { net_owners_[network_id] = o; }
    Result<NetToL1Conversion> network_conversion(const Id& network_id) const override;
    void set_network_conversion(const Id& network_id, const NetToL1Conversion& c) override {
        conversions_[network_id] = c;
    }
    Result<txs::Tx> network_transformation(const Id& network_id) const override;
    void add_network_transformation(const txs::Tx& tx) override;
    void add_chain(const txs::Tx& create_chain_tx) override;
    std::vector<txs::Tx> chains(const Id& network_id) const override;
    std::vector<Id> networks() const override;
    bool chain_name_taken(const std::string& name) const override;

    Result<std::pair<txs::Tx, status::Status>> get_tx(const Id& tx_id) const override;
    void add_tx(const txs::Tx& tx, status::Status s) override { added_txs_.insert_or_assign(tx.tx_id, std::make_pair(tx, s)); }

    Result<l1::Validator> get_l1_validator(const Id& validation_id) const override;
    bool has_l1_validator(const Id& chain_id, const NodeId& node_id) const override;
    Status put_l1_validator(const l1::Validator& v) override;
    std::vector<l1::Validator> active_l1_validators() const override;
    std::vector<l1::Validator> l1_validators(const Id& chain_id) const override;
    std::size_t num_active_l1_validators() const override;
    Result<std::uint64_t> weight_of_l1_validators(const Id& chain_id) const override;

    std::vector<l1::ExpiryEntry> expiries() const override;
    bool has_expiry(const l1::ExpiryEntry& e) const override;
    void put_expiry(const l1::ExpiryEntry& e) override;
    void delete_expiry(const l1::ExpiryEntry& e) override;

    // What this layer changed about the validator sets. A node that has to
    // answer "who validated at height H" reads these, because the set at H is
    // the set now with every change since undone.
    const std::map<Id, std::map<NodeId, ValidatorDiff>>& current_validator_diffs() const {
        return current_.validator_diffs();
    }
    const std::map<Id, l1::Validator>& l1_changes() const { return l1_validators_; }

  private:
    Chain* parent_;
    std::uint64_t timestamp_ = 0;
    std::uint64_t accrued_fees_ = 0;
    gas::State fee_state_{};
    std::uint64_t l1_excess_ = 0;
    std::map<Id, l1::Validator> l1_validators_;
    std::map<l1::ExpiryEntry, bool, l1::ExpiryLess> expiry_diff_;  // true = added
    std::map<Id, std::uint64_t> supply_;
    std::map<Id, UTXO> added_utxos_;
    std::set<Id> deleted_utxos_;
    std::map<Id, std::vector<UTXO>> reward_utxos_;
    DiffStakers current_;
    DiffStakers pending_;
    std::map<Id, std::map<NodeId, std::uint64_t>> delegatee_rewards_;
    std::set<Id> added_networks_;
    std::map<Id, txs::Owner> net_owners_;
    std::map<Id, NetToL1Conversion> conversions_;
    std::map<Id, txs::Tx> transformations_;
    std::map<Id, std::vector<txs::Tx>> chains_;
    std::set<std::string> chain_names_;
    std::map<Id, std::pair<txs::Tx, status::Status>> added_txs_;
};

// The next moment the staker set changes: the earliest of the next current
// staker's end and the next pending staker's start. Rendered from
// state.GetNextStakerChangeTime, with the L1 fee arm left to the caller that
// has a fee config. `upper` bounds the answer.
std::uint64_t next_staker_change_time(const Chain& chain, std::uint64_t upper);

// The commitment a block carries: sha256 over a canonical walk of the state its
// execution produced — the clock, the accrued fees, the supply of every network,
// the current and pending staker sets in their consensus order, every unspent
// output in id order, and every network with its owner.
//
// It is the whole state rather than an incremental root, which costs a walk per
// block. That is the honest trade at this size: a validator signs what it
// executed, and a commitment that skips a part of the state is a part a node can
// forge underneath.
Id state_root(const Chain& chain);

}  // namespace lux::platformvm::state
