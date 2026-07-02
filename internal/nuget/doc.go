// Package nuget reads .nupkg/.nuspec packages, selects the best-matching
// target framework moniker (favoring netstandard2.0), resolves transitive
// dependencies and writes vmnet's own lockfile. See docs/ROADMAP.md, Fase 3,
// module "/nuget".
package nuget
