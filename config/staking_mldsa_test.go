// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/crypto/mldsa"
	"github.com/stretchr/testify/require"
)

// writeMLDSAFixture generates an ML-DSA-65 keypair, PEM-encodes both halves
// with the block types loadStakingMLDSA expects, writes them under dir, and
// returns (privPath, pubPath, privPEM, pubPEM).
func writeMLDSAFixture(t *testing.T, dir string) (privPath, pubPath string, privPEM, pubPEM []byte) {
	t.Helper()
	priv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(t, err)

	privPEM = pem.EncodeToMemory(&pem.Block{Type: "ML-DSA-65 PRIVATE KEY", Bytes: priv.Bytes()})
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "ML-DSA-65 PUBLIC KEY", Bytes: priv.PublicKey.Bytes()})

	require.NoError(t, os.MkdirAll(dir, 0o755))
	privPath = filepath.Join(dir, "mldsa.key")
	pubPath = filepath.Join(dir, "mldsa.pub")
	require.NoError(t, os.WriteFile(privPath, privPEM, 0o600))
	require.NoError(t, os.WriteFile(pubPath, pubPEM, 0o644))
	return privPath, pubPath, privPEM, pubPEM
}

// TestLoadStakingMLDSA_PathForm: both keys via explicit --staking-mldsa-*-file
// paths. The canonical strict-PQ deploy form.
func TestLoadStakingMLDSA_PathForm(t *testing.T) {
	dir := t.TempDir()
	privPath, pubPath, _, _ := writeMLDSAFixture(t, dir)

	v, err := BuildViper(BuildFlagSet(), []string{
		"--" + StakingMLDSAKeyPathKey + "=" + privPath,
		"--" + StakingMLDSAPubKeyPathKey + "=" + pubPath,
	})
	require.NoError(t, err)

	priv, pub, _, _, err := loadStakingMLDSA(v)
	require.NoError(t, err)
	require.NotNil(t, priv)
	require.NotEmpty(t, pub)
}

// TestLoadStakingMLDSA_ContentForm: keys via base64 PEM --*-content flags.
func TestLoadStakingMLDSA_ContentForm(t *testing.T) {
	dir := t.TempDir()
	_, _, privPEM, pubPEM := writeMLDSAFixture(t, dir)

	v, err := BuildViper(BuildFlagSet(), []string{
		"--" + StakingMLDSAKeyContentKey + "=" + base64.StdEncoding.EncodeToString(privPEM),
		"--" + StakingMLDSAPubKeyContentKey + "=" + base64.StdEncoding.EncodeToString(pubPEM),
	})
	require.NoError(t, err)

	priv, pub, _, _, err := loadStakingMLDSA(v)
	require.NoError(t, err)
	require.NotNil(t, priv)
	require.NotEmpty(t, pub)
}

// TestLoadStakingMLDSA_EmptyContentFallsThroughToPath pins the fall-through:
// when a deploy passes the *-content flag blank (e.g. an env/template that
// rendered to "") alongside a valid *-file path, the loader MUST consult the
// path. Short-circuiting on the empty content value returns no key, so
// StakingMLDSAPub stays empty, IsStrictPQ() is false, and the node boots under
// an ECDSA NodeID with no error — a strict-PQ validator silently degraded to
// classical-compat.
func TestLoadStakingMLDSA_EmptyContentFallsThroughToPath(t *testing.T) {
	dir := t.TempDir()
	privPath, pubPath, _, _ := writeMLDSAFixture(t, dir)

	v, err := BuildViper(BuildFlagSet(), []string{
		"--" + StakingMLDSAKeyPathKey + "=" + privPath,
		"--" + StakingMLDSAKeyContentKey + "=", // blank content
		"--" + StakingMLDSAPubKeyPathKey + "=" + pubPath,
		"--" + StakingMLDSAPubKeyContentKey + "=", // blank content
	})
	require.NoError(t, err)

	priv, pub, gotPrivPath, gotPubPath, err := loadStakingMLDSA(v)
	require.NoError(t, err)
	require.NotNil(t, priv, "blank content must fall through to the path")
	require.NotEmpty(t, pub, "blank content must fall through to the path")
	require.Equal(t, privPath, gotPrivPath)
	require.Equal(t, pubPath, gotPubPath)
}

// TestLoadStakingMLDSA_NothingSuppliedMints: a node handed no staking identity
// makes one, at the default paths, exactly as it does for the classical TLS
// keypair. This is the ordinary first boot. Under a strict-PQ profile the
// network layer refuses to start without this key, so minting it here is what
// lets a fresh node come up instead of stopping to ask for `pqkeygen`.
func TestLoadStakingMLDSA_NothingSuppliedMints(t *testing.T) {
	// Point DataDir at an empty dir so the default paths resolve under it
	// rather than picking up a developer's ~/.lux key.
	v, err := BuildViper(BuildFlagSet(), []string{
		"--" + DataDirKey + "=" + t.TempDir(),
	})
	require.NoError(t, err)

	priv, pub, privPath, pubPath, err := loadStakingMLDSA(v)
	require.NoError(t, err)
	require.NotNil(t, priv)
	require.NotEmpty(t, pub)
	require.FileExists(t, privPath)
	require.FileExists(t, pubPath)
}

// TestLoadStakingMLDSA_MintSurvivesRestart: the mint happens once. The NodeID
// of a strict-PQ node is derived from this key, so a second boot that minted
// again would hand the node a new identity and leave its stake behind on the
// old one. The key on disk wins every time after the first.
func TestLoadStakingMLDSA_MintSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	boot := func() ([]byte, []byte) {
		v, err := BuildViper(BuildFlagSet(), []string{"--" + DataDirKey + "=" + dir})
		require.NoError(t, err)
		_, pub, privPath, _, err := loadStakingMLDSA(v)
		require.NoError(t, err)
		onDisk, err := os.ReadFile(privPath)
		require.NoError(t, err)
		return pub, onDisk
	}

	firstPub, firstPriv := boot()
	secondPub, secondPriv := boot()
	require.Equal(t, firstPub, secondPub, "a restart must not change the NodeID")
	require.Equal(t, firstPriv, secondPriv, "a restart must not rewrite the key")
}

// TestLoadStakingMLDSA_NamedButMissingDoesNotMint: an operator who names a key
// is holding it. If the path is empty the answer is not to invent a different
// key there — that would swap a validator's identity for a fresh one and file
// it under success. The loader stands down and reports nothing, which leaves a
// strict-PQ node to refuse at the network layer and say `pqkeygen`.
func TestLoadStakingMLDSA_NamedButMissingDoesNotMint(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "held-elsewhere.key")
	pubPath := filepath.Join(dir, "held-elsewhere.pub")

	v, err := BuildViper(BuildFlagSet(), []string{
		"--" + DataDirKey + "=" + dir,
		"--" + StakingMLDSAKeyPathKey + "=" + privPath,
		"--" + StakingMLDSAPubKeyPathKey + "=" + pubPath,
	})
	require.NoError(t, err)

	priv, pub, _, _, err := loadStakingMLDSA(v)
	require.NoError(t, err)
	require.Nil(t, priv)
	require.Empty(t, pub)
	require.NoFileExists(t, privPath, "a named key must never be minted over")
	require.NoFileExists(t, pubPath, "a named key must never be minted over")
}

// TestLoadStakingMLDSA_PrivOnlyIsFatal: a private key with no public key is a
// loud config error — a strict-PQ validator must never silently degrade.
func TestLoadStakingMLDSA_PrivOnlyIsFatal(t *testing.T) {
	base := t.TempDir()
	_, _, privPEM, _ := writeMLDSAFixture(t, filepath.Join(base, "staking"))

	v, err := BuildViper(BuildFlagSet(), []string{
		"--" + DataDirKey + "=" + base, // default pub path under here will be missing... but priv content wins
		"--" + StakingMLDSAKeyContentKey + "=" + base64.StdEncoding.EncodeToString(privPEM),
		"--" + StakingMLDSAPubKeyPathKey + "=" + filepath.Join(base, "does-not-exist.pub"),
	})
	require.NoError(t, err)

	_, _, _, _, err = loadStakingMLDSA(v)
	require.Error(t, err)
	require.Contains(t, err.Error(), "both private and public key are required")
}
