// Package vmnet is the public API of vmnet: a pure-Go interpreter for a
// supported subset of ECMA-335 CIL/IL, used to embed C# plugins and selected
// NuGet packages inside Go programs without requiring a .NET runtime.
//
// vmnet is not a full .NET implementation. It executes a supported subset of
// CIL and selected BCL APIs against compiled assemblies. See docs/en/spec.md
// and docs/en/ROADMAP.md for the full technical specification and the phased
// delivery plan.
//
// Load an assembly with LoadFile/LoadBytes/LoadPackage, then either call a
// static method directly (Call/CallBytes/CallJSON) or construct an instance
// and drive its own API (New/Instance.Call) — see the package-level examples
// and examples/ for runnable programs, including a real third-party
// JavaScript engine (Jint) executing end to end.
//
// The implementation is under active development (see docs/en/ROADMAP.md,
// Fase 3.28 onward).
package vmnet
