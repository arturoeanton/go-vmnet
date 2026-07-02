package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Console::WriteLine", false, consoleWriteLine)
}

func consoleWriteLine(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		fmt.Println()
		return runtime.Value{}, nil
	}
	fmt.Println(args[0].String())
	return runtime.Value{}, nil
}
