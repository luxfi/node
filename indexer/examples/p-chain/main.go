// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/luxfi/node/indexer"
	"github.com/luxfi/node/wallet/subnet/primary"

	platformvmblock "github.com/luxfi/node/vms/platformvm/block"
	proposervmblock "github.com/luxfi/node/vms/proposervm/block"
)

// This example program continuously polls for the next P-Chain block
// and prints the ID of the block and its transactions.
func main() {
	var (
		uri       = fmt.Sprintf("%s/ext/index/P/block", primary.LocalAPIURI)
		client    = indexer.NewClient(uri)
		ctx       = context.Background()
		nextIndex uint64
	)
	for {
		container, err := client.GetContainerByIndex(ctx, nextIndex)
		if err != nil {
			time.Sleep(time.Second)
			log.Printf("polling for next accepted block\n")
			continue
		}

		platformvmBlockBytes := container.Bytes
		proposerVMBlock, err := proposervmblock.Parse(container.Bytes)
		if err == nil {
			platformvmBlockBytes = proposerVMBlock.Block()
		}

		platformvmBlock, err := platformvmblock.Parse(platformvmblock.Codec, platformvmBlockBytes)
		if err != nil {
			log.Fatalf("failed to parse platformvm block: %s\n", err)
		}

		acceptedTxs := platformvmBlock.Txs()
		log.Printf("accepted block %s with %d transactions\n", platformvmBlock.ID(), len(acceptedTxs))

		for _, tx := range acceptedTxs {
			log.Printf("accepted transaction %s\n", tx.ID())
		}

		nextIndex++
	}
}
