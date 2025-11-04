// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/net/primary"
	"github.com/luxfi/node/wallet/net/primary/common"
)

const (
	defaultNetDirName = "subnets"
	jsonFileSuffix       = ".json"
)

type Chain struct {
	// Set statically
	VMID    ids.ID
	Config  string
	Genesis []byte
	// VersionArgs are the argument(s) to pass to the VM binary to receive
	// version details in json format (e.g. `--version-json`). This
	// supports checking that the rpcchainvm version of the VM binary
	// matches the version used by the configured node binary. If
	// empty, the version check will be skipped.
	VersionArgs []string

	// Set at runtime
	ChainID      ids.ID
	PreFundedKey *secp256k1.PrivateKey
}

type Net struct {
	// A unique string that can be used to refer to the subnet across different temporary
	// networks (since the NetID will be different every time the subnet is created)
	Name string

	Config ConfigMap

	// The ID of the transaction that created the subnet
	NetID ids.ID

	// The private key that owns the subnet
	OwningKey *secp256k1.PrivateKey

	// IDs of the nodes responsible for validating the subnet
	ValidatorIDs []ids.NodeID

	Chains []*Chain
}

// Retrieves a wallet configured for use with the subnet
func (s *Net) GetWallet(ctx context.Context, uri string) (*primary.Wallet, error) {
	keychain := secp256k1fx.NewKeychain(s.OwningKey)

	// Only fetch the net transaction if a net ID is present. This won't be true when
	// the wallet is first used to create the subnet.
	subnetIDs := []ids.ID{}
	if s.NetID != ids.Empty {
		subnetIDs = append(subnetIDs, s.NetID)
	}

	return primary.MakeWallet(
		ctx,
		uri,
		keychain,
		keychain,
		primary.WalletConfig{
			NetIDs: subnetIDs,
		},
	)
}

// Issues the net creation transaction and retains the result. The URI of a node is
// required to issue the transaction.
func (s *Net) Create(ctx context.Context, uri string) error {
	wallet, err := s.GetWallet(ctx, uri)
	if err != nil {
		return err
	}
	pWallet := wallet.P()

	subnetTx, err := pWallet.IssueCreateNetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				s.OwningKey.Address(),
			},
		},
		common.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to create net %s: %w", s.Name, err)
	}
	s.NetID = subnetTx.ID()

	return nil
}

func (s *Net) CreateChains(ctx context.Context, log log.Logger, uri string) error {
	wallet, err := s.GetWallet(ctx, uri)
	if err != nil {
		return err
	}
	pWallet := wallet.P()

	log.Info("creating chains for subnet",
		zap.String("subnet", s.Name),
	)

	for _, chain := range s.Chains {
		createChainTx, err := pWallet.IssueCreateChainTx(
			s.NetID,
			chain.Genesis,
			chain.VMID,
			nil,
			"",
			common.WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf("failed to create chain: %w", err)
		}
		chain.ChainID = createChainTx.ID()

		log.Info("created chain",
			zap.Stringer("chain", chain.ChainID),
			zap.String("subnet", s.Name),
			zap.Stringer("vm", chain.VMID),
		)
	}
	return nil
}

// Add validators to the subnet
func (s *Net) AddValidators(ctx context.Context, log log.Logger, apiURI string, nodes ...*Node) error {
	wallet, err := s.GetWallet(ctx, apiURI)
	if err != nil {
		return err
	}
	pWallet := wallet.P()

	// Collect the end times for current validators to reuse for net validators
	pvmClient := platformvm.NewClient(apiURI)
	validators, err := pvmClient.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
	if err != nil {
		return err
	}
	endTimes := make(map[ids.NodeID]uint64)
	for _, validator := range validators {
		endTimes[validator.NodeID] = validator.EndTime
	}

	startTime := time.Now().Add(DefaultValidatorStartTimeDiff)
	for _, node := range nodes {
		endTime, ok := endTimes[node.NodeID]
		if !ok {
			return fmt.Errorf("failed to find end time for %s", node.NodeID)
		}

		_, err := pWallet.IssueAddNetValidatorTx(
			&txs.NetValidator{
				Validator: txs.Validator{
					NodeID: node.NodeID,
					Start:  uint64(startTime.Unix()),
					End:    endTime,
					Wght:   units.Schmeckle,
				},
				Net: s.NetID,
			},
			common.WithContext(ctx),
		)
		if err != nil {
			return err
		}

		log.Info("added validator to subnet",
			zap.String("subnet", s.Name),
			zap.Stringer("nodeID", node.NodeID),
		)
	}

	return nil
}

// Write the subnet configuration to disk
func (s *Net) Write(subnetDir string) error {
	if err := os.MkdirAll(subnetDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create net dir: %w", err)
	}

	tmpnetConfigPath := filepath.Join(subnetDir, s.Name+jsonFileSuffix)

	// Since subnets are expected to be serialized for the first time
	// without their chains having been created (i.e. chains will have
	// empty IDs), use the absence of chain IDs as a prompt for a
	// net name uniqueness check.
	if len(s.Chains) > 0 && s.Chains[0].ChainID == ids.Empty {
		_, err := os.Stat(tmpnetConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			return fmt.Errorf("a net with name %s already exists", s.Name)
		}
	}

	// Write net configuration for tmpnet
	bytes, err := DefaultJSONMarshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal tmpnet net %s: %w", s.Name, err)
	}
	if err := os.WriteFile(tmpnetConfigPath, bytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write tmpnet net config %s: %w", s.Name, err)
	}

	return nil
}

// HasChainConfig indicates whether at least one of the subnet's
// chains have explicit configuration. This can be used to determine
// whether validator restart is required after chain creation to
// ensure that chains are configured correctly.
func (s *Net) HasChainConfig() bool {
	for _, chain := range s.Chains {
		if len(chain.Config) > 0 {
			return true
		}
	}
	return false
}

func WaitForActiveValidators(
	ctx context.Context,
	log log.Logger,
	pChainClient *platformvm.Client,
	subnet *Net,
) error {
	ticker := time.NewTicker(DefaultPollingInterval)
	defer ticker.Stop()

	log.Info("waiting for subnet validators to become active",
		zap.String("subnet", subnet.Name),
	)

	for {
		validators, err := pChainClient.GetCurrentValidators(ctx, subnet.NetID, nil)
		if err != nil {
			return err
		}
		validatorSet := set.NewSet[ids.NodeID](len(validators))
		for _, validator := range validators {
			validatorSet.Add(validator.NodeID)
		}
		allActive := true
		for _, validatorID := range subnet.ValidatorIDs {
			if !validatorSet.Contains(validatorID) {
				allActive = false
			}
		}
		if allActive {
			log.Info("saw the expected active validators of the subnet",
				zap.String("subnet", subnet.Name),
			)
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see the expected active validators of subnet %q before timeout: %w", subnet.Name, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Reads subnets from [network dir]/subnets/[subnet name].json
func readNets(subnetDir string) ([]*Net, error) {
	entries, err := os.ReadDir(subnetDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read subnet dir: %w", err)
	}

	subnets := []*Net{}
	for _, entry := range entries {
		if entry.IsDir() {
			// Looking only for files
			continue
		}
		fileName := entry.Name()
		if filepath.Ext(fileName) != jsonFileSuffix {
			// Net files should have a .json extension
			continue
		}

		subnetPath := filepath.Join(subnetDir, fileName)
		bytes, err := os.ReadFile(subnetPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read net file %s: %w", subnetPath, err)
		}
		subnet := &Net{}
		if err := json.Unmarshal(bytes, subnet); err != nil {
			return nil, fmt.Errorf("failed to unmarshal net from %s: %w", subnetPath, err)
		}
		subnets = append(subnets, subnet)
	}

	return subnets, nil
}
