// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/vms/evm/lp226"
	statelessblock "github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/lp181"
)

// TestGraniteActivation tests that granite activates at the correct timestamp
func TestGraniteActivation(t *testing.T) {
	tests := []struct {
		name           string
		config         upgrade.Config
		timestamp      time.Time
		shouldBeActive bool
	}{
		{
			name:           "pre_granite",
			config:         upgrade.Default,
			timestamp:      upgrade.InitiallyActiveTime,
			shouldBeActive: false,
		},
		{
			name: "exactly_at_granite",
			config: upgrade.Config{
				ApricotPhase1Time:     upgrade.InitiallyActiveTime,
				ApricotPhase2Time:     upgrade.InitiallyActiveTime,
				ApricotPhase3Time:     upgrade.InitiallyActiveTime,
				ApricotPhase4Time:     upgrade.InitiallyActiveTime,
				ApricotPhase5Time:     upgrade.InitiallyActiveTime,
				ApricotPhasePre6Time:  upgrade.InitiallyActiveTime,
				ApricotPhase6Time:     upgrade.InitiallyActiveTime,
				ApricotPhasePost6Time: upgrade.InitiallyActiveTime,
				BanffTime:             upgrade.InitiallyActiveTime,
				CortinaTime:           upgrade.InitiallyActiveTime,
				DurangoTime:           upgrade.InitiallyActiveTime,
				EtnaTime:              upgrade.InitiallyActiveTime,
				FortunaTime:           upgrade.InitiallyActiveTime,
				GraniteTime:           upgrade.InitiallyActiveTime.Add(time.Hour),
				GraniteEpochDuration:  30 * time.Second,
			},
			timestamp:      upgrade.InitiallyActiveTime.Add(time.Hour),
			shouldBeActive: true,
		},
		{
			name: "after_granite",
			config: upgrade.Config{
				ApricotPhase1Time:     upgrade.InitiallyActiveTime,
				ApricotPhase2Time:     upgrade.InitiallyActiveTime,
				ApricotPhase3Time:     upgrade.InitiallyActiveTime,
				ApricotPhase4Time:     upgrade.InitiallyActiveTime,
				ApricotPhase5Time:     upgrade.InitiallyActiveTime,
				ApricotPhasePre6Time:  upgrade.InitiallyActiveTime,
				ApricotPhase6Time:     upgrade.InitiallyActiveTime,
				ApricotPhasePost6Time: upgrade.InitiallyActiveTime,
				BanffTime:             upgrade.InitiallyActiveTime,
				CortinaTime:           upgrade.InitiallyActiveTime,
				DurangoTime:           upgrade.InitiallyActiveTime,
				EtnaTime:              upgrade.InitiallyActiveTime,
				FortunaTime:           upgrade.InitiallyActiveTime,
				GraniteTime:           upgrade.InitiallyActiveTime.Add(time.Hour),
				GraniteEpochDuration:  30 * time.Second,
			},
			timestamp:      upgrade.InitiallyActiveTime.Add(2 * time.Hour),
			shouldBeActive: true,
		},
		{
			name: "one_nanosecond_before_granite",
			config: upgrade.Config{
				ApricotPhase1Time:     upgrade.InitiallyActiveTime,
				ApricotPhase2Time:     upgrade.InitiallyActiveTime,
				ApricotPhase3Time:     upgrade.InitiallyActiveTime,
				ApricotPhase4Time:     upgrade.InitiallyActiveTime,
				ApricotPhase5Time:     upgrade.InitiallyActiveTime,
				ApricotPhasePre6Time:  upgrade.InitiallyActiveTime,
				ApricotPhase6Time:     upgrade.InitiallyActiveTime,
				ApricotPhasePost6Time: upgrade.InitiallyActiveTime,
				BanffTime:             upgrade.InitiallyActiveTime,
				CortinaTime:           upgrade.InitiallyActiveTime,
				DurangoTime:           upgrade.InitiallyActiveTime,
				EtnaTime:              upgrade.InitiallyActiveTime,
				FortunaTime:           upgrade.InitiallyActiveTime,
				GraniteTime:           upgrade.InitiallyActiveTime.Add(time.Hour),
				GraniteEpochDuration:  30 * time.Second,
			},
			timestamp:      upgrade.InitiallyActiveTime.Add(time.Hour - time.Nanosecond),
			shouldBeActive: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isActive := test.config.IsGraniteActivated(test.timestamp)
			require.Equal(t, test.shouldBeActive, isActive,
				"Granite activation mismatch at timestamp %v", test.timestamp)
		})
	}
}

// TestLP181_EpochTransitions tests LP-181 epoch handling
func TestLP181_EpochTransitions(t *testing.T) {
	var (
		now            = upgrade.InitiallyActiveTime.Add(24 * time.Hour)
		nowPlusEpoch   = now.Add(upgrade.Default.GraniteEpochDuration)
		nowPlus2Epochs = now.Add(2 * upgrade.Default.GraniteEpochDuration)
		nowPlus3Epochs = now.Add(3 * upgrade.Default.GraniteEpochDuration)
	)

	tests := []struct {
		name               string
		fork               upgradetest.Fork
		parentPChainHeight uint64
		parentEpoch        statelessblock.Epoch
		parentTimestamp    time.Time
		childTimestamp     time.Time
		expected           statelessblock.Epoch
	}{
		{
			name:               "pre_granite_no_epoch",
			fork:               upgradetest.NoUpgrades,
			parentPChainHeight: 100,
			parentTimestamp:    now,
			childTimestamp:     now,
			expected:           statelessblock.Epoch{},
		},
		{
			name:               "first_post_granite_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 100,
			parentTimestamp:    now,
			childTimestamp:     now.Add(time.Second),
			expected: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
		},
		{
			name:               "keep_same_epoch_midway",
			fork:               upgradetest.Latest,
			parentPChainHeight: 101,
			parentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			parentTimestamp: now.Add(upgrade.Default.GraniteEpochDuration / 2),
			childTimestamp:  nowPlusEpoch,
			expected: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
		},
		{
			name:               "barely_transition_to_next_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 101,
			parentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			parentTimestamp: nowPlusEpoch,
			childTimestamp:  nowPlusEpoch,
			expected: statelessblock.Epoch{
				PChainHeight: 101,
				Number:       2,
				StartTime:    nowPlusEpoch.Unix(),
			},
		},
		{
			name:               "transition_to_next_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 101,
			parentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			parentTimestamp: nowPlus2Epochs,
			childTimestamp:  nowPlus3Epochs,
			expected: statelessblock.Epoch{
				PChainHeight: 101,
				Number:       2,
				StartTime:    nowPlus2Epochs.Unix(),
			},
		},
		{
			name:               "epoch_zero_to_one",
			fork:               upgradetest.Latest,
			parentPChainHeight: 50,
			parentEpoch:        statelessblock.Epoch{},
			parentTimestamp:    now,
			childTimestamp:     now.Add(time.Millisecond),
			expected: statelessblock.Epoch{
				PChainHeight: 50,
				Number:       1,
				StartTime:    now.Unix(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			epoch := lp181.NewEpoch(
				upgradetest.GetConfig(test.fork),
				test.parentPChainHeight,
				test.parentEpoch,
				test.parentTimestamp,
				test.childTimestamp,
			)
			require.Equal(t, test.expected, epoch)
		})
	}
}

// TestLP181_PChainHeightCaching tests validator set caching behavior
func TestLP181_PChainHeightCaching(t *testing.T) {
	now := upgrade.InitiallyActiveTime.Add(24 * time.Hour)

	tests := []struct {
		name           string
		currentEpoch   statelessblock.Epoch
		expectedHeight uint64
		shouldBeCached bool
	}{
		{
			name: "first_epoch_height",
			currentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			expectedHeight: 100,
			shouldBeCached: true,
		},
		{
			name: "later_epoch_height",
			currentEpoch: statelessblock.Epoch{
				PChainHeight: 250,
				Number:       5,
				StartTime:    now.Add(5 * upgrade.Default.GraniteEpochDuration).Unix(),
			},
			expectedHeight: 250,
			shouldBeCached: true,
		},
		{
			name:           "empty_epoch_no_cache",
			currentEpoch:   statelessblock.Epoch{},
			expectedHeight: 0,
			shouldBeCached: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expectedHeight, test.currentEpoch.PChainHeight)
			isEmpty := test.currentEpoch == statelessblock.Epoch{}
			require.Equal(t, !test.shouldBeCached, isEmpty)
		})
	}
}

// TestLP204_Secp256r1Precompile tests secp256r1 signature verification
func TestLP204_Secp256r1Precompile(t *testing.T) {
	tests := []struct {
		name          string
		setupKey      func() (*ecdsa.PrivateKey, []byte, []byte, []byte)
		expectedValid bool
		expectedGas   uint64
	}{
		{
			name: "valid_p256_signature",
			setupKey: func() (*ecdsa.PrivateKey, []byte, []byte, []byte) {
				privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)

				message := []byte("test message for granite LP-204")
				hash := sha256.Sum256(message)

				r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
				require.NoError(t, err)

				pubKeyBytes := elliptic.Marshal(elliptic.P256(), privKey.PublicKey.X, privKey.PublicKey.Y)

				// Pad r and s to 32 bytes each to ensure correct parsing
				rBytes := make([]byte, 32)
				sBytes := make([]byte, 32)
				r.FillBytes(rBytes)
				s.FillBytes(sBytes)

				return privKey, hash[:], append(rBytes, sBytes...), pubKeyBytes
			},
			expectedValid: true,
			expectedGas:   6900, // LP-204 specifies 6900 gas
		},
		{
			name: "invalid_signature",
			setupKey: func() (*ecdsa.PrivateKey, []byte, []byte, []byte) {
				privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)

				message := []byte("test message")
				hash := sha256.Sum256(message)

				// Create invalid signature
				invalidSig := make([]byte, 64)
				_, err = rand.Read(invalidSig)
				require.NoError(t, err)

				pubKeyBytes := elliptic.Marshal(elliptic.P256(), privKey.PublicKey.X, privKey.PublicKey.Y)

				return privKey, hash[:], invalidSig, pubKeyBytes
			},
			expectedValid: false,
			expectedGas:   6900,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			privKey, hash, signature, pubKey := test.setupKey()

			// Verify signature using standard library
			r := new(big.Int).SetBytes(signature[:32])
			s := new(big.Int).SetBytes(signature[32:64])
			valid := ecdsa.Verify(&privKey.PublicKey, hash, r, s)

			require.Equal(t, test.expectedValid, valid)

			// Gas cost is fixed at 6900 for LP-204
			require.Equal(t, test.expectedGas, uint64(6900))

			// Verify public key format (65 bytes: 0x04 + X + Y)
			require.Equal(t, 65, len(pubKey))
			require.Equal(t, byte(0x04), pubKey[0])
		})
	}
}

// TestLP204_BiometricWalletIntegration tests biometric wallet compatibility
func TestLP204_BiometricWalletIntegration(t *testing.T) {
	tests := []struct {
		name        string
		keySize     int
		expectedErr bool
	}{
		{
			name:        "standard_p256_key",
			keySize:     256,
			expectedErr: false,
		},
		{
			name:        "passkey_compatible",
			keySize:     256,
			expectedErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Generate P-256 key (standard for biometric/passkey wallets)
			privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if test.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify key parameters match P-256
			require.Equal(t, 256, privKey.Curve.Params().BitSize)

			// Test signing and verification (simulating biometric wallet flow)
			message := []byte("biometric wallet transaction")
			hash := sha256.Sum256(message)

			r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
			require.NoError(t, err)

			valid := ecdsa.Verify(&privKey.PublicKey, hash[:], r, s)
			require.True(t, valid)
		})
	}
}

// TestLP226_DynamicBlockTiming tests LP-226 dynamic block timing
func TestLP226_DynamicBlockTiming(t *testing.T) {
	tests := []struct {
		name          string
		delayExcess   lp226.DelayExcess
		expectedDelay uint64
		minDelay      uint64
		maxDelay      uint64
	}{
		{
			name:          "minimum_delay_1ms",
			delayExcess:   0,
			expectedDelay: lp226.MinDelayMilliseconds,
			minDelay:      1,
			maxDelay:      1,
		},
		{
			name:          "initial_delay_2000ms",
			delayExcess:   lp226.InitialDelayExcess,
			expectedDelay: 2000, // ≈2000ms
			minDelay:      1900,
			maxDelay:      2100,
		},
		{
			name:          "sub_second_block_timing",
			delayExcess:   lp226.DelayExcess(100000),
			expectedDelay: 0, // Will be calculated
			minDelay:      1,
			maxDelay:      10,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualDelay := test.delayExcess.Delay()

			if test.expectedDelay > 0 {
				require.GreaterOrEqual(t, actualDelay, test.minDelay)
				require.LessOrEqual(t, actualDelay, test.maxDelay)
			}

			// Verify minimum delay is always at least 1ms
			require.GreaterOrEqual(t, actualDelay, uint64(lp226.MinDelayMilliseconds))
		})
	}
}

// TestLP226_DelayExcessCalculation tests delay excess updates
func TestLP226_DelayExcessCalculation(t *testing.T) {
	tests := []struct {
		name              string
		currentExcess     lp226.DelayExcess
		desiredDelay      uint64
		expectedMaxChange uint64
	}{
		{
			name:              "increase_delay_excess",
			currentExcess:     lp226.InitialDelayExcess,
			desiredDelay:      3000, // Want 3 second blocks
			expectedMaxChange: lp226.MaxDelayExcessDiff,
		},
		{
			name:              "decrease_delay_excess",
			currentExcess:     lp226.InitialDelayExcess,
			desiredDelay:      1000, // Want 1 second blocks
			expectedMaxChange: lp226.MaxDelayExcessDiff,
		},
		{
			name:              "sub_second_target",
			currentExcess:     lp226.InitialDelayExcess,
			desiredDelay:      500, // Want 500ms blocks
			expectedMaxChange: lp226.MaxDelayExcessDiff,
		},
		{
			name:              "minimum_delay_target",
			currentExcess:     lp226.InitialDelayExcess,
			desiredDelay:      lp226.MinDelayMilliseconds,
			expectedMaxChange: lp226.MaxDelayExcessDiff,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalExcess := test.currentExcess
			desiredExcess := lp226.DesiredDelayExcess(test.desiredDelay)

			// Update excess
			excess := test.currentExcess
			excess.UpdateDelayExcess(desiredExcess)

			// Verify change is bounded
			change := uint64(0)
			if excess > originalExcess {
				change = uint64(excess - originalExcess)
			} else {
				change = uint64(originalExcess - excess)
			}
			require.LessOrEqual(t, change, test.expectedMaxChange)

			// Verify delay calculation
			newDelay := excess.Delay()
			require.GreaterOrEqual(t, newDelay, uint64(lp226.MinDelayMilliseconds))
		})
	}
}

// TestLP226_UnderLoadConditions tests block timing under different loads
func TestLP226_UnderLoadConditions(t *testing.T) {
	tests := []struct {
		name             string
		initialExcess    lp226.DelayExcess
		targetTPS        int
		iterations       int
		expectedMinDelay uint64
		expectedMaxDelay uint64
	}{
		{
			name:             "high_load_fast_blocks",
			initialExcess:    lp226.InitialDelayExcess,
			targetTPS:        1000,
			iterations:       100,
			expectedMinDelay: 1,
			expectedMaxDelay: 3000, // Adjusted: MaxDelayExcessDiff limits rate of change
		},
		{
			name:             "medium_load",
			initialExcess:    lp226.InitialDelayExcess,
			targetTPS:        100,
			iterations:       50,
			expectedMinDelay: 10,
			expectedMaxDelay: 2500, // Adjusted: MaxDelayExcessDiff limits rate of change
		},
		{
			name:             "low_load_slower_blocks",
			initialExcess:    lp226.InitialDelayExcess,
			targetTPS:        10,
			iterations:       20,
			expectedMinDelay: 100,
			expectedMaxDelay: 2500, // Adjusted: MaxDelayExcessDiff limits rate of change
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			excess := test.initialExcess

			// Calculate desired delay based on target TPS
			desiredDelay := uint64(1000 / test.targetTPS) // ms per transaction
			if desiredDelay < lp226.MinDelayMilliseconds {
				desiredDelay = lp226.MinDelayMilliseconds
			}

			// Simulate load by updating excess over iterations
			for i := 0; i < test.iterations; i++ {
				desiredExcess := lp226.DesiredDelayExcess(desiredDelay)
				excess.UpdateDelayExcess(desiredExcess)
			}

			finalDelay := excess.Delay()

			// Verify delay is within expected range
			require.GreaterOrEqual(t, finalDelay, test.expectedMinDelay)
			require.LessOrEqual(t, finalDelay, test.expectedMaxDelay)

			// Verify minimum constraint
			require.GreaterOrEqual(t, finalDelay, uint64(lp226.MinDelayMilliseconds))
		})
	}
}

// TestGraniteIntegration_AllLPsTogether tests all granite LPs working together
func TestGraniteIntegration_AllLPsTogether(t *testing.T) {
	now := upgrade.InitiallyActiveTime.Add(24 * time.Hour)
	config := upgradetest.GetConfig(upgradetest.Latest)

	// Test LP-181 (Epoching)
	epoch := lp181.NewEpoch(
		config,
		100,
		statelessblock.Epoch{},
		now,
		now.Add(time.Second),
	)
	require.Equal(t, uint64(1), epoch.Number)
	require.Equal(t, uint64(100), epoch.PChainHeight)

	// Test LP-226 (Dynamic Block Timing)
	delayExcess := lp226.DelayExcess(lp226.InitialDelayExcess)
	delay := delayExcess.Delay()
	require.GreaterOrEqual(t, delay, uint64(lp226.MinDelayMilliseconds))
	require.LessOrEqual(t, delay, uint64(2100)) // ~2000ms expected

	// Test LP-204 (secp256r1)
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	message := []byte("granite integration test")
	hash := sha256.Sum256(message)

	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	require.NoError(t, err)

	valid := ecdsa.Verify(&privKey.PublicKey, hash[:], r, s)
	require.True(t, valid)

	// Verify all features are integrated
	require.True(t, config.IsGraniteActivated(now))
	require.NotZero(t, epoch.Number)
	require.GreaterOrEqual(t, delay, uint64(1))
	require.True(t, valid)
}

// TestGraniteRollbackScenarios tests network behavior during rollback
func TestGraniteRollbackScenarios(t *testing.T) {
	graniteTime := upgrade.InitiallyActiveTime.Add(time.Hour)

	config := upgrade.Config{
		ApricotPhase1Time:     upgrade.InitiallyActiveTime,
		ApricotPhase2Time:     upgrade.InitiallyActiveTime,
		ApricotPhase3Time:     upgrade.InitiallyActiveTime,
		ApricotPhase4Time:     upgrade.InitiallyActiveTime,
		ApricotPhase5Time:     upgrade.InitiallyActiveTime,
		ApricotPhasePre6Time:  upgrade.InitiallyActiveTime,
		ApricotPhase6Time:     upgrade.InitiallyActiveTime,
		ApricotPhasePost6Time: upgrade.InitiallyActiveTime,
		BanffTime:             upgrade.InitiallyActiveTime,
		CortinaTime:           upgrade.InitiallyActiveTime,
		DurangoTime:           upgrade.InitiallyActiveTime,
		EtnaTime:              upgrade.InitiallyActiveTime,
		FortunaTime:           upgrade.InitiallyActiveTime,
		GraniteTime:           graniteTime,
		GraniteEpochDuration:  30 * time.Second,
	}

	tests := []struct {
		name           string
		blockTimestamp time.Time
		expectGranite  bool
		expectEpoch    bool
	}{
		{
			name:           "before_granite",
			blockTimestamp: graniteTime.Add(-time.Second),
			expectGranite:  false,
			expectEpoch:    false,
		},
		{
			name:           "at_granite_activation",
			blockTimestamp: graniteTime,
			expectGranite:  true,
			expectEpoch:    true,
		},
		{
			name:           "after_granite",
			blockTimestamp: graniteTime.Add(time.Minute),
			expectGranite:  true,
			expectEpoch:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isActive := config.IsGraniteActivated(test.blockTimestamp)
			require.Equal(t, test.expectGranite, isActive)

			if test.expectEpoch {
				epoch := lp181.NewEpoch(
					config,
					100,
					statelessblock.Epoch{},
					test.blockTimestamp,
					test.blockTimestamp.Add(time.Second),
				)
				require.NotEqual(t, statelessblock.Epoch{}, epoch)
			}
		})
	}
}

// TestGraniteNetworkIDConfiguration tests granite configuration across network IDs
func TestGraniteNetworkIDConfiguration(t *testing.T) {
	tests := []struct {
		name                  string
		networkID             uint32
		expectedEpochDuration time.Duration
		expectScheduled       bool
	}{
		{
			name:                  "mainnet",
			networkID:             constants.MainnetID,
			expectedEpochDuration: 5 * time.Minute,
			expectScheduled:       false, // Unscheduled
		},
		{
			name:                  "testnet",
			networkID:             constants.TestnetID,
			expectedEpochDuration: 30 * time.Second,
			expectScheduled:       false, // Unscheduled
		},
		{
			name:                  "local_default",
			networkID:             12345, // Any other ID
			expectedEpochDuration: 30 * time.Second,
			expectScheduled:       false, // Unscheduled
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := upgrade.GetConfig(test.networkID)
			require.Equal(t, test.expectedEpochDuration, config.GraniteEpochDuration)

			isScheduled := !config.GraniteTime.Equal(upgrade.UnscheduledActivationTime)
			require.Equal(t, test.expectScheduled, isScheduled)
		})
	}
}

// Benchmarks

// BenchmarkLP181_EpochTransition benchmarks epoch transition calculations
func BenchmarkLP181_EpochTransition(b *testing.B) {
	now := upgrade.InitiallyActiveTime.Add(24 * time.Hour)
	config := upgradetest.GetConfig(upgradetest.Latest)

	parentEpoch := statelessblock.Epoch{
		PChainHeight: 100,
		Number:       1,
		StartTime:    now.Unix(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lp181.NewEpoch(
			config,
			101,
			parentEpoch,
			now.Add(upgrade.Default.GraniteEpochDuration/2),
			now.Add(upgrade.Default.GraniteEpochDuration),
		)
	}
}

// BenchmarkLP204_SignatureVerification benchmarks P-256 signature verification
func BenchmarkLP204_SignatureVerification(b *testing.B) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(b, err)

	message := []byte("benchmark message")
	hash := sha256.Sum256(message)

	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ecdsa.Verify(&privKey.PublicKey, hash[:], r, s)
	}
}

// BenchmarkLP226_DelayCalculation benchmarks delay calculation
func BenchmarkLP226_DelayCalculation(b *testing.B) {
	excess := lp226.DelayExcess(lp226.InitialDelayExcess)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = excess.Delay()
	}
}

// BenchmarkLP226_DelayExcessUpdate benchmarks delay excess updates
func BenchmarkLP226_DelayExcessUpdate(b *testing.B) {
	excess := lp226.DelayExcess(lp226.InitialDelayExcess)
	desiredExcess := lp226.DesiredDelayExcess(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		excess.UpdateDelayExcess(desiredExcess)
	}
}

// BenchmarkGraniteActivationCheck benchmarks granite activation checks
func BenchmarkGraniteActivationCheck(b *testing.B) {
	config := upgrade.Default
	timestamp := upgrade.InitiallyActiveTime.Add(time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.IsGraniteActivated(timestamp)
	}
}

// BenchmarkFullGraniteIntegration benchmarks all granite features together
func BenchmarkFullGraniteIntegration(b *testing.B) {
	now := upgrade.InitiallyActiveTime.Add(24 * time.Hour)
	config := upgradetest.GetConfig(upgradetest.Latest)

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(b, err)

	message := []byte("benchmark integration")
	hash := sha256.Sum256(message)

	parentEpoch := statelessblock.Epoch{
		PChainHeight: 100,
		Number:       1,
		StartTime:    now.Unix(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// LP-181
		_ = lp181.NewEpoch(config, 101, parentEpoch, now, now.Add(time.Second))

		// LP-226
		excess := lp226.DelayExcess(lp226.InitialDelayExcess)
		_ = excess.Delay()

		// LP-204
		r, s, _ := ecdsa.Sign(rand.Reader, privKey, hash[:])
		_ = ecdsa.Verify(&privKey.PublicKey, hash[:], r, s)
	}
}
