package e2e_test

import (
	"testing"
)

func TestE2ESkipGracefully(t *testing.T) {
	t.Skip("Skipping e2e tests - requires running network infrastructure")
}
