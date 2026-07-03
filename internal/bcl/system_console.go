package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Console::WriteLine", false, consoleWriteLine)
	register("System.Console::Write", false, consoleWrite)
}

func consoleWriteLine(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		fmt.Println()
		return runtime.Value{}, nil
	}
	fmt.Println(args[0].String())
	return runtime.Value{}, nil
}

func consoleWrite(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, nil
	}
	fmt.Print(displayString(args[0]))
	return runtime.Value{}, nil
}
