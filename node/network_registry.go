package node

import (
	"fmt"
	"sync"

	"github.com/luxfi/node/chains"
	"github.com/luxfi/database"
	"github.com/luxfi/nodevalidators"
	"github.com/luxfi/node/config"
)

// NetworkContext contains all components specific to a single network
type NetworkContext struct {
	NetworkID     uint32
	ChainManager  chains.Manager
	Validators    *nodevalidators.Manager
	Database      database.Database
	Bootstrappers *nodevalidators.Manager
	Config        *config.Config
	ChainDataDir  string
	Active        bool
}

// NetworkRegistry manages multiple network contexts
type NetworkRegistry struct {
	mu       sync.RWMutex
	networks map[uint32]*NetworkContext
	primary  uint32 // Primary network ID for backwards compatibility
}

// NewNetworkRegistry creates a new network registry
func NewNetworkRegistry() *NetworkRegistry {
	return &NetworkRegistry{
		networks: make(map[uint32]*NetworkContext),
		primary:  96369, // Default to Lux mainnet
	}
}

// Register adds a new network context
func (r *NetworkRegistry) Register(networkID uint32, ctx *NetworkContext) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.networks[networkID]; exists {
		return fmt.Errorf("network %d already registered", networkID)
	}

	r.networks[networkID] = ctx

	// First network becomes primary if not set
	if len(r.networks) == 1 {
		r.primary = networkID
	}

	return nil
}

// Get retrieves a network context
func (r *NetworkRegistry) Get(networkID uint32) (*NetworkContext, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ctx, exists := r.networks[networkID]
	if !exists {
		return nil, fmt.Errorf("network %d not found", networkID)
	}
	return ctx, nil
}

// GetPrimary retrieves the primary network context
func (r *NetworkRegistry) GetPrimary() (*NetworkContext, error) {
	return r.Get(r.primary)
}

// SetPrimary sets the primary network
func (r *NetworkRegistry) SetPrimary(networkID uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.networks[networkID]; !exists {
		return fmt.Errorf("network %d not registered", networkID)
	}
	r.primary = networkID
	return nil
}

// All returns all registered networks
func (r *NetworkRegistry) All() map[uint32]*NetworkContext {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[uint32]*NetworkContext, len(r.networks))
	for k, v := range r.networks {
		result[k] = v
	}
	return result
}

// Remove unregisters a network
func (r *NetworkRegistry) Remove(networkID uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if networkID == r.primary {
		return fmt.Errorf("cannot remove primary network %d", networkID)
	}

	ctx, exists := r.networks[networkID]
	if !exists {
		return fmt.Errorf("network %d not found", networkID)
	}

	// Cleanup network resources
	if ctx.ChainManager != nil {
		ctx.ChainManager.Shutdown()
	}
	if ctx.Database != nil {
		ctx.Database.Close()
	}

	delete(r.networks, networkID)
	return nil
}

// IsRegistered checks if a network is registered
func (r *NetworkRegistry) IsRegistered(networkID uint32) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.networks[networkID]
	return exists
}

// Count returns the number of registered networks
func (r *NetworkRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.networks)
}

// NetworkIDs returns all registered network IDs
func (r *NetworkRegistry) NetworkIDs() []uint32 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]uint32, 0, len(r.networks))
	for id := range r.networks {
		ids = append(ids, id)
	}
	return ids
}