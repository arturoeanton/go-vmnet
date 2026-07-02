# hello

Smallest possible vmnet example: load a one-method C# DLL and call it from
Go. Ships with the Fase 1 demo (`SimpleMath.Add`, `Strings.Hello`) — see
`docs/ROADMAP.md`.

Not runnable yet: depends on `vmnet.New()` / `Assembly.Call`, which land in
Fase 1.
