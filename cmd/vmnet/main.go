// Command vmnet is the CLI for the vmnet IL/CIL runtime: inspect, il,
// check, run, add, restore and packages (see docs/ROADMAP.md, Fase 1-3).
// Not yet implemented — this is Phase 0 scaffolding.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "vmnet: no commands implemented yet (see docs/ROADMAP.md)")
	os.Exit(1)
}
