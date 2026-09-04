// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire_test.cpp — the bytes, against Go's.
//
// This is the load-bearing test of the whole port. Every constant in golden.hpp
// was produced by the GO X-Chain (test/golden/golden_gen.go run against
// luxfi/node/vms/xvm); this file builds the SAME value in C++ and compares the
// hex. If a byte moves, the two implementations have forked the chain, and that
// is what fails here rather than in production.
//
// Both directions are checked: C++ writes what Go writes, AND C++ reads what Go
// wrote back into the same values.

#include "check.hpp"
#include "fixtures.hpp"
#include "golden.hpp"

#include "lux/xvm/fx.hpp"
#include "lux/xvm/txs.hpp"

using namespace lux::xvm;
using namespace lux::xvm::test;

int main() {
    std::printf("xvm — the wire, against the Go X-Chain's own bytes\n\n");

    // ---- fx primitives ----
    check_eq(hex_of(owners_one().bytes()), golden::kOutputOwners, "OutputOwners");
    check_eq(hex_of(owners_two().bytes()), golden::kOutputOwnersTwo,
             "OutputOwners, two addresses, threshold 2");
    check_eq(hex_of(transfer_output()->bytes()), golden::kTransferOutput,
             "secp256k1fx TransferOutput");
    check_eq(hex_of(transfer_input()->bytes()), golden::kTransferInput,
             "secp256k1fx TransferInput");
    check_eq(hex_of(mint_output()->bytes()), golden::kMintOutput, "secp256k1fx MintOutput");
    check_eq(hex_of(mint_operation()->bytes()), golden::kMintOperation,
             "secp256k1fx MintOperation");
    check_eq(hex_of(credential()->bytes()), golden::kCredential, "secp256k1fx Credential");

    {
        fx::nftfx::MintOutput o;
        o.group_id = 7;
        o.out_owners = owners_one();
        check_eq(hex_of(o.bytes()), golden::kNFTMintOutput, "nftfx MintOutput");
    }
    fx::nftfx::TransferOutput nft_transfer;
    {
        nft_transfer.group_id = 7;
        const char* p = "hello nft";
        nft_transfer.payload.assign(p, p + 9);
        nft_transfer.out_owners = owners_one();
        check_eq(hex_of(nft_transfer.bytes()), golden::kNFTTransferOutput, "nftfx TransferOutput");
    }
    {
        fx::nftfx::MintOperation op;
        op.mint_input.sig_indices = {0};
        op.group_id = 7;
        const char* p = "hello nft";
        op.payload.assign(p, p + 9);
        op.outputs.push_back(std::make_shared<fx::OutputOwners>(owners_one()));
        op.outputs.push_back(std::make_shared<fx::OutputOwners>(owners_two()));
        check_eq(hex_of(op.bytes()), golden::kNFTMintOperation, "nftfx MintOperation");
    }
    {
        fx::nftfx::TransferOperation op;
        op.input.sig_indices = {0, 1};
        op.output = nft_transfer;
        check_eq(hex_of(op.bytes()), golden::kNFTTransferOperation, "nftfx TransferOperation");
    }
    {
        fx::nftfx::Credential c;
        c.signatures = {sig(3)};
        check_eq(hex_of(c.bytes()), golden::kNFTCredential, "nftfx Credential");
    }
    fx::propertyfx::MintOutput prop_mint;
    fx::propertyfx::OwnedOutput prop_owned;
    {
        prop_mint.out_owners = owners_one();
        check_eq(hex_of(prop_mint.bytes()), golden::kPropertyMintOutput, "propertyfx MintOutput");
        prop_owned.out_owners = owners_two();
        check_eq(hex_of(prop_owned.bytes()), golden::kPropertyOwnedOutput,
                 "propertyfx OwnedOutput");
    }
    {
        fx::propertyfx::MintOperation op;
        op.mint_input.sig_indices = {1};
        op.mint_output = prop_mint;
        op.owned_output = prop_owned;
        check_eq(hex_of(op.bytes()), golden::kPropertyMintOperation, "propertyfx MintOperation");
    }
    {
        fx::propertyfx::BurnOperation op;
        op.input.sig_indices = {0};
        check_eq(hex_of(op.bytes()), golden::kPropertyBurnOperation, "propertyfx BurnOperation");
    }
    {
        fx::propertyfx::Credential c;
        c.signatures = {sig(11)};
        check_eq(hex_of(c.bytes()), golden::kPropertyCredential, "propertyfx Credential");
    }

    // ---- UTXO, and the id a spend of it names ----
    {
        txs::UTXO u{txs::UTXOID{id(100), 3, false}, id(50), transfer_output()};
        auto b = u.wire_bytes();
        check(b.has_value(), "UTXO serializes");
        if (b) check_eq(hex_of(*b), golden::kUTXO, "UTXO wire envelope");
        check_eq(hex_of(u.utxo_id.input_id()), golden::kUTXOInputID,
                 "UTXOID.input_id = sha256(be64(index) || txID)");
    }

    // ---- the XVMBaseTx envelope on its own ----
    check_eq(hex_of(base_fields().wire_envelope()), golden::kXVMBaseTxEnvelope,
             "XVMBaseTx envelope");

    // ---- the five transactions ----
    {
        auto tx = std::make_shared<txs::BaseTx>();
        tx->base = base_fields();
        check_eq(hex_of(tx->bytes()), golden::kBaseTxUnsigned, "BaseTx unsigned bytes");

        txs::Tx signed_tx;
        signed_tx.unsigned_tx = tx;
        signed_tx.creds.push_back(credential());
        auto r = signed_tx.initialize();
        check(r.has_value(), "BaseTx signs");
        check_eq(hex_of(signed_tx.bytes()), golden::kBaseTxSigned, "BaseTx signed bytes");
        check_eq(hex_of(signed_tx.id()), golden::kBaseTxID, "TxID = sha256(signed bytes)");

        // ...and the same bytes read back.
        auto parsed = txs::parse(view(signed_tx.bytes_));
        check(parsed.has_value(), "signed BaseTx parses");
        if (parsed) {
            check((*parsed)->id() == signed_tx.id(), "parsed TxID matches");
            check((*parsed)->unsigned_tx->kind() == txs::XKind::Base, "parsed kind is Base");
            check((*parsed)->creds.size() == 1, "parsed one credential");
            check_eq(hex_of((*parsed)->unsigned_tx->bytes()), golden::kBaseTxUnsigned,
                     "parsed unsigned bytes are byte-preserved");
        }
    }

    {
        auto tx = std::make_shared<txs::CreateAssetTx>();
        tx->base = base_fields();
        tx->name = "Lux Test Asset";
        tx->symbol = "LTA";
        tx->denomination = 9;
        txs::InitialState st;
        st.fx_index = 0;
        st.outs = {mint_output(), transfer_output()};
        st.sort();
        check_eq(hex_of(st.bytes()), golden::kInitialState, "InitialState");
        tx->states.push_back(st);
        check_eq(hex_of(tx->bytes()), golden::kCreateAssetTxUnsigned,
                 "CreateAssetTx unsigned bytes");

        auto parsed = txs::parse_unsigned(view(tx->bytes()));
        check(parsed.has_value(), "CreateAssetTx parses");
        if (parsed) {
            auto* ca = dynamic_cast<txs::CreateAssetTx*>(parsed->get());
            check(ca != nullptr, "parsed as CreateAssetTx");
            if (ca) {
                check(ca->name == "Lux Test Asset", "name round-trips");
                check(ca->symbol == "LTA", "symbol round-trips");
                check(ca->denomination == 9, "denomination round-trips");
                check(ca->states.size() == 1 && ca->states[0].outs.size() == 2,
                      "initial state round-trips");
            }
        }
    }

    {
        txs::Operation op;
        op.asset_id = id(50);
        op.utxo_ids = {txs::UTXOID{id(100), 1, false}, txs::UTXOID{id(100), 4, false}};
        op.op = mint_operation();
        check_eq(hex_of(op.bytes()), golden::kOperation, "Operation");

        auto tx = std::make_shared<txs::OperationTx>();
        tx->base = base_fields();
        tx->ops.push_back(op);
        check_eq(hex_of(tx->bytes()), golden::kOperationTxUnsigned, "OperationTx unsigned bytes");

        auto parsed = txs::parse_unsigned(view(tx->bytes()));
        check(parsed.has_value(), "OperationTx parses");
        if (parsed) {
            auto* o = dynamic_cast<txs::OperationTx*>(parsed->get());
            check(o != nullptr && o->ops.size() == 1, "one operation round-trips");
            if (o && o->ops.size() == 1) {
                check(o->ops[0].utxo_ids.size() == 2, "operation utxo ids round-trip");
                check(o->ops[0].asset_id == id(50), "operation asset round-trips");
            }
        }
    }

    {
        auto tx = std::make_shared<txs::ImportTx>();
        tx->base = base_fields();
        tx->source_chain = id(77);
        tx->imported_ins.push_back(
            txs::TransferableInput{txs::UTXOID{id(150), 0, false}, id(50), transfer_input()});
        check_eq(hex_of(tx->bytes()), golden::kImportTxUnsigned, "ImportTx unsigned bytes");

        auto parsed = txs::parse_unsigned(view(tx->bytes()));
        check(parsed.has_value(), "ImportTx parses");
        if (parsed) {
            auto* i = dynamic_cast<txs::ImportTx*>(parsed->get());
            check(i != nullptr && i->source_chain == id(77), "source chain round-trips");
            check(i != nullptr && i->imported_ins.size() == 1, "imported input round-trips");
        }
    }

    {
        auto tx = std::make_shared<txs::ExportTx>();
        tx->base = base_fields();
        tx->destination_chain = id(88);
        tx->exported_outs.push_back(txs::TransferableOutput{id(50), transfer_output()});
        check_eq(hex_of(tx->bytes()), golden::kExportTxUnsigned, "ExportTx unsigned bytes");

        auto parsed = txs::parse_unsigned(view(tx->bytes()));
        check(parsed.has_value(), "ExportTx parses");
        if (parsed) {
            auto* e = dynamic_cast<txs::ExportTx*>(parsed->get());
            check(e != nullptr && e->destination_chain == id(88),
                  "destination chain round-trips");
            check(e != nullptr && e->exported_outs.size() == 1, "exported output round-trips");
        }
    }

    // ---- reading GO's bytes, not just writing them ----
    {
        Bytes go_bytes = from_hex(golden::kBaseTxSigned);
        auto parsed = txs::parse(view(go_bytes));
        check(parsed.has_value(), "a tx written by GO parses in C++");
        if (parsed) {
            check(hex_of((*parsed)->id()) == golden::kBaseTxID, "…with Go's TxID");
            check((*parsed)->unsigned_tx->base.network_id == 10, "…and Go's network id");
            check((*parsed)->unsigned_tx->base.memo.size() == 10, "…and Go's memo");
        }
        Bytes go_utxo = from_hex(golden::kUTXO);
        auto u = txs::parse_utxo(view(go_utxo));
        check(u.has_value(), "a UTXO written by GO parses in C++");
        if (u) {
            check(u->utxo_id.output_index == 3, "…with its output index");
            check(u->asset_id == id(50), "…and its asset");
        }
    }

    return report("wire");
}
