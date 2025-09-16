// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/perms"
)

const (
	initTimeout = 2 * time.Minute

	BootstrapStartingMessage = "Starting bootstrap test"
	BootstrapResumingMessage = "Resuming bootstrap test"
)

func NodeDataDir(path string) string {
	return path + "/luxd"
}

func InitBootstrapTest(log log.Logger, namespace string, podName string, nodeContainerName string, dataDir string) error {
	clientset, err := getClientset(log)
	if err != nil {
		return fmt.Errorf("failed to get clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	log.Info("Retrieving pod to determine bootstrap test config",
		"namespace", namespace,
		"pod", podName,
		"container", nodeContainerName,
	)
	testConfig, err := GetBootstrapTestConfigFromPod(ctx, clientset, namespace, podName, nodeContainerName)
	if err != nil {
		return fmt.Errorf("failed to determine bootstrap test config: %w", err)
	}
	log.Info("Retrieved bootstrap test config", log.Reflect("testConfig", testConfig))

	// If the image uses the latest tag, determine the latest image id and set the container image to that
	if strings.HasSuffix(testConfig.Image, ":latest") {
		log.Info("Determining image id for image", "image", testConfig.Image)
		latestImageDetails, err := getLatestImageDetails(ctx, log, clientset, namespace, testConfig.Image, nodeContainerName)
		if err != nil {
			return fmt.Errorf("failed to get latest image details: %w", err)
		}
		log.Info("Updating owning statefulset with image details",
			"image", latestImageDetails.Image,
			log.Reflect("versions", latestImageDetails.Versions),
		)
		if err := setImageDetails(ctx, log, clientset, namespace, podName, latestImageDetails); err != nil {
			return fmt.Errorf("failed to set container image: %w", err)
		}
	}

	// A bootstrap is being resumed if a version file exists and the image name it contains matches the container
	// image. If a bootstrap is being started, the version file should be created and the data path cleared.

	testDetailsPath := getTestDetailsPath(dataDir)

	var testDetails bootstrapTestDetails
	if testDetailsBytes, err := os.ReadFile(testDetailsPath); errors.Is(err, os.ErrNotExist) {
		log.Info("Test details file does not exist", "path", testDetailsPath)
	} else if err != nil {
		return fmt.Errorf("failed to read test details file: %w", err)
	} else {
		if err := json.Unmarshal(testDetailsBytes, &testDetails); err != nil {
			return fmt.Errorf("failed to unmarshal test details: %w", err)
		}
		log.Info("Loaded test details", log.Reflect("testDetails", testDetails))
	}

	if testDetails.Image == testConfig.Image {
		log.Info("Test details image matches test config image")
		log.Info(BootstrapResumingMessage, log.Reflect("testConfig", testConfig))
		return nil
	} else if len(testDetails.Image) > 0 {
		log.Info("Test details image differs from test config image")
	}

	nodeDataDir := NodeDataDir(dataDir)
	log.Info("Removing node directory", "path", nodeDataDir)
	if err := os.RemoveAll(nodeDataDir); err != nil {
		return fmt.Errorf("failed to remove contents of node directory: %w", err)
	}

	log.Info("Writing test details to file",
		log.Reflect("testDetails", testDetails),
		"path", testDetailsPath,
	)
	testDetails = bootstrapTestDetails{
		Image:     testConfig.Image,
		StartTime: time.Now(),
	}
	testDetailsBytes, err := json.Marshal(testDetails)
	if err != nil {
		return fmt.Errorf("failed to marshal test details: %w", err)
	}
	if err := os.WriteFile(testDetailsPath, testDetailsBytes, perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write test details to file: %w", err)
	}

	log.Info(BootstrapStartingMessage,
		log.Reflect("testConfig", testConfig),
		"startTime", testDetails.StartTime.Format(time.RFC3339),
	)

	return nil
}
