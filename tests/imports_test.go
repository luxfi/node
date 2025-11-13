// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
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
		// Directly importing these packages configures the EVM plugin globally.
		// This must not be done to support both geth (go-ethereum) and subnet-evm.
		// Note: Transitive dependencies through geth/common are acceptable.
		"tests/...": {
			"github.com/luxfi/geth/params",
			"github.com/luxfi/geth/plugin/evm/customtypes",
		},
	}
	for packageName, forbiddenImports := range mustNotImport {
		packagePath := path.Join("github.com/luxfi/node", packageName)
		// Use GetDirectImports instead of GetDependencies to only check direct imports,
		// not transitive dependencies. Transitive deps on geth/params are unavoidable
		// when using geth/common for Address and Hash types.
		imports, err := packages.GetDirectImports(packagePath)
		require.NoError(err)

		for _, forbiddenImport := range forbiddenImports {
			require.NotContains(imports, forbiddenImport, "package %s must not directly import %s, check output of: go list -f '{{ .Imports }}' %q", packageName, forbiddenImport, packagePath)
		}
	}
}
