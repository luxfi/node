// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/utils/packages"
)

func TestMustNotImport(t *testing.T) {
	require := require.New(t)

	mustNotImport := map[string][]string{
		// Importing these packages configures libevm globally. This must not be
		// done to support both coreth and subnet-evm.
		"tests/...": {
			"github.com/ava-labs/coreth/params",
			"github.com/ava-labs/coreth/plugin/evm/customtypes",
		},
	}
	for packageName, forbiddenImports := range mustNotImport {
		packagePath := path.Join("github.com/luxfi/node", packageName)
		imports, err := packages.GetDependencies(packagePath)
		require.NoError(err)

		for _, forbiddenImport := range forbiddenImports {
			require.NotContains(imports, forbiddenImport, "package %s must not import %s, check output of: go list -f '{{ .Deps }}' %q", packageName, forbiddenImport, packagePath)
		}
	}
}
