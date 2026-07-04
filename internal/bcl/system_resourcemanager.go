package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Resources.ResourceManager is a stub (Fase 3.40) — same posture
// as CultureInfo (Fase 3.6): vmnet has no localized-resource data
// anywhere. GetString answers the requested resource *name* itself
// rather than Null() — found the hard way: System.Memory's own SR
// helper class calls every one of its message properties as
// `GetResourceString(key, null)`, which just returns whatever GetString
// gives back verbatim (no null-check, since a real ResourceManager
// backed by a real .resources blob never fails for a name the assembly
// itself declares) — and that string then feeds straight into
// string.Format as the format string. Returning Null() there produces
// a genuinely invalid `string.Format(null, ...)`, worse than the
// unlocalized-but-real resource key vmnet can trivially provide instead.
type nativeResourceManager struct{}

func init() {
	registerCtor("System.Resources.ResourceManager", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeResourceManager{}}, nil
	})
	register("System.Resources.ResourceManager::GetString", true, resourceManagerGetString)
}

func resourceManagerGetString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	return args[1], nil
}
