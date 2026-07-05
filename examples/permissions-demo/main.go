// Command permissions-demo shows vmnet's deny-by-default capability gate
// (Fase 3.59, docs/en/security.md) end to end against real disk I/O: the
// same real C# code (Vmnet.Fixtures.FileIO, reused from vmnet's own test
// fixtures the same way examples/hello reuses SimpleMath/Strings) behaves
// completely differently depending only on how the embedding Go program
// configures vm.Permissions() before loading it — nothing in the C# source
// changes between the "denied" and "granted" runs below.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	vmnet "github.com/arturoeanton/go-vmnet"
)

const fixtureDLL = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func main() {
	dir, err := os.MkdirTemp("", "vmnet-permissions-demo-*")
	if err != nil {
		log.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "secret.txt")

	fmt.Println("--- 1. A fresh VM denies real file I/O by default ---")
	denied := vmnet.New() // vm.Permissions() is all zero values: everything denied.
	asmDenied, err := denied.LoadFile(fixtureDLL)
	if err != nil {
		log.Fatalf("LoadFile: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release` first)", err)
	}
	_, err = asmDenied.Call("Vmnet.Fixtures.FileIO", "WriteThenReadText", vmnet.String(path), vmnet.String("top secret"))
	fmt.Printf("File.WriteAllText/ReadAllText with no Permissions granted: %v\n", err)

	caught, err := asmDenied.Call("Vmnet.Fixtures.FileIO", "ReadCatchingUnauthorized", vmnet.String(path))
	if err != nil {
		log.Fatalf("ReadCatchingUnauthorized: %v", err)
	}
	fmt.Printf("The C# code itself can catch it too: File.ReadAllText -> caught %q\n", caught.Native())
	if _, statErr := os.Stat(path); statErr == nil {
		log.Fatal("a real file was created despite every permission being denied — this is a bug")
	}
	fmt.Println("Confirmed: no file exists on disk at all — the gate ran before any real syscall.")

	fmt.Println("\n--- 2. Granting AllowFileRead/AllowFileWrite makes the exact same C# code do real I/O ---")
	granted := vmnet.New()
	granted.Permissions().AllowFileRead = true
	granted.Permissions().AllowFileWrite = true
	asmGranted, err := granted.LoadFile(fixtureDLL)
	if err != nil {
		log.Fatalf("LoadFile: %v", err)
	}
	roundTripped, err := asmGranted.Call("Vmnet.Fixtures.FileIO", "WriteThenReadText", vmnet.String(path), vmnet.String("top secret"))
	if err != nil {
		log.Fatalf("WriteThenReadText: %v", err)
	}
	fmt.Printf("File.WriteAllText/ReadAllText, now granted -> %q\n", roundTripped.Native())

	onDisk, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("independently re-reading the file from Go: %v", err)
	}
	fmt.Printf("Independently re-read from Go (not through vmnet at all): %q — a real file, not an illusion.\n", string(onDisk))

	fmt.Println("\n--- 3. Granting only AllowFileRead still denies a write ---")
	readOnly := vmnet.New()
	readOnly.Permissions().AllowFileRead = true
	asmReadOnly, err := readOnly.LoadFile(fixtureDLL)
	if err != nil {
		log.Fatalf("LoadFile: %v", err)
	}
	otherPath := filepath.Join(dir, "should-not-exist.txt")
	_, err = asmReadOnly.Call("Vmnet.Fixtures.FileIO", "WriteThenReadText", vmnet.String(otherPath), vmnet.String("nope"))
	fmt.Printf("File.WriteAllText with only AllowFileRead granted: %v\n", err)
}
