<<<<<<< HEAD
// (c) 2019-2020, Lux Industries, Inc. All rights reserved.
=======
// (c) 2020-2020, Lux Industries, Inc. All rights reserved.
>>>>>>> main
// See the file LICENSE for licensing terms.

package loader

import (
	"fmt"
<<<<<<< HEAD
	"plugin"
	
=======

>>>>>>> main
	"github.com/luxfi/node/vms/registry"
)

// LoadPlugins loads all available VM plugins
func LoadPlugins() error {
	// For compiled-in plugins, they register themselves via init()
	// For dynamic plugins, load them here
<<<<<<< HEAD
	
=======

>>>>>>> main
	// Example of loading a dynamic plugin:
	// p, err := plugin.Open("path/to/plugin.so")
	// if err != nil {
	//     return err
	// }
<<<<<<< HEAD
	
=======

>>>>>>> main
	// Check if EVM is registered
	if vms := registry.List(); len(vms) == 0 {
		return fmt.Errorf("no VMs registered")
	}
<<<<<<< HEAD
	
=======

>>>>>>> main
	return nil
}
