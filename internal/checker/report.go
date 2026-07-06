package checker

// Status is the overall compatibility verdict for an assembly or package
// (spec §23.2-23.4).
type Status string

const (
	StatusCompatible  Status = "compatible"
	StatusPartial     Status = "partial"
	StatusUnsupported Status = "unsupported"
)

// FindingKind groups *why* something is unsupported, so a report can show
// human reasons ("heavy reflection", "async/Task usage") instead of a
// dump of raw opcode/method names (spec §23.3-23.4).
type FindingKind string

const (
	KindUnsupportedOpcode FindingKind = "unsupported-opcode"
	KindUnsupportedMethod FindingKind = "unsupported-bcl-method"
	KindReflection        FindingKind = "reflection"
	KindAsync             FindingKind = "async"
	KindPInvoke           FindingKind = "p-invoke"
	KindUnsafePointer     FindingKind = "unsafe-pointer"
	KindOutOfProfile      FindingKind = "out-of-profile"
)

// Finding is one concrete reason an assembly isn't fully compatible.
type Finding struct {
	Kind       FindingKind
	Method     string // "Namespace.Type::Method" where this was found ("" for assembly-wide findings)
	Detail     string // the opcode, the unresolved call target, ...
	Suggestion string
}

// Report is the result of analyzing one assembly against one Profile.
type Report struct {
	AssemblyName    string
	Profile         Profile
	MethodsAnalyzed int
	MethodsFlagged  int
	Findings        []Finding
	Status          Status

	// PerType breaks MethodsAnalyzed/MethodsFlagged down by declaring
	// type ("Namespace.Type", the same qualified name Finding.Method's
	// own "Type::Method" prefix uses) — a natural byproduct of the same
	// per-method loop AnalyzeWithDeps already runs, kept here so a
	// caller can rank individual TYPES by their own clean-method ratio
	// (e.g. `vmnet analyze`'s own "best migration candidates" list)
	// without re-walking the assembly's metadata a second time.
	PerType map[string]*TypeReport
}

// TypeReport is one TypeDef's own slice of a Report's totals.
type TypeReport struct {
	MethodsAnalyzed int
	MethodsFlagged  int
}

func (r *Report) finalize() {
	switch {
	case len(r.Findings) == 0:
		r.Status = StatusCompatible
	case r.MethodsAnalyzed == 0 || r.MethodsFlagged >= r.MethodsAnalyzed:
		r.Status = StatusUnsupported
	default:
		r.Status = StatusPartial
	}
}
