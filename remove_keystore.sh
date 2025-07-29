#!/bin/bash

# Remove keystore-related methods from service.go
# We'll replace these methods with simple error returns

cat > /tmp/removed_methods.go << 'EOF'
// CreateAsset - REMOVED: Keystore functionality has been removed
func (s *Service) CreateAsset(_ *http.Request, _ *CreateAssetArgs, _ *AssetIDChangeAddr) error {
	return errors.New("CreateAsset API has been removed - keystore functionality is no longer supported")
}

// CreateVariableCapAsset - REMOVED: Keystore functionality has been removed
func (s *Service) CreateVariableCapAsset(_ *http.Request, _ *CreateAssetArgs, _ *AssetIDChangeAddr) error {
	return errors.New("CreateVariableCapAsset API has been removed - keystore functionality is no longer supported")
}

// CreateNFTAsset - REMOVED: Keystore functionality has been removed
func (s *Service) CreateNFTAsset(_ *http.Request, _ *CreateNFTAssetArgs, _ *AssetIDChangeAddr) error {
	return errors.New("CreateNFTAsset API has been removed - keystore functionality is no longer supported")
}

// CreateAddress - REMOVED: Keystore functionality has been removed
func (s *Service) CreateAddress(_ *http.Request, _ *api.UserPass, _ *api.JSONAddress) error {
	return errors.New("CreateAddress API has been removed - keystore functionality is no longer supported")
}

// ListAddresses - REMOVED: Keystore functionality has been removed
func (s *Service) ListAddresses(_ *http.Request, _ *api.UserPass, _ *api.JSONAddresses) error {
	return errors.New("ListAddresses API has been removed - keystore functionality is no longer supported")
}

// ExportKey - REMOVED: Keystore functionality has been removed
func (s *Service) ExportKey(_ *http.Request, _ *ExportKeyArgs, _ *ExportKeyReply) error {
	return errors.New("ExportKey API has been removed - keystore functionality is no longer supported")
}

// ImportKey - REMOVED: Keystore functionality has been removed
func (s *Service) ImportKey(_ *http.Request, _ *ImportKeyArgs, _ *api.JSONAddress) error {
	return errors.New("ImportKey API has been removed - keystore functionality is no longer supported")
}

// Send - REMOVED: Keystore functionality has been removed
func (s *Service) Send(_ *http.Request, _ *SendArgs, _ *api.JSONTxIDChangeAddr) error {
	return errors.New("Send API has been removed - keystore functionality is no longer supported")
}

// SendMultiple - REMOVED: Keystore functionality has been removed
func (s *Service) SendMultiple(_ *http.Request, _ *SendMultipleArgs, _ *api.JSONTxIDChangeAddr) error {
	return errors.New("SendMultiple API has been removed - keystore functionality is no longer supported")
}

// Mint - REMOVED: Keystore functionality has been removed
func (s *Service) Mint(_ *http.Request, _ *MintArgs, _ *api.JSONTxIDChangeAddr) error {
	return errors.New("Mint API has been removed - keystore functionality is no longer supported")
}

// MintNFT - REMOVED: Keystore functionality has been removed
func (s *Service) MintNFT(_ *http.Request, _ *MintNFTArgs, _ *api.JSONTxIDChangeAddr) error {
	return errors.New("MintNFT API has been removed - keystore functionality is no longer supported")
}

// Import - REMOVED: Keystore functionality has been removed
func (s *Service) Import(_ *http.Request, _ *ImportArgs, _ *api.JSONTxID) error {
	return errors.New("Import API has been removed - keystore functionality is no longer supported")
}

// Export - REMOVED: Keystore functionality has been removed
func (s *Service) Export(_ *http.Request, _ *ExportArgs, _ *api.JSONTxIDChangeAddr) error {
	return errors.New("Export API has been removed - keystore functionality is no longer supported")
}
EOF

echo "Removing keystore-related code from XVM..."

# Find all test files that test keystore functionality and remove them
rm -f vms/xvm/service_test.go

# Remove keystore field from envConfig struct
sed -i '/keystoreUsers.*\[\]\*user/d' vms/xvm/environment_test.go

# Remove keystore initialization comments from environment_test.go
sed -i '/Keystore functionality has been removed/,/^[[:space:]]*$/d' vms/xvm/environment_test.go

# Remove benchmark tests that use keystore
sed -i '/func.*BenchmarkLoadUser/,/^}/d' vms/xvm/vm_benchmark_test.go

# Remove keystore-related tests from wallet_service_test.go
rm -f vms/xvm/wallet_service_test.go

echo "Done removing keystore functionality"