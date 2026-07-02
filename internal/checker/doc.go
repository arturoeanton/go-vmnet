// Package checker analyzes an assembly or NuGet package against a
// compatibility profile (minimal, rules, netstandard-lite) and reports
// which opcodes, BCL calls, generics, reflection or async usages are
// unsupported, with actionable reasons instead of a raw crash. See
// docs/ROADMAP.md, Fase 3, module "/checker".
package checker
