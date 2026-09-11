// Command surface prints the precompile surface THIS luxd dispatches: one line
// per registered module, address and config key. A chain's upgrade.json may only
// name a configKey that appears here — luxd refuses an activation for one it has
// never seen — so this is the list a genesis is allowed to draw from.
package main

import (
	"fmt"
	"os"
	"sort"

	_ "github.com/luxfi/evm/precompile/registry"
	"github.com/luxfi/precompile/modules"
)

func main() {
	all := modules.RegisteredModules()
	rows := make([]string, 0, len(all))
	for _, m := range all {
		rows = append(rows, fmt.Sprintf("%s  %s", m.Address.Hex(), m.ConfigKey))
	}
	sort.Strings(rows)
	for _, r := range rows {
		fmt.Println(r)
	}
	fmt.Fprintf(os.Stderr, "%d modules\n", len(rows))
}
