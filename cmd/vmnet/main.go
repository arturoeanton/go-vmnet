// Command vmnet is the CLI for the vmnet IL/CIL runtime. Fase 1 ships
// inspect/il/run; check/add/restore/packages land in Fase 3 (see
// docs/en/ROADMAP.md).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	vmnet "github.com/arturoeanton/go-vmnet"
	"github.com/arturoeanton/go-vmnet/internal/bind"
	"github.com/arturoeanton/go-vmnet/internal/checker"
	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/migrate"
	"github.com/arturoeanton/go-vmnet/internal/nuget"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "inspect":
		err = runInspect(os.Args[2:])
	case "il":
		err = runIL(os.Args[2:])
	case "run":
		err = runRun(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	case "analyze":
		err = runAnalyze(os.Args[2:])
	case "bind":
		err = runBind(os.Args[2:])
	case "add":
		err = runAdd(os.Args[2:])
	case "restore":
		err = runRestore(os.Args[2:])
	case "packages":
		err = runPackages(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "vmnet: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  vmnet inspect <dll>
  vmnet il <dll> <Type.Method>
  vmnet run <dll> <Type.Method> '<json-array-of-args>'
  vmnet check [--profile=minimal|rules|netstandard-lite] [--html=<file>] <dll>
  vmnet check package [--profile=...] [--html=<file>] <id>@<version>
  vmnet analyze <dir> [--profile=...] [--html=<file>]
  vmnet bind <dll> --out=<dir> [--package=<name>]
  vmnet bind package <id>@<version> --out=<dir> [--package=<name>]
  vmnet add <id>[@<version>]
  vmnet restore
  vmnet packages`)
}

func loadRaw(path string) (*pe.File, *metadata.Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := pe.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		return nil, nil, err
	}
	return f, md, nil
}

func runInspect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vmnet inspect <dll>")
	}
	_, md, err := loadRaw(args[0])
	if err != nil {
		return err
	}

	if asmRow, err := md.Assembly(1); err == nil {
		fmt.Printf("Assembly: %s\n", asmRow.Name)
		fmt.Printf("Version: %d.%d.%d.%d\n", asmRow.MajorVersion, asmRow.MinorVersion, asmRow.BuildNumber, asmRow.RevisionNumber)
	}

	fmt.Println("Types:")
	typeCount := md.RowCount(metadata.TableTypeDef)
	for rid := uint32(1); rid <= typeCount; rid++ {
		t, err := md.TypeDef(rid)
		if err != nil {
			return err
		}
		if t.Name == "<Module>" {
			continue
		}
		fmt.Printf("- %s\n", qualify(t.Namespace, t.Name))
	}

	fmt.Println("Methods:")
	methodCount := md.RowCount(metadata.TableMethodDef)
	for rid := uint32(1); rid <= methodCount; rid++ {
		m, err := md.MethodDef(rid)
		if err != nil {
			return err
		}
		ownerRID, err := md.MethodDefOwner(rid)
		if err != nil {
			continue
		}
		t, err := md.TypeDef(ownerRID)
		if err != nil {
			continue
		}
		fmt.Printf("- %s.%s(...)\n", qualify(t.Namespace, t.Name), m.Name)
	}
	return nil
}

func runIL(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: vmnet il <dll> <Type.Method>")
	}
	f, md, err := loadRaw(args[0])
	if err != nil {
		return err
	}
	typeName, methodName, err := splitTypeMethod(args[1])
	if err != nil {
		return err
	}
	namespace, name := splitLastDot(typeName)
	typeRID, _, err := md.FindTypeDef(namespace, name)
	if err != nil {
		return err
	}
	_, m, err := md.FindMethodDef(typeRID, methodName)
	if err != nil {
		return err
	}
	if m.RVA == 0 {
		return fmt.Errorf("%s.%s has no body (abstract/extern)", typeName, methodName)
	}

	body, err := f.RVA(m.RVA)
	if err != nil {
		return err
	}
	_, code, err := il.ReadMethodBody(body)
	if err != nil {
		return err
	}
	instrs, err := il.Decode(code)
	if err != nil {
		return err
	}
	for _, instr := range instrs {
		if instr.Operand == nil {
			fmt.Printf("IL_%04x: %s\n", instr.Offset, instr.OpCode.Name())
		} else {
			fmt.Printf("IL_%04x: %s %v\n", instr.Offset, instr.OpCode.Name(), instr.Operand)
		}
	}
	return nil
}

func runRun(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: vmnet run <dll> <Type.Method> '<json-array-of-args>'")
	}
	vm := vmnet.New()
	asm, err := vm.LoadFile(args[0])
	if err != nil {
		return err
	}
	typeName, methodName, err := splitTypeMethod(args[1])
	if err != nil {
		return err
	}

	var rawArgs []json.RawMessage
	if err := json.Unmarshal([]byte(args[2]), &rawArgs); err != nil {
		return fmt.Errorf("invalid JSON argument array: %w", err)
	}
	values := make([]vmnet.Value, len(rawArgs))
	for i, raw := range rawArgs {
		v, err := jsonToValue(raw)
		if err != nil {
			return fmt.Errorf("argument %d: %w", i, err)
		}
		values[i] = v
	}

	result, err := asm.Call(typeName, methodName, values...)
	if err != nil {
		return err
	}
	if result == nil {
		fmt.Println("(void)")
		return nil
	}
	fmt.Println(result.Native())
	return nil
}

func runCheck(args []string) error {
	if len(args) > 0 && args[0] == "package" {
		return runCheckPackage(args[1:])
	}

	profile := checker.ProfileRules
	var dllPath, htmlPath string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--profile="):
			profile = checker.Profile(strings.TrimPrefix(a, "--profile="))
		case strings.HasPrefix(a, "--html="):
			htmlPath = strings.TrimPrefix(a, "--html=")
		default:
			dllPath = a
		}
	}
	if dllPath == "" {
		return fmt.Errorf("usage: vmnet check [--profile=minimal|rules|netstandard-lite] [--html=<file>] <dll>")
	}
	if err := validateProfile(profile); err != nil {
		return err
	}

	f, md, err := loadRaw(dllPath)
	if err != nil {
		return err
	}
	report := checker.Analyze(f, md, profile)
	printReport(report)

	name := report.AssemblyName
	if name == "" {
		name = dllPath
	}
	if err := writeHTMLReport(htmlPath, "vmnet compatibility report — "+name, []checker.NamedReport{{Name: name, Report: report}}, nil); err != nil {
		return err
	}

	if report.Status != checker.StatusCompatible {
		os.Exit(1)
	}
	return nil
}

// writeHTMLReport is a no-op when htmlPath is empty — every `--html=`
// call site shares this one helper so `vmnet check`/`check package`/
// `analyze` all produce the exact same report shape and the same
// "wrote ..." confirmation message.
func writeHTMLReport(htmlPath, title string, reports []checker.NamedReport, candidates []checker.Candidate) error {
	if htmlPath == "" {
		return nil
	}
	html, err := checker.RenderHTML(title, reports, candidates, time.Now().Format("2006-01-02 15:04:05 MST"))
	if err != nil {
		return fmt.Errorf("rendering HTML report: %w", err)
	}
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		return fmt.Errorf("writing HTML report: %w", err)
	}
	fmt.Printf("wrote %s\n", htmlPath)
	return nil
}

func validateProfile(p checker.Profile) error {
	switch p {
	case checker.ProfileMinimal, checker.ProfileRules, checker.ProfileNetStandardLite:
		return nil
	default:
		return fmt.Errorf("unknown profile %q (want minimal, rules or netstandard-lite)", p)
	}
}

func runCheckPackage(args []string) error {
	profile := checker.ProfileNetStandardLite
	var spec, htmlPath string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--profile="):
			profile = checker.Profile(strings.TrimPrefix(a, "--profile="))
		case strings.HasPrefix(a, "--html="):
			htmlPath = strings.TrimPrefix(a, "--html=")
		default:
			spec = a
		}
	}
	if spec == "" {
		return fmt.Errorf("usage: vmnet check package [--profile=...] [--html=<file>] <id>@<version>")
	}
	if err := validateProfile(profile); err != nil {
		return err
	}
	id, version, err := splitPackageSpec(spec)
	if err != nil {
		return err
	}

	client := nuget.NewClient()
	cache := nuget.NewCache(vmnet.NuGetCacheDir)
	if version == "" {
		version, err = client.LatestVersion(id)
		if err != nil {
			return err
		}
	}
	data, err := cache.Fetch(client, id, version)
	if err != nil {
		return err
	}
	pkg, err := nuget.OpenPackage(data)
	if err != nil {
		return err
	}

	fmt.Printf("%s %s\n", pkg.Spec.Metadata.ID, pkg.Spec.Metadata.Version)
	asset, ok, reason := pkg.SelectLibAsset(nuget.SelectOptions{})
	if !ok {
		fmt.Println("Status: unsupported")
		fmt.Printf("Reason: %s\n", reason)
		os.Exit(1)
	}
	fmt.Printf("Selected target: %s (%s)\n", asset.TFM, asset.Path)
	if asset.ReferenceOnly {
		fmt.Println("Note: reference-only asset (ref/) — inspected, but cannot be executed")
	}

	f, err := pe.Parse(asset.Data)
	if err != nil {
		return fmt.Errorf("parsing selected assembly: %w", err)
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		return fmt.Errorf("parsing selected assembly metadata: %w", err)
	}

	// Resolve id's full transitive dependency graph the same way
	// vm.LoadPackage does at runtime (Fase 3.29), so a call into a real
	// dependency's own types (e.g. NPOI -> ZString) isn't misreported as
	// unsupported just because the checker only decoded id's own DLL.
	var deps []*metadata.Metadata
	resolver := nuget.NewResolver(client, cache, nuget.SelectOptions{})
	resolved, err := resolver.Resolve([]nuget.Dependency{{ID: id, Version: version}})
	if err != nil {
		return fmt.Errorf("resolving dependency graph: %w", err)
	}
	for key, rp := range resolved {
		if key == strings.ToLower(id) || rp.SelectedAsset == "" {
			continue
		}
		depData, err := cache.Fetch(client, rp.ID, rp.Version)
		if err != nil {
			return fmt.Errorf("fetching dependency %s@%s: %w", rp.ID, rp.Version, err)
		}
		depPkg, err := nuget.OpenPackage(depData)
		if err != nil {
			return fmt.Errorf("opening dependency %s@%s: %w", rp.ID, rp.Version, err)
		}
		depAssetData, ok := depPkg.Entry(rp.SelectedAsset)
		if !ok {
			continue
		}
		depF, err := pe.Parse(depAssetData)
		if err != nil {
			continue
		}
		depMD, err := metadata.Parse(depF.Metadata)
		if err != nil {
			continue
		}
		deps = append(deps, depMD)
	}
	fmt.Printf("Dependencies resolved: %d\n\n", len(deps))

	report := checker.AnalyzeWithDeps(f, md, deps, profile)
	printReport(report)

	pkgName := fmt.Sprintf("%s@%s", pkg.Spec.Metadata.ID, pkg.Spec.Metadata.Version)
	if err := writeHTMLReport(htmlPath, "vmnet compatibility report — "+pkgName, []checker.NamedReport{{Name: pkgName, Report: report}}, nil); err != nil {
		return err
	}

	if report.Status != checker.StatusCompatible {
		os.Exit(1)
	}
	return nil
}

// runAnalyze implements `vmnet analyze <dir>`: a whole-directory
// migration decision tool, not a per-file checker run — see
// internal/migrate's own doc comment for why cross-assembly resolution
// across every DLL found in dir matters here.
func runAnalyze(args []string) error {
	profile := checker.ProfileNetStandardLite
	var dir, htmlPath string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--profile="):
			profile = checker.Profile(strings.TrimPrefix(a, "--profile="))
		case strings.HasPrefix(a, "--html="):
			htmlPath = strings.TrimPrefix(a, "--html=")
		default:
			dir = a
		}
	}
	if dir == "" {
		return fmt.Errorf("usage: vmnet analyze <dir> [--profile=minimal|rules|netstandard-lite] [--html=<file>]")
	}
	if err := validateProfile(profile); err != nil {
		return err
	}

	summary, err := migrate.AnalyzeDirectory(dir, profile)
	if err != nil {
		return err
	}

	fmt.Printf("Assemblies: %d\n", summary.TotalAssemblies)
	fmt.Printf("Methods analyzed: %s\n", formatCount(summary.TotalMethods))
	fmt.Printf("Runnable today: %s\n", formatCount(summary.TotalRunnable))
	for _, cat := range migrate.BlockedByCategory(summary) {
		fmt.Printf("Blocked by %s: %s\n", cat.Label, formatCount(cat.Count))
	}
	if len(summary.Skipped) > 0 {
		fmt.Println()
		fmt.Printf("Skipped %d file(s) that aren't readable .NET assemblies:\n", len(summary.Skipped))
		for _, s := range summary.Skipped {
			fmt.Printf("  %s: %s\n", s.Path, s.Reason)
		}
	}

	if len(summary.Candidates) > 0 {
		fmt.Println()
		fmt.Println("Best migration candidates:")
		for _, c := range summary.Candidates {
			ratio := 100 * float64(c.MethodsAnalyzed-c.MethodsFlagged) / float64(c.MethodsAnalyzed)
			fmt.Printf("- %s (%s) — %.1f%% clean (%d/%d)\n", c.Type, c.Assembly, ratio, c.MethodsAnalyzed-c.MethodsFlagged, c.MethodsAnalyzed)
		}
	}

	if err := writeHTMLReport(htmlPath, "vmnet migration analysis — "+dir, summary.Reports, summary.Candidates); err != nil {
		return err
	}

	if summary.TotalFlagged > 0 {
		os.Exit(1)
	}
	return nil
}

// formatCount adds thousands separators (48,392, not 48392) — the exact
// shape the user's own example output uses, and genuinely more readable
// once a real legacy system's method count reaches five or six digits.
func formatCount(n int) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// runBind implements `vmnet bind`: generating idiomatic Go wrapper code
// over a real assembly's or NuGet package's own public API — see
// internal/bind's own doc comment for exactly what it can and can't map
// precisely.
func runBind(args []string) error {
	if len(args) > 0 && args[0] == "package" {
		return runBindPackage(args[1:])
	}

	var dllPath, outDir, goPackage string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--out="):
			outDir = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "--package="):
			goPackage = strings.TrimPrefix(a, "--package=")
		default:
			dllPath = a
		}
	}
	if dllPath == "" || outDir == "" {
		return fmt.Errorf("usage: vmnet bind <dll> --out=<dir> [--package=<name>]")
	}
	_, md, err := loadRaw(dllPath)
	if err != nil {
		return err
	}
	sourceName := filepath.Base(dllPath)
	if asm, err := md.Assembly(1); err == nil && asm.Name != "" {
		sourceName = asm.Name
	}
	if goPackage == "" {
		goPackage = defaultGoPackageName(sourceName)
	}
	return writeBoundPackage(md, goPackage, sourceName, outDir)
}

func runBindPackage(args []string) error {
	var spec, outDir, goPackage string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--out="):
			outDir = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "--package="):
			goPackage = strings.TrimPrefix(a, "--package=")
		default:
			spec = a
		}
	}
	if spec == "" || outDir == "" {
		return fmt.Errorf("usage: vmnet bind package <id>@<version> --out=<dir> [--package=<name>]")
	}
	id, version, err := splitPackageSpec(spec)
	if err != nil {
		return err
	}

	client := nuget.NewClient()
	cache := nuget.NewCache(vmnet.NuGetCacheDir)
	if version == "" {
		version, err = client.LatestVersion(id)
		if err != nil {
			return err
		}
	}
	data, err := cache.Fetch(client, id, version)
	if err != nil {
		return err
	}
	pkg, err := nuget.OpenPackage(data)
	if err != nil {
		return err
	}
	asset, ok, reason := pkg.SelectLibAsset(nuget.SelectOptions{})
	if !ok {
		return fmt.Errorf("%s@%s has no usable managed assembly: %s", id, version, reason)
	}
	f, err := pe.Parse(asset.Data)
	if err != nil {
		return fmt.Errorf("parsing selected assembly: %w", err)
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		return fmt.Errorf("parsing selected assembly metadata: %w", err)
	}

	sourceName := fmt.Sprintf("%s@%s", pkg.Spec.Metadata.ID, pkg.Spec.Metadata.Version)
	if goPackage == "" {
		goPackage = defaultGoPackageName(pkg.Spec.Metadata.ID)
	}
	return writeBoundPackage(md, goPackage, sourceName, outDir)
}

func writeBoundPackage(md *metadata.Metadata, goPackage, sourceName, outDir string) error {
	model, err := bind.BuildModel(md, goPackage, sourceName)
	if err != nil {
		return fmt.Errorf("building bind model: %w", err)
	}
	src, err := model.Generate()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	outPath := filepath.Join(outDir, goPackage+".go")
	if err := os.WriteFile(outPath, []byte(src), 0o644); err != nil {
		return fmt.Errorf("writing generated Go file: %w", err)
	}
	fmt.Printf("wrote %s: package %s, %d bound type(s) from %s\n", outPath, goPackage, len(model.Types), sourceName)
	if len(model.Types) == 0 {
		fmt.Println("note: no public, top-level class or struct was found to bind — nothing was generated beyond the package clause")
	}
	return nil
}

// defaultGoPackageName derives a valid, lowercase Go package name from a
// real assembly/package name — "Jint" -> "jint", "System.Text.Json" ->
// "systemtextjson" (a real Go package name can't contain dots).
func defaultGoPackageName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "bound"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "pkg" + out
	}
	return out
}

func runAdd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vmnet add <id>[@<version>]")
	}
	id, version, err := splitPackageSpec(args[0])
	if err != nil {
		return err
	}
	vm := vmnet.New()
	if err := vm.NuGet().Add(id, version); err != nil {
		return err
	}
	fmt.Printf("added %s to %s (run `vmnet restore` to resolve it)\n", id, vmnet.NuGetManifestFile)
	return nil
}

func runRestore(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: vmnet restore")
	}
	vm := vmnet.New()
	if err := vm.NuGet().Restore(); err != nil {
		return err
	}
	packages, err := vm.NuGet().Packages()
	if err != nil {
		return err
	}
	for _, p := range packages {
		if p.Unselectable != "" {
			fmt.Printf("%s@%s: unresolved (%s)\n", p.ID, p.Version, p.Unselectable)
			continue
		}
		fmt.Printf("%s@%s -> %s\n", p.ID, p.Version, p.SelectedAsset)
	}
	fmt.Printf("wrote %s\n", vmnet.NuGetLockFile)
	return nil
}

func runPackages(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: vmnet packages")
	}
	vm := vmnet.New()
	packages, err := vm.NuGet().Packages()
	if err != nil {
		return err
	}
	for _, p := range packages {
		status := p.SelectedAsset
		if p.Unselectable != "" {
			status = "unresolved: " + p.Unselectable
		}
		fmt.Printf("%s@%s %s\n", p.ID, p.Version, status)
	}
	return nil
}

func splitPackageSpec(spec string) (id, version string, err error) {
	if idx := strings.LastIndex(spec, "@"); idx > 0 {
		return spec[:idx], spec[idx+1:], nil
	}
	if spec == "" {
		return "", "", fmt.Errorf("empty package spec")
	}
	return spec, "", nil
}

func printReport(r *checker.Report) {
	name := r.AssemblyName
	if name == "" {
		name = "(unnamed assembly)"
	}
	fmt.Println(name)
	fmt.Printf("Status: %s\n", r.Status)
	fmt.Printf("Profile: %s\n", r.Profile)
	fmt.Printf("Methods analyzed: %d\n", r.MethodsAnalyzed)
	fmt.Printf("Methods flagged: %d\n", r.MethodsFlagged)

	if len(r.Findings) == 0 {
		fmt.Println("Findings: none")
		return
	}

	fmt.Println()
	fmt.Println("Findings:")
	for _, finding := range r.Findings {
		loc := finding.Method
		if loc == "" {
			loc = "(assembly)"
		}
		fmt.Printf("- [%s] %s: %s\n", finding.Kind, loc, finding.Detail)
		if finding.Suggestion != "" {
			fmt.Printf("    suggestion: %s\n", finding.Suggestion)
		}
	}
}

func jsonToValue(raw json.RawMessage) (vmnet.Value, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if f == float64(int32(f)) {
			return vmnet.Int32(int32(f)), nil
		}
		return vmnet.Float64(f), nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return vmnet.String(s), nil
	}
	return nil, fmt.Errorf("unsupported JSON argument %s", raw)
}

func splitTypeMethod(s string) (typeName, methodName string, err error) {
	idx := strings.LastIndex(s, ".")
	if idx < 0 {
		return "", "", fmt.Errorf("expected <Type.Method>, got %q", s)
	}
	return s[:idx], s[idx+1:], nil
}

func splitLastDot(s string) (namespace, name string) {
	idx := strings.LastIndex(s, ".")
	if idx < 0 {
		return "", s
	}
	return s[:idx], s[idx+1:]
}

func qualify(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}
