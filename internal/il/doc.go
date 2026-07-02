// Package il decodes raw CIL method-body bytes into structured
// instructions (opcode, operand, offset). It recognizes the full CIL
// opcode set so decoding never fails on unknown-but-valid instructions,
// even when a given opcode is not yet supported by the interpreter. See
// docs/ROADMAP.md, Fase 1, module "/il".
package il
