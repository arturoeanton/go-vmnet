package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

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
}
