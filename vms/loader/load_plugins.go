// (c) 2019-2020, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package loader

import (
	"fmt"
	
	"github.com/luxfi/node/vms/registry"
)

// LoadPlugins loads all available VM plugins
func LoadPlugins() error {
	// For compiled-in plugins, they register themselves via init()
	// For dynamic plugins, load them here
	
	// Example of loading a dynamic plugin:
	// p, err := plugin.Open("path/to/plugin.so")
	// if err != nil {
	//     return err
	// }
	
	// Check if EVM is registered
	if vms := registry.List(); len(vms) == 0 {
		return fmt.Errorf("no VMs registered")
	}
	
	return nil
}
