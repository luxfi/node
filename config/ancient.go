// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// AncientConfig is where the C-Chain keeps blocks it will never rewrite.
//
// A node with an ancient directory moves blocks older than the freeze threshold
// out of its chain database into an append-only store on that path, so the
// database it works against stays the size of the recent chain rather than the
// whole of it. A node that shares a directory reads a store another node writes:
// it keeps no copy of the frozen blocks at all and serves them from the shared
// one. That is what lets a machine run many nodes on one copy of history instead
// of one copy each.
type AncientConfig struct {
	Enabled   bool
	Dir       string
	Shared    bool
	Threshold uint64
}

func addAncientFlags(fs *pflag.FlagSet) {
	fs.Bool(CChainAncientKey, false,
		"Keep C-Chain history in an append-only ancient store instead of the chain database")
	fs.String(CChainAncientDirKey, "",
		fmt.Sprintf("Path to the ancient store (default: <%s>/ancient). Point several nodes at one path to share it", ChainDataDirKey))
	fs.Bool(CChainAncientSharedKey, false,
		"Read an ancient store another node writes; this node never writes to it")
	fs.Uint64(CChainFreezeThresholdKey, defaultFreezeThreshold,
		"Number of recent blocks the C-Chain keeps in its chain database before they move to the ancient store")
}

// defaultFreezeThreshold matches the EVM's own default retention.
const defaultFreezeThreshold = 90_000

func getAncientConfig(v *viper.Viper, chainDataDir string) (AncientConfig, error) {
	c := AncientConfig{
		Enabled:   v.GetBool(CChainAncientKey),
		Dir:       v.GetString(CChainAncientDirKey),
		Shared:    v.GetBool(CChainAncientSharedKey),
		Threshold: v.GetUint64(CChainFreezeThresholdKey),
	}
	if err := c.Validate(chainDataDir); err != nil {
		return AncientConfig{}, err
	}
	return c, nil
}

// Validate resolves the default path and rejects the combinations that do not
// describe a real arrangement of nodes on a machine.
func (c *AncientConfig) Validate(chainDataDir string) error {
	if !c.Enabled {
		if c.Dir != "" || c.Shared {
			return fmt.Errorf("--%s and --%s need --%s",
				CChainAncientDirKey, CChainAncientSharedKey, CChainAncientKey)
		}
		return nil
	}
	if c.Dir == "" {
		if chainDataDir == "" {
			return fmt.Errorf("--%s needs --%s or --%s", CChainAncientKey, CChainAncientDirKey, ChainDataDirKey)
		}
		c.Dir = filepath.Join(chainDataDir, "ancient")
	}
	if !filepath.IsAbs(c.Dir) {
		return fmt.Errorf("--%s must be an absolute path, got %q", CChainAncientDirKey, c.Dir)
	}
	// Sharing means reading a store another node writes. Two writers on one
	// store would interleave two chains into the same append-only files.
	// The other way round is not a posture, it is a stalled node: a store
	// nobody writes stops at whatever block it was left holding, and this node
	// would never see another.
	if c.Threshold == 0 {
		return fmt.Errorf("--%s must be at least 1: the block being built needs its parent in the chain database",
			CChainFreezeThresholdKey)
	}
	return nil
}

// chainConfig renders the settings the way the C-Chain reads them. The keys are
// the json tags on the EVM plugin's Config: AncientDir, AncientShared and
// FreezeThreshold.
func (c *AncientConfig) chainConfig() map[string]interface{} {
	if !c.Enabled {
		return nil
	}
	return map[string]interface{}{
		"ancient-dir":      c.Dir,
		"ancient-shared":   c.Shared,
		"freeze-threshold": c.Threshold,
	}
}
