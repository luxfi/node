// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/luxfi/node/chains"
)

// Every ancient key getAncientConfig reads must also be a registered flag. A key
// wired into the getter but never registered reads fine from a config file and
// kills the node on the command line with "unknown flag".
func TestAncientKeysAreRegisteredFlags(t *testing.T) {
	fs := BuildFlagSet()
	for _, key := range []string{
		CChainAncientKey,
		CChainAncientDirKey,
		CChainAncientSharedKey,
		CChainFreezeThresholdKey,
	} {
		if fs.Lookup(key) == nil {
			t.Errorf("--%s is read by getAncientConfig but is NOT a registered flag; luxd would exit with %q", key, "unknown flag: --"+key)
		}
	}
}

// ancientFromArgv runs a real command line through the flag set and viper, which
// is the only way to catch a key that is read but never registered.
func ancientFromArgv(t *testing.T, argv ...string) (AncientConfig, error) {
	t.Helper()
	fs := BuildFlagSet()
	v, err := BuildViper(fs, argv)
	if err != nil {
		t.Fatalf("parsing %v: %v", argv, err)
	}
	return getAncientConfig(v, "/data/chainData")
}

func TestAncientDefaultsToOff(t *testing.T) {
	c, err := ancientFromArgv(t)
	if err != nil {
		t.Fatalf("default config rejected: %v", err)
	}
	if c.Enabled {
		t.Error("the ancient store is on by default")
	}
	if got := c.chainConfig(); got != nil {
		t.Errorf("an off ancient store still writes %v into the C-Chain config", got)
	}
}

func TestAncientResolvesDefaultPath(t *testing.T) {
	c, err := ancientFromArgv(t, "--cchain-ancient")
	if err != nil {
		t.Fatalf("enabling the ancient store failed: %v", err)
	}
	if want := filepath.Join("/data/chainData", "ancient"); c.Dir != want {
		t.Errorf("dir = %q, want %q", c.Dir, want)
	}
	if c.Threshold != defaultFreezeThreshold {
		t.Errorf("threshold = %d, want %d", c.Threshold, defaultFreezeThreshold)
	}
	if c.Shared {
		t.Error("a node owns its own store unless it is told to share one")
	}
}

func TestAncientSharedReader(t *testing.T) {
	c, err := ancientFromArgv(t,
		"--cchain-ancient",
		"--cchain-ancient-dir=/srv/lux/ancient",

		"--cchain-ancient-shared",
		"--cchain-freeze-threshold=100",
	)
	if err != nil {
		t.Fatalf("shared reader rejected: %v", err)
	}
	if c.Dir != "/srv/lux/ancient" || !c.Shared || c.Threshold != 100 {
		t.Fatalf("parsed %+v", c)
	}
	want := map[string]interface{}{
		"ancient-dir":      "/srv/lux/ancient",
		"ancient-shared":   true,
		"freeze-threshold": uint64(100),
	}
	got := c.chainConfig()
	if len(got) != len(want) {
		t.Fatalf("chain config = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("chain config %s = %v, want %v", key, got[key], value)
		}
	}
}

func TestAncientRejectsBrokenCombinations(t *testing.T) {
	for _, tt := range []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "dir without enabling",
			argv: []string{"--cchain-ancient-dir=/srv/lux/ancient"},
			want: "need --cchain-ancient",
		},
		{
			name: "shared without enabling",
			argv: []string{"--cchain-ancient-shared"},
			want: "need --cchain-ancient",
		},
		{
			name: "zero threshold",
			argv: []string{"--cchain-ancient", "--cchain-freeze-threshold=0"},
			want: "must be at least 1",
		},
		{
			name: "relative dir",
			argv: []string{"--cchain-ancient", "--cchain-ancient-dir=ancient"},
			want: "must be an absolute path",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ancientFromArgv(t, tt.argv...)
			if err == nil {
				t.Fatalf("%v was accepted", tt.argv)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// The C-Chain reads its settings from its own config, so the node's flags have
// to land there without disturbing what an operator already put in the file.
func TestMergeCChainConfigKeepsExistingSettings(t *testing.T) {
	configs := map[string]chains.ChainConfig{
		"C": {Config: []byte(`{"eth-apis":["eth"],"freeze-threshold":7}`)},
		"P": {Config: []byte(`{"unrelated":true}`)},
	}
	err := mergeCChainConfig(configs, map[string]interface{}{
		"ancient-dir":      "/srv/lux/ancient",
		"freeze-threshold": uint64(100),
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	merged := map[string]interface{}{}
	if err := json.Unmarshal(configs["C"].Config, &merged); err != nil {
		t.Fatalf("unmarshal merged config: %v", err)
	}
	if merged["ancient-dir"] != "/srv/lux/ancient" {
		t.Errorf("ancient-dir = %v", merged["ancient-dir"])
	}
	if merged["freeze-threshold"] != float64(100) {
		t.Errorf("freeze-threshold = %v, want the node's 100", merged["freeze-threshold"])
	}
	apis, ok := merged["eth-apis"].([]interface{})
	if !ok || len(apis) != 1 || apis[0] != "eth" {
		t.Errorf("the operator's eth-apis did not survive the merge: %v", merged["eth-apis"])
	}
	if string(configs["P"].Config) != `{"unrelated":true}` {
		t.Errorf("the P-Chain config was touched: %s", configs["P"].Config)
	}
}

func TestMergeCChainConfigWithNothingToMerge(t *testing.T) {
	configs := map[string]chains.ChainConfig{}
	if err := mergeCChainConfig(configs, nil); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("an empty merge invented a C-Chain config: %v", configs)
	}
}
