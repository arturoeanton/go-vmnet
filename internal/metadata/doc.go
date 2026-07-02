// Package metadata parses ECMA-335 CLI metadata: the #~, #Strings, #US,
// #Blob and #GUID streams, the metadata tables (TypeDef, MethodDef,
// MemberRef, ...), tokens, coded indexes and method/field/local signatures.
// It is the layer that turns raw metadata bytes from internal/pe into typed
// records the rest of the runtime can resolve. See docs/ROADMAP.md, Fase 1,
// module "/metadata".
package metadata
