// (c) 2019-2020, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"fmt"
	"sync"
)

// VMFactory creates VM instances
type VMFactory interface {
	New() (interface{}, error)
}

// Registry manages VM factories
type Registry struct {
	mu        sync.RWMutex
	factories map[string]VMFactory
}

// New creates a new registry
func New() *Registry {
	return &Registry{
		factories: make(map[string]VMFactory),
	}
}

// Register registers a VM factory
func (r *Registry) Register(name string, factory VMFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("VM %s already registered", name)
	}
	
	r.factories[name] = factory
	return nil
}

// Get retrieves a VM factory
func (r *Registry) Get(name string) (VMFactory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	factory, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("VM %s not found", name)
	}
	
	return factory, nil
}

// List returns all registered VM names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// Global registry instance
var globalRegistry = New()

// Register registers a VM factory in the global registry
func Register(name string, factory VMFactory) error {
	return globalRegistry.Register(name, factory)
}

// Get retrieves a VM factory from the global registry
func Get(name string) (VMFactory, error) {
	return globalRegistry.Get(name)
}

// List returns all registered VM names from the global registry
func List() []string {
	return globalRegistry.List()
}
