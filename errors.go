package vmnet

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// ManagedException is a thrown-and-unhandled CIL exception, surfaced as a
// wrapped Go error from Call/CallBytes/CallJSON — use errors.As to inspect
// it. Fase 2 only supports unhandled throw; try/catch/finally are handled
// on the C# side, not here (see docs/ROADMAP.md).
type ManagedException = runtime.ManagedException
