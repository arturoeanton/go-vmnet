// Package ir defines vmnet's intermediate representation and the builder
// that lowers decoded IL (internal/il) into it. The IR exists so the
// interpreter, the compatibility checker and any future codegen backend
// work against one simplified, validated instruction set instead of raw
// CIL. See docs/ROADMAP.md, Fase 1, module "/ir".
package ir
