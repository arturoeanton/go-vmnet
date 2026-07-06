// Package migrate implements `vmnet analyze`: scanning a whole directory
// of real, already-compiled .NET assemblies (a legacy application's own
// bin/ folder, not a single package) and answering "which parts of this
// system could vmnet already run today" — a decision tool, not just a
// per-file checker run.
//
// The real value over running `vmnet check` once per DLL by hand is
// cross-assembly resolution: a legacy bin/ folder ships every one of its
// own private dependencies side by side, so a type defined in
// Billing.Core.dll and used from Billing.Rules.dll should resolve
// exactly like it would at real runtime — not get flagged as an
// unsupported external call just because only one DLL was inspected at
// a time (the same principle AnalyzeWithDeps already applies to a
// single NuGet package's own transitive dependency graph, generalized
// here to "every other DLL found in this same directory").
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/checker"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

// SkippedFile records a real .dll this scan couldn't analyze at all
// (not a checker Finding — the file never got far enough to produce
// one) — a native-only DLL, a corrupted file, or anything else that
// isn't a real, parseable CLI assembly. Reported plainly rather than
// silently dropped, matching the checker's own "never bails, always
// says why" posture (spec §23: "the checker is mandatory, without it
// the user suffers" applies just as much to a whole-directory scan).
type SkippedFile struct {
	Path   string
	Reason string
}

// Summary is the result of scanning one directory.
type Summary struct {
	Dir             string
	Reports         []checker.NamedReport
	Candidates      []checker.Candidate
	Skipped         []SkippedFile
	TotalAssemblies int
	TotalMethods    int
	TotalRunnable   int
	TotalFlagged    int
}

// minCandidateMethods excludes a type from the "best migration
// candidates" ranking unless it has at least this many analyzed
// methods — without a floor, a real one-method, 100%-clean type (there
// are always many of these: trivial property getters, empty
// constructors, ...) would flood the top of the list ahead of genuinely
// substantial, real business-logic classes that are merely very good
// rather than perfect.
const minCandidateMethods = 3

// maxCandidates bounds the ranked list to a genuinely reviewable size —
// a real legacy system can have thousands of types; nobody reads a
// "best candidates" list past the first couple dozen anyway, and a
// silent truncation is noted in Summary.TotalAssemblies/TotalMethods
// still reflecting the FULL scan, not just the ranked slice.
const maxCandidates = 25

// AnalyzeDirectory walks dir for real .NET assemblies (any file ending
// in .dll, recursively) and runs the checker against each — with every
// OTHER assembly found in the same scan available as a same-directory
// dependency, so a real cross-assembly call inside this one legacy
// system resolves the same way it would if vmnet actually loaded the
// whole thing (AnalyzeWithDeps's own existing multi-assembly resolution,
// generalized from "a package's own transitive NuGet graph" to "every
// sibling DLL in this folder").
func AnalyzeDirectory(dir string, profile checker.Profile) (*Summary, error) {
	paths, err := findDLLs(dir)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .dll files found under %s", dir)
	}

	type parsed struct {
		path string
		name string
		f    *pe.File
		md   *metadata.Metadata
	}
	var assemblies []parsed
	summary := &Summary{Dir: dir}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			summary.Skipped = append(summary.Skipped, SkippedFile{Path: path, Reason: err.Error()})
			continue
		}
		f, err := pe.Parse(data)
		if err != nil {
			summary.Skipped = append(summary.Skipped, SkippedFile{Path: path, Reason: "not a .NET assembly: " + err.Error()})
			continue
		}
		md, err := metadata.Parse(f.Metadata)
		if err != nil {
			summary.Skipped = append(summary.Skipped, SkippedFile{Path: path, Reason: "unreadable metadata: " + err.Error()})
			continue
		}
		name := filepath.Base(path)
		if asmRow, err := md.Assembly(1); err == nil && asmRow.Name != "" {
			name = asmRow.Name
		}
		assemblies = append(assemblies, parsed{path: path, name: name, f: f, md: md})
	}

	if len(assemblies) == 0 {
		return nil, fmt.Errorf("found %d .dll file(s) under %s, but none is a readable .NET assembly (see Skipped)", len(paths), dir)
	}

	for i, a := range assemblies {
		var deps []*metadata.Metadata
		for j, other := range assemblies {
			if i != j {
				deps = append(deps, other.md)
			}
		}
		report := checker.AnalyzeWithDeps(a.f, a.md, deps, profile)
		summary.Reports = append(summary.Reports, checker.NamedReport{Name: a.name, Report: report})
		summary.TotalAssemblies++
		summary.TotalMethods += report.MethodsAnalyzed
		summary.TotalFlagged += report.MethodsFlagged

		for typeName, tr := range report.PerType {
			if tr.MethodsAnalyzed < minCandidateMethods {
				continue
			}
			summary.Candidates = append(summary.Candidates, checker.Candidate{
				Type:            typeName,
				Assembly:        a.name,
				MethodsAnalyzed: tr.MethodsAnalyzed,
				MethodsFlagged:  tr.MethodsFlagged,
			})
		}
	}
	summary.TotalRunnable = summary.TotalMethods - summary.TotalFlagged

	sort.Slice(summary.Candidates, func(i, j int) bool {
		ci, cj := summary.Candidates[i], summary.Candidates[j]
		ri := float64(ci.MethodsAnalyzed-ci.MethodsFlagged) / float64(ci.MethodsAnalyzed)
		rj := float64(cj.MethodsAnalyzed-cj.MethodsFlagged) / float64(cj.MethodsAnalyzed)
		if ri != rj {
			return ri > rj
		}
		// Same ratio: prefer the type with more real methods analyzed —
		// a 10/10-clean type is a more substantial, more convincing
		// migration candidate than a 3/3-clean one, even though both
		// round to 100%.
		return ci.MethodsAnalyzed > cj.MethodsAnalyzed
	})
	if len(summary.Candidates) > maxCandidates {
		summary.Candidates = summary.Candidates[:maxCandidates]
	}
	sort.Slice(summary.Reports, func(i, j int) bool { return summary.Reports[i].Name < summary.Reports[j].Name })

	return summary, nil
}

// CategoryCount is one line of the "blocked by category" breakdown —
// either a whole FindingKind (Reflection, Async, P/Invoke, ...) or, for
// the much larger KindUnsupportedMethod bucket, a real BCL namespace
// prefix (`System.Data`, `System.Reflection.Emit`, ...) extracted from
// the actual unresolved call targets — the shape a real migration
// decision needs ("what part of the BCL is actually missing"), not a
// generic "unsupported method" count that says nothing on its own.
type CategoryCount struct {
	Label string
	Count int
}

// namespaceBucketDepth is how many leading dot-separated segments of a
// call target's declaring type name become its own bucket label — 2
// matches real BCL namespace granularity (`System.Data`, `System.
// Reflection`) without collapsing everything down to a single top-level
// `System` bucket, which would tell a reader nothing.
const namespaceBucketDepth = 2

// BlockedByCategory aggregates every finding across summary's own
// Reports into human-meaningful buckets: each non-method FindingKind
// (Reflection, Async/Task, P/Invoke, ...) counts as its own bucket by
// its own label; KindUnsupportedMethod findings are re-bucketed by the
// real BCL namespace of their own unresolved call target instead,
// since "unsupported BCL method" alone is true of hundreds of
// unrelated real gaps and tells a reader nothing about WHICH part of
// the BCL their own migration candidate actually needs. Sorted by
// count, highest first.
func BlockedByCategory(summary *Summary) []CategoryCount {
	counts := map[string]int{}
	for _, nr := range summary.Reports {
		for _, f := range nr.Report.Findings {
			if f.Kind == checker.KindUnsupportedMethod {
				counts[namespaceBucket(f.Detail)]++
				continue
			}
			counts[checker.KindLabel(f.Kind)]++
		}
	}
	out := make([]CategoryCount, 0, len(counts))
	for label, c := range counts {
		out = append(out, CategoryCount{Label: label, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// namespaceBucket extracts a real BCL namespace prefix from a call
// target like "System.Data.SqlClient.SqlConnection::Open" ->
// "System.Data" — the declaring type's own leading namespaceBucketDepth
// segments, dropping the "::Method" suffix and the type's own bare name
// (the last segment before "::").
func namespaceBucket(target string) string {
	typeName, _, ok := strings.Cut(target, "::")
	if !ok {
		typeName = target
	}
	segments := strings.Split(typeName, ".")
	if len(segments) <= 1 {
		return typeName
	}
	// Drop the bare type name itself (the last segment) before taking
	// the namespace's own leading depth — "System.Data.SqlClient.
	// SqlConnection" -> namespace segments are
	// [System, Data, SqlClient], not [System, Data, SqlClient,
	// SqlConnection].
	nsSegments := segments[:len(segments)-1]
	if len(nsSegments) == 0 {
		return typeName
	}
	depth := namespaceBucketDepth
	if depth > len(nsSegments) {
		depth = len(nsSegments)
	}
	return strings.Join(nsSegments[:depth], ".")
}

func findDLLs(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".dll") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", dir, err)
	}
	sort.Strings(out)
	return out, nil
}
