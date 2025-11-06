// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/log"
	logfields "github.com/luxfi/log"

	"github.com/luxfi/node/api/health"
)

// Waits for the nodes at the provided URIs to report healthy.
func awaitHealthyNodes(ctx context.Context, log log.Logger, uris []string) error {
	for _, uri := range uris {
		if err := awaitHealthyNode(ctx, log, uri); err != nil {
			return err
		}
	}
	log.Info("all nodes reported healthy")
	return nil
}

func awaitHealthyNode(ctx context.Context, log log.Logger, uri string) error {
	client := health.NewClient(uri)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Info("awaiting node health",
		logfields.UserString("uri", uri),
	)
	for {
		res, err := client.Health(ctx, nil)
		switch {
		case err != nil:
			log.Warn("failed to reach node",
				logfields.UserString("uri", uri),
				logfields.Err(err),
			)
		case res.Healthy:
			log.Info("node reported healthy",
				logfields.UserString("uri", uri),
			)
			return nil
		default:
			log.Info("node reported unhealthy",
				logfields.UserString("uri", uri),
			)
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("node health check cancelled at %s: %w", uri, ctx.Err())
		}
	}
}
