// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"testing"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
)

func TestLabelGatherer_Registration(t *testing.T) {
	const (
		firstName  = "first"
		secondName = "second"
	)
	firstLabeledGatherer := &labeledGatherer{
		labelValue: firstName,
		gatherer:   &testGatherer{},
	}
	firstLabelGatherer := func() *labelGatherer {
		return &labelGatherer{
			multiGatherer: multiGatherer{
				names: []string{firstLabeledGatherer.labelValue},
				gatherers: []metric.Gatherer{
					firstLabeledGatherer,
				},
			},
		}
	}
	secondLabeledGatherer := &labeledGatherer{
		labelValue: secondName,
		gatherer: &testGatherer{
			mfs: []*metric.MetricFamily{{}},
		},
	}
	secondLabelGatherer := func() *labelGatherer {
		return &labelGatherer{
			multiGatherer: multiGatherer{
				names: []string{
					firstLabeledGatherer.labelValue,
					secondLabeledGatherer.labelValue,
				},
				gatherers: metric.Gatherers{
					firstLabeledGatherer,
					secondLabeledGatherer,
				},
			},
		}
	}
	onlySecondLabeledGatherer := &labelGatherer{
		multiGatherer: multiGatherer{
			names: []string{
				secondLabeledGatherer.labelValue,
			},
			gatherers: metric.Gatherers{
				secondLabeledGatherer,
			},
		},
	}

	registerTests := []struct {
		name                  string
		labelGatherer         *labelGatherer
		labelValue            string
		gatherer              metric.Gatherer
		expectedErr           error
		expectedLabelGatherer *labelGatherer
	}{
		{
			name:                  "first registration",
			labelGatherer:         &labelGatherer{},
			labelValue:            firstName,
			gatherer:              firstLabeledGatherer.gatherer,
			expectedErr:           nil,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:                  "second registration",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            secondName,
			gatherer:              secondLabeledGatherer.gatherer,
			expectedErr:           nil,
			expectedLabelGatherer: secondLabelGatherer(),
		},
		{
			name:                  "conflicts with previous registration",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            firstName,
			gatherer:              secondLabeledGatherer.gatherer,
			expectedErr:           errDuplicateGatherer,
			expectedLabelGatherer: firstLabelGatherer(),
		},
	}
	for _, test := range registerTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			err := test.labelGatherer.Register(test.labelValue, test.gatherer)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedLabelGatherer, test.labelGatherer)
		})
	}

	deregisterTests := []struct {
		name                  string
		labelGatherer         *labelGatherer
		labelValue            string
		expectedRemoved       bool
		expectedLabelGatherer *labelGatherer
	}{
		{
			name:                  "remove from nothing",
			labelGatherer:         &labelGatherer{},
			labelValue:            firstName,
			expectedRemoved:       false,
			expectedLabelGatherer: &labelGatherer{},
		},
		{
			name:                  "remove unknown name",
			labelGatherer:         firstLabelGatherer(),
			labelValue:            secondName,
			expectedRemoved:       false,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:            "remove first name",
			labelGatherer:   firstLabelGatherer(),
			labelValue:      firstName,
			expectedRemoved: true,
			expectedLabelGatherer: &labelGatherer{
				multiGatherer: multiGatherer{
					// We must populate with empty slices rather than nil slices
					// to pass the equality check.
					names:     []string{},
					gatherers: metric.Gatherers{},
				},
			},
		},
		{
			name:                  "remove second name",
			labelGatherer:         secondLabelGatherer(),
			labelValue:            secondName,
			expectedRemoved:       true,
			expectedLabelGatherer: firstLabelGatherer(),
		},
		{
			name:                  "remove only first name",
			labelGatherer:         secondLabelGatherer(),
			labelValue:            firstName,
			expectedRemoved:       true,
			expectedLabelGatherer: onlySecondLabeledGatherer,
		},
	}
	for _, test := range deregisterTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			removed := test.labelGatherer.Deregister(test.labelValue)
			require.Equal(test.expectedRemoved, removed)
			require.Equal(test.expectedLabelGatherer, test.labelGatherer)
		})
	}
}
