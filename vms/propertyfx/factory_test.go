// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/utils/logging"
)

func TestFactory(t *testing.T) {
	require := require.New(t)

	factory := Factory{}
	fx, err := factory.New(logging.NoLog{})
	require.NoError(err)
	require.NotNil(fx)
}
