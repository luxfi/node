// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Command repair-proposervm is a ONE-TIME, fail-closed surgical tool to reconcile
// a proposervm chain-state DB onto a known-canonical outer block at a single height.
//
// Motivation (incident 1082814, Lux mainnet C-Chain): a sub-quorum proposervm
// envelope A (3-of-5 ACCEPT) was locally accepted on some nodes while the network
// finalized the supermajority sibling B (4-of-5) that wraps the IDENTICAL inner EVM
// block. The two siblings differ ONLY in the proposervm outer envelope; the inner
// EVM state is byte-identical (no EVM divergence). On restart those nodes re-seed
// finality from their persisted proposervm lastAccepted=A and fatal on B's cert
// (EQUIVOCATION). This tool swaps the persisted record of `height` from A to the
// canonical B, leaving the inner EVM completely untouched (no EVM rollback).
//
// It is NOT a blind hex edit: it opens the exact same typed proposervm state
// (luxfi/node/vms/proposervm/state) over the exact same nested keyspace luxd uses
// (chainID -> "vm" -> "proposervm" -> versiondb -> chain/block/height), and writes
// via the state's own PutBlock / SetBlockIDAtHeight / SetLastAccepted so the on-disk
// bytes are identical to what luxd itself wrote for B on the canonical node.
//
// proposervm invariant honored: proLastAcceptedHeight must never be < the inner VM's
// last-accepted height (vm.repairAcceptedChainByHeight). The inner EVM is at `height`
// (it accepted the shared inner block under A), so the recovery target is the
// canonical block AT `height` (B), never height-1 — keeping outer==inner height.
//
// Modes:
//
//	inspect : read-only. Print lastAccepted, height index at H and H-1, and the
//	          outer block currently recorded at H.
//	export  : read-only. Read the outer block recorded at H and write its raw
//	          stateless bytes to --block-file (run against a canonical node's DB).
//	repair  : read-write, fail-closed. Parse --block-file (canonical B), assert it is
//	          the expected block, assert the DB is in the expected bad state (lastAccepted
//	          and heightIndex[H] both == the expected sub-quorum block A, and B's parent
//	          == heightIndex[H-1]); then PutBlock(B), SetBlockIDAtHeight(H,B),
//	          SetLastAccepted(B), Commit. Idempotent: if already on B, it no-ops.
//
// The DB uses an exclusive LOCK; luxd MUST be stopped on the target before `repair`.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/luxfi/database"
	databasefactory "github.com/luxfi/database/factory"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"

	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/state"
)

// The proposervm nests its state under these fixed prefixes inside the chain DB.
// chainDB = prefixdb(chainID[:], baseDB)            [chains.ChainDBManager.GetDatabase]
// vmDB    = prefixdb("vm", chainDB)                 [chains.ChainDBManager.GetVMDatabase / VMDBPrefix]
// ppvmDB  = versiondb(prefixdb("proposervm", vmDB)) [proposervm.VM.Initialize dbPrefix]
// state.New(ppvmDB) -> "chain"/"block"/"height" sub-prefixes
var (
	vmDBPrefix         = []byte("vm")
	proposervmDBPrefix = []byte("proposervm")
)

var (
	dbPath        string
	dbType        string
	chainIDStr    string
	height        uint64
	blockFile     string
	expectBlockID string // canonical B (target)
	expectCurID   string // sub-quorum A (the bad state we expect to overwrite)
	yes           bool
)

func main() {
	root := &cobra.Command{
		Use:   "repair-proposervm",
		Short: "Surgical, fail-closed reconcile of a proposervm chain-state onto a canonical outer block at one height",
	}
	root.PersistentFlags().StringVar(&dbPath, "db-path", "", "zapdb root (e.g. /data/db/mainnet/db) (required)")
	root.PersistentFlags().StringVar(&dbType, "db-type", "zapdb", "database type")
	root.PersistentFlags().StringVar(&chainIDStr, "chain-id", "2wRdZGeca1qkxzNCq88NWDF5nJ5A9o623vRJKd3FsjRYvuVvvt", "blockchain ID (proposervm chain)")
	root.PersistentFlags().Uint64Var(&height, "height", 1082814, "contested height")
	root.MarkPersistentFlagRequired("db-path")

	inspect := &cobra.Command{Use: "inspect", Short: "read-only: print proposervm finality state at the height", RunE: runInspect}

	export := &cobra.Command{Use: "export", Short: "read-only: write the outer block recorded at the height to --block-file", RunE: runExport}
	export.Flags().StringVar(&blockFile, "block-file", "", "output file for the canonical outer block bytes (required)")
	export.MarkFlagRequired("block-file")

	probe := &cobra.Command{Use: "probe", Short: "read-only: look up an arbitrary block by ID (is it present in the store?)", RunE: runProbe}
	probe.Flags().StringVar(&expectBlockID, "block-id", "", "block ID to look up (required)")
	probe.MarkFlagRequired("block-id")

	dump := &cobra.Command{Use: "dump", Short: "read-only: write an arbitrary block (by ID) raw stateless bytes to --block-file", RunE: runDump}
	dump.Flags().StringVar(&expectBlockID, "block-id", "wDMUyGyaKcC2Vng8i8ngU5f83XEtHZx5hqCSe5tMTwLAPagmo", "block ID to dump (canonical B)")
	dump.Flags().StringVar(&blockFile, "block-file", "", "output file for the block bytes (required)")
	dump.MarkFlagRequired("block-file")

	repair := &cobra.Command{Use: "repair", Short: "fail-closed: swap height's outer block from A to canonical B", RunE: runRepair}
	repair.Flags().StringVar(&blockFile, "block-file", "", "canonical outer block (B) bytes, from `export` (required)")
	repair.Flags().StringVar(&expectBlockID, "expect-block", "wDMUyGyaKcC2Vng8i8ngU5f83XEtHZx5hqCSe5tMTwLAPagmo", "expected canonical block ID (B)")
	repair.Flags().StringVar(&expectCurID, "expect-current", "2U2pR3DHCNEFDLnMq2uraNVkThRWgETDd468hR26yGHBQuAnNy", "expected current sub-quorum block ID (A) to be overwritten")
	repair.Flags().BoolVar(&yes, "yes", false, "confirm the write")
	repair.MarkFlagRequired("block-file")

	root.AddCommand(inspect, export, probe, dump, repair)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

// openState opens the proposervm typed state over the exact nested keyspace luxd uses.
// Returns the state, the proposervm versiondb (for Commit), and the base DB (for Close).
func openState(readOnly bool) (state.State, *versiondb.Database, database.Database, ids.ID, error) {
	chainID, err := ids.FromString(chainIDStr)
	if err != nil {
		return nil, nil, nil, ids.Empty, fmt.Errorf("bad chain-id: %w", err)
	}
	logger := log.New("cmd", "repair-proposervm")
	gatherer := metric.NewRegistry()
	base, err := databasefactory.New(dbType, dbPath, readOnly, nil, gatherer, logger, "repair", "db")
	if err != nil {
		return nil, nil, nil, ids.Empty, fmt.Errorf("open db %q: %w", dbPath, err)
	}
	chainDB := prefixdb.New(chainID[:], base)
	vmDB := prefixdb.New(vmDBPrefix, chainDB)
	ppvmDB := versiondb.New(prefixdb.New(proposervmDBPrefix, vmDB))
	return state.New(ppvmDB), ppvmDB, base, chainID, nil
}

func idAt(st state.State, h uint64) string {
	id, err := st.GetBlockIDAtHeight(h)
	if errors.Is(err, database.ErrNotFound) {
		return "<none>"
	}
	if err != nil {
		return "<err:" + err.Error() + ">"
	}
	return id.String()
}

func runInspect(_ *cobra.Command, _ []string) error {
	st, _, base, _, err := openState(true)
	if err != nil {
		return err
	}
	defer base.Close()
	la, err := st.GetLastAccepted()
	if err != nil {
		return fmt.Errorf("GetLastAccepted: %w", err)
	}
	fmt.Printf("proposervm lastAccepted = %s\n", la)
	fmt.Printf("heightIndex[%d]        = %s\n", height, idAt(st, height))
	fmt.Printf("heightIndex[%d]        = %s\n", height-1, idAt(st, height-1))
	if id, err := st.GetBlockIDAtHeight(height); err == nil {
		if blk, err := st.GetBlock(id); err == nil {
			fmt.Printf("block@%d: id=%s parent=%s bytes=%d\n", height, blk.ID(), blk.ParentID(), len(blk.Bytes()))
		}
	}
	return nil
}

func runProbe(_ *cobra.Command, _ []string) error {
	want, err := ids.FromString(expectBlockID)
	if err != nil {
		return fmt.Errorf("bad --block-id: %w", err)
	}
	// RW so Badger replays its WAL: a verified/built-but-unflushed block may live
	// only in the memtable. Disposable copy only.
	st, _, base, _, err := openState(false)
	if err != nil {
		return err
	}
	defer base.Close()
	blk, err := st.GetBlock(want)
	if errors.Is(err, database.ErrNotFound) {
		fmt.Printf("NOT-PRESENT: block %s is not in this store\n", want)
		return nil
	}
	if err != nil {
		return fmt.Errorf("GetBlock(%s): %w", want, err)
	}
	fmt.Printf("PRESENT: id=%s parent=%s bytes=%d\n", blk.ID(), blk.ParentID(), len(blk.Bytes()))
	return nil
}

func runDump(_ *cobra.Command, _ []string) error {
	want, err := ids.FromString(expectBlockID)
	if err != nil {
		return fmt.Errorf("bad --block-id: %w", err)
	}
	// RW so Badger replays its WAL: a verified-but-unflushed block may live only in
	// the memtable. We never call a state WRITER here (read + write output file only),
	// and this runs against an idle (luxd-stopped) pod DB; the contested sibling B was
	// verified by this node when it saw the conflicting cert, so it is present by ID.
	st, _, base, _, err := openState(false)
	if err != nil {
		return err
	}
	defer base.Close()
	blk, err := st.GetBlock(want)
	if errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("block %s is NOT present in this store (cannot dump)", want)
	}
	if err != nil {
		return fmt.Errorf("GetBlock(%s): %w", want, err)
	}
	if blk.ID() != want {
		return fmt.Errorf("self-ID mismatch: requested=%s parsed=%s (refusing)", want, blk.ID())
	}
	if err := os.WriteFile(blockFile, blk.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", blockFile, err)
	}
	fmt.Printf("DUMPED id=%s parent=%s bytes=%d -> %s\n", blk.ID(), blk.ParentID(), len(blk.Bytes()), blockFile)
	return nil
}

func runExport(_ *cobra.Command, _ []string) error {
	// Open read-WRITE so Badger replays its value-log/WAL: the canonical block at
	// the contested height was the LAST write before the chain went idle, so on a
	// crash-consistent snapshot copy it may live only in the memtable/WAL, not yet
	// in an SST. A read-only open skips recovery and could miss it. We never call a
	// state writer here (we only read + write the output file), and this only ever
	// runs against a DISPOSABLE snapshot copy of a canonical node — never the live node.
	st, _, base, _, err := openState(false)
	if err != nil {
		return err
	}
	defer base.Close()
	id, err := st.GetBlockIDAtHeight(height)
	if err != nil {
		return fmt.Errorf("GetBlockIDAtHeight(%d): %w", height, err)
	}
	blk, err := st.GetBlock(id)
	if err != nil {
		return fmt.Errorf("GetBlock(%s): %w", id, err)
	}
	if blk.ID() != id {
		return fmt.Errorf("block id mismatch: index=%s block=%s", id, blk.ID())
	}
	if err := os.WriteFile(blockFile, blk.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", blockFile, err)
	}
	la, _ := st.GetLastAccepted()
	fmt.Printf("EXPORTED height=%d id=%s parent=%s bytes=%d -> %s\n", height, blk.ID(), blk.ParentID(), len(blk.Bytes()), blockFile)
	fmt.Printf("  (source lastAccepted=%s heightIndex[%d-1]=%s)\n", la, height, idAt(st, height-1))
	return nil
}

func runRepair(_ *cobra.Command, _ []string) error {
	wantB, err := ids.FromString(expectBlockID)
	if err != nil {
		return fmt.Errorf("bad --expect-block: %w", err)
	}
	wantA, err := ids.FromString(expectCurID)
	if err != nil {
		return fmt.Errorf("bad --expect-current: %w", err)
	}
	raw, err := os.ReadFile(blockFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", blockFile, err)
	}
	blk, err := block.ParseWithoutVerification(raw)
	if err != nil {
		return fmt.Errorf("parse block file: %w", err)
	}
	// (1) the supplied block must be exactly the canonical target B.
	if blk.ID() != wantB {
		return fmt.Errorf("block-file id %s != --expect-block %s (refusing)", blk.ID(), wantB)
	}

	st, ppvmDB, base, _, err := openState(false)
	if err != nil {
		return err
	}
	defer base.Close()

	la, err := st.GetLastAccepted()
	if err != nil {
		return fmt.Errorf("GetLastAccepted: %w", err)
	}
	curAtH, err := st.GetBlockIDAtHeight(height)
	if err != nil {
		return fmt.Errorf("GetBlockIDAtHeight(%d): %w", height, err)
	}

	// (2) idempotency: if already on B, do nothing.
	if la == wantB && curAtH == wantB {
		fmt.Printf("ALREADY-CANONICAL: lastAccepted and heightIndex[%d] already == B (%s); no-op\n", height, wantB)
		return nil
	}

	// (3) fail-closed: only proceed from the exact expected bad state (A at H, lastAccepted A).
	if la != wantA {
		return fmt.Errorf("refusing: lastAccepted=%s is neither A(%s) nor B(%s) — unexpected state", la, wantA, wantB)
	}
	if curAtH != wantA {
		return fmt.Errorf("refusing: heightIndex[%d]=%s != A(%s) — unexpected state", height, curAtH, wantA)
	}

	// (4) B must extend the SAME finalized prefix: B.parent == the block recorded at H-1.
	parentAtH1, err := st.GetBlockIDAtHeight(height - 1)
	if err != nil {
		return fmt.Errorf("GetBlockIDAtHeight(%d): %w", height-1, err)
	}
	if blk.ParentID() != parentAtH1 {
		return fmt.Errorf("refusing: B.parent=%s != heightIndex[%d]=%s (B does not extend the local finalized prefix)", blk.ParentID(), height-1, parentAtH1)
	}
	if _, err := st.GetBlock(parentAtH1); err != nil {
		return fmt.Errorf("refusing: parent %s (height %d) not present in store: %w", parentAtH1, height-1, err)
	}

	fmt.Printf("PLAN: height=%d  %s (A) -> %s (B)\n", height, wantA, wantB)
	fmt.Printf("  current lastAccepted = %s\n", la)
	fmt.Printf("  B.parent             = %s == heightIndex[%d] OK\n", blk.ParentID(), height-1)
	if !yes {
		return errors.New("dry-run only: re-run with --yes to write")
	}

	// (5) apply via the state's own typed writers (identical on-disk bytes to luxd).
	if err := st.PutBlock(blk); err != nil {
		return fmt.Errorf("PutBlock(B): %w", err)
	}
	if err := st.SetBlockIDAtHeight(height, blk.ID()); err != nil {
		return fmt.Errorf("SetBlockIDAtHeight(%d,B): %w", height, err)
	}
	if err := st.SetLastAccepted(blk.ID()); err != nil {
		return fmt.Errorf("SetLastAccepted(B): %w", err)
	}
	if err := ppvmDB.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// (6) re-read to confirm.
	la2, _ := st.GetLastAccepted()
	fmt.Printf("DONE: lastAccepted=%s heightIndex[%d]=%s heightIndex[%d]=%s\n", la2, height, idAt(st, height), height-1, idAt(st, height-1))
	if la2 != wantB || idAt(st, height) != wantB.String() {
		return fmt.Errorf("post-write verification FAILED")
	}
	fmt.Println("VERIFIED: proposervm now records canonical B at the contested height; inner EVM untouched.")
	return nil
}
