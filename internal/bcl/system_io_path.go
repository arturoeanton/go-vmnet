package bcl

import (
	"os"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// pathStaticsType backs System.IO.Path's static fields (Fase 3.39) —
// DirectorySeparatorChar/AltDirectorySeparatorChar, the only ones found in
// real use (`ldsfld System.IO.Path::DirectorySeparatorChar`, from NPOI's
// own SupBookRecord.PATH_SEPERATOR field initializer). Always '/' —
// vmnet has no concept of "the target OS this program will run on" since
// it interprets the same IL identically everywhere, and no real case in
// this loop's target packages does anything path-separator-sensitive with
// the value (SupBookRecord only ever stores it in a field, never branches
// on it while reading a real workbook).
var pathStaticsType = runtime.NewType("System.IO", "Path", nil,
	[]string{"DirectorySeparatorChar", "AltDirectorySeparatorChar"}, nil,
	[]runtime.Value{runtime.Int32('/'), runtime.Int32('/')})

func init() {
	registerStaticFieldHost(pathStaticsType)
	register("System.IO.Path::GetExtension", true, pathGetExtension)
	register("System.IO.Path::GetFileName", true, pathGetFileName)
	register("System.IO.Path::GetFileNameWithoutExtension", true, pathGetFileNameWithoutExtension)
	register("System.IO.Path::Combine", true, pathCombine)
	register("System.IO.Path::GetTempFileName", true, pathGetTempFileName)
}

func pathLastSep(s string) int {
	return strings.LastIndexAny(s, "/\\")
}

func pathGetFileName(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	s := args[0].Str
	if i := pathLastSep(s); i >= 0 {
		return runtime.String(s[i+1:]), nil
	}
	return runtime.String(s), nil
}

func pathGetExtension(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	name := args[0].Str
	if i := pathLastSep(name); i >= 0 {
		name = name[i+1:]
	}
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return runtime.String(""), nil
	}
	return runtime.String(name[dot:]), nil
}

func pathGetFileNameWithoutExtension(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	s := args[0].Str
	if i := pathLastSep(s); i >= 0 {
		s = s[i+1:]
	}
	if dot := strings.LastIndexByte(s, '.'); dot > 0 {
		s = s[:dot]
	}
	return runtime.String(s), nil
}

// pathCombine backs every real overload (2/3/4 string args, or a single
// string[] — vmnet's native registry has no arity-based dispatch, so this
// switches on the actual argument shape) — the common real semantics: any
// segment that is itself rooted ("/...") discards everything before it.
func pathCombine(args []runtime.Value) (runtime.Value, error) {
	var parts []string
	if len(args) == 1 && args[0].Kind == runtime.KindArray && args[0].Arr != nil {
		for _, e := range args[0].Arr.Elems {
			if e.Kind == runtime.KindString {
				parts = append(parts, e.Str)
			}
		}
	} else {
		for _, a := range args {
			if a.Kind == runtime.KindString {
				parts = append(parts, a.Str)
			}
		}
	}
	result := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
			result = p
			continue
		}
		if result == "" || strings.HasSuffix(result, "/") || strings.HasSuffix(result, "\\") {
			result += p
		} else {
			result += "/" + p
		}
	}
	return runtime.String(result), nil
}

func pathGetTempFileName(args []runtime.Value) (runtime.Value, error) {
	f, err := os.CreateTemp("", "vmnet-tmp-*")
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
	}
	name := f.Name()
	f.Close()
	return runtime.String(name), nil
}
