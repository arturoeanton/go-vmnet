// Package pe reads Windows PE/COFF binaries and locates the CLI (CLR)
// header and metadata root within them, per ECMA-335 partition II. It
// resolves RVAs to file offsets so higher layers (internal/metadata) can
// read metadata streams and method bodies. See docs/ROADMAP.md, Fase 1,
// module "/pe".
package pe
