package integration_test

import (
	"testing"
)

func TestIntegrationSkipGracefully(t *testing.T) {
	t.Skip("Skipping integration tests - requires running network infrastructure")
}
