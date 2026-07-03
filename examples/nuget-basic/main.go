// Command nuget-basic is the Fase 3 NuGet demo: add a real, published
// package as a dependency, restore it (downloading + resolving from
// nuget.org for real), and call one of its functions from Go. See
// docs/en/ROADMAP.md, "Demo de cierre de Fase 3", and docs/en/ROADMAP.md's
// certification section for why SimpleBase was picked (small, zero
// dependencies, and known to fully execute under vmnet's current
// profile).
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("SimpleBase", "4.0.0"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	fmt.Println("added SimpleBase@4.0.0 to", vmnet.NuGetManifestFile)

	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}
	fmt.Println("restored dependencies into", vmnet.NuGetLockFile)

	packages, err := vm.NuGet().Packages()
	if err != nil {
		log.Fatalf("NuGet().Packages: %v", err)
	}
	for _, p := range packages {
		fmt.Printf("  %s@%s -> %s\n", p.ID, p.Version, p.SelectedAsset)
	}

	asm, err := vm.LoadPackage("SimpleBase")
	if err != nil {
		log.Fatalf("LoadPackage: %v", err)
	}

	for _, n := range []int32{0, 8, 16, 100, 1000} {
		out, err := asm.Call("SimpleBase.Base32", "getAllocationByteCountForDecoding", vmnet.Int32(n))
		if err != nil {
			log.Fatalf("Call: %v", err)
		}
		fmt.Printf("SimpleBase.Base32.getAllocationByteCountForDecoding(%d) = %v\n", n, out.Native())
	}
}
