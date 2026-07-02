// Package interpreter executes vmnet IR (internal/ir) against a call frame
// and value stack: arithmetic, branches, calls and exception handling, with
// configurable limits (max stack depth, call depth, instruction count) for
// sandboxed execution. See docs/ROADMAP.md, Fase 1 y Fase 2, module
// "/interpreter".
package interpreter
