package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.AppContext backs a real, if narrow, BCL surface: framework
// libraries (e.g. System.SR's own static cctor, hit loading
// DocumentFormat.OpenXml's dependency chain — Fase 3.40) probe
// feature-compatibility switches via TryGetSwitch before falling back to a
// default. vmnet has never set any switch (there's no app config/csproj
// MSBuild property system here), so every switch is simply unset —
// TryGetSwitch always reports isEnabled=false, found=false, exactly what a
// real fresh AppContext with no explicit AppContext.SetSwitch calls does.
var appContextSwitches = map[string]bool{}
var appContextData = map[string]runtime.Value{}

func init() {
	register("System.AppContext::TryGetSwitch", true, appContextTryGetSwitch)
	register("System.AppContext::SetSwitch", false, appContextSetSwitch)
	register("System.AppContext::SetData", false, appContextSetData)
	register("System.AppContext::GetData", true, appContextGetData)
	register("System.AppContext::get_BaseDirectory", true, appContextGetBaseDirectory)
	register("System.AppContext::get_TargetFrameworkName", true, appContextGetTargetFrameworkName)
}

func appContextTryGetSwitch(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: AppContext.TryGetSwitch expects (string, out bool)")
	}
	if args[1].Kind != runtime.KindRef || args[1].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: AppContext.TryGetSwitch expects an out parameter")
	}
	enabled, found := appContextSwitches[args[0].Str]
	*args[1].Ref = runtime.Bool(enabled)
	return runtime.Bool(found), nil
}

func appContextSetSwitch(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: AppContext.SetSwitch expects (string, bool)")
	}
	appContextSwitches[args[0].Str] = args[1].Kind == runtime.KindI4 && args[1].I4 != 0
	return runtime.Value{}, nil
}

func appContextSetData(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: AppContext.SetData expects (string, object)")
	}
	appContextData[args[0].Str] = args[1]
	return runtime.Value{}, nil
}

func appContextGetData(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: AppContext.GetData expects a string name")
	}
	if v, ok := appContextData[args[0].Str]; ok {
		return v, nil
	}
	return runtime.Null(), nil
}

func appContextGetBaseDirectory(args []runtime.Value) (runtime.Value, error) {
	return runtime.String(""), nil
}

func appContextGetTargetFrameworkName(args []runtime.Value) (runtime.Value, error) {
	return runtime.Null(), nil
}
