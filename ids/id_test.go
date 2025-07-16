package ids

import (
	"testing"
	
	"github.com/stretchr/testify/require"
	"github.com/mr-tron/base58/base58"
)

func TestFromStringWithForce(t *testing.T) {
	tests := []struct {
		name                string
		idStr               string
		forceIgnoreChecksum bool
		wantErr             bool
	}{
		{
			name:                "valid ID with checksum",
			idStr:               "TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES",
			forceIgnoreChecksum: false,
			wantErr:             false,
		},
		{
			name:                "invalid checksum without force",
			idStr:               "tJqmx13PV8UPQJBbuumANQCKnfPUHCxfahdG29nJa6BHkumCK",
			forceIgnoreChecksum: false,
			wantErr:             true,
		},
		{
			name:                "invalid checksum with force",
			idStr:               "tJqmx13PV8UPQJBbuumANQCKnfPUHCxfahdG29nJa6BHkumCK",
			forceIgnoreChecksum: true,
			wantErr:             false,
		},
		{
			name:                "LUX blockchain ID with force",
			idStr:               "dnmzhuf6poM6PUNQCe7MWWfBdTJEnddhHRNXz2x7H6qSmyBEJ",
			forceIgnoreChecksum: true,
			wantErr:             false,
		},
		{
			name:                "ZOO subnet ID with force",
			idStr:               "xJzemKCLvBNgzYHoBHzXQr9uesR3S3kf3YtZ5mPHTA9LafK6L",
			forceIgnoreChecksum: true,
			wantErr:             false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := FromStringWithForce(tt.idStr, tt.forceIgnoreChecksum)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Verify we got a valid ID
				require.NotEqual(t, Empty, id)
				
				// If force was used, verify the ID matches what we expect
				if tt.forceIgnoreChecksum && err == nil {
					// Decode and verify we got the right bytes
					rawBytes, _ := base58.Decode(tt.idStr)
					if len(rawBytes) >= IDLen {
						var expectedID ID
						copy(expectedID[:], rawBytes[:IDLen])
						require.Equal(t, expectedID, id)
					}
				}
			}
		})
	}
}

func TestHistoricSubnetIDs(t *testing.T) {
	// Test all our historic subnet IDs work with force flag
	historicIDs := []string{
		"tJqmx13PV8UPQJBbuumANQCKnfPUHCxfahdG29nJa6BHkumCK",  // LUX subnet
		"xJzemKCLvBNgzYHoBHzXQr9uesR3S3kf3YtZ5mPHTA9LafK6L",  // ZOO subnet
		"2hMMhMFfVvpCFrA9LBGS3j5zr5XfARuXdLLYXKpJR3RpnrunH9", // SPC subnet
		"dnmzhuf6poM6PUNQCe7MWWfBdTJEnddhHRNXz2x7H6qSmyBEJ",  // LUX blockchain
		"bXe2MhhAnXg6WGj6G8oDk55AKT1dMMsN72S8te7JdvzfZX1zM",  // ZOO blockchain
		"QFAFyn1hh59mh7kokA55dJq5ywskF5A1yn8dDpLhmKApS6FP1",  // SPC blockchain
	}
	
	for _, idStr := range historicIDs {
		// Should fail without force
		_, err := FromString(idStr)
		require.Error(t, err, "ID %s should fail without force", idStr)
		
		// Should succeed with force
		id, err := FromStringWithForce(idStr, true)
		require.NoError(t, err, "ID %s should succeed with force", idStr)
		require.NotEqual(t, Empty, id)
	}
}