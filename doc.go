// Package vmnet is the public API of vmnet: a pure-Go interpreter for a
// supported subset of ECMA-335 CIL/IL, used to embed C# plugins and selected
// NuGet packages inside Go programs without requiring a .NET runtime.
//
// vmnet is not a full .NET implementation. It executes a supported subset of
// CIL and selected BCL APIs against compiled assemblies. See docs/spec.md
// and docs/ROADMAP.md for the full technical specification and the phased
// delivery plan.
//
// The implementation is under active development (see docs/ROADMAP.md,
// Phase 1 onward); this package currently has no public surface yet.
package vmnet
