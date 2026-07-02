# rules

Business-rules plugin example: a `Rules.Engine.Eval` C# method called from a
Go host via `Assembly.CallJSON`, including sandbox limits and a managed
exception. This is the Fase 2 demo scenario — see `docs/ROADMAP.md`.

Not runnable yet: depends on objects, `callvirt`, collections, exceptions
and the JSON bridge, which land in Fase 2.
