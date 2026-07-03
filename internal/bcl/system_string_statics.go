package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// stringStaticsType backs System.String's static fields (Fase 3.27) —
// just Empty, the only one found in real use (`ldsfld System.String::
// Empty`, from string.Empty). See LookupStaticFieldHost's doc comment
// for why this can't share valueTypeRegistry the way TimeSpan.Zero does.
var stringStaticsType = runtime.NewType("System", "String", nil, []string{"Empty"}, nil, []runtime.Value{runtime.String("")})

func init() {
	registerStaticFieldHost(stringStaticsType)
}
