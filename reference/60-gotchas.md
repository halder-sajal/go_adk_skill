# 60 GOTCHAS (non-obvious; many verified)
1. Import path is google.golang.org/adk/v2 (v1 = google.golang.org/adk, no graph engine). Don't mix.
2. session.NewEvent REQUIRES ctx first arg in v2.
3. NewFunctionNode returns no error; *WithSchema/FromState/AgentNode/ToolNode/ParallelWorker/WorkflowNode return (node, error); NewDynamicNode returns Node (no error).
4. Plain FunctionNode output → Event.Output only (no Content text) → console/UI may show nothing visible. For visible text: return string from EMITTING fn (sets Content) or emit ev.Content yourself. Returning *genai.Content sets Content but NO Output (successor gets nil).
5. Emitting fn returning (nil, nil) suppresses terminal event — use when you already emitted the event with Output/Routes; otherwise successor input is nil.
6. Only ONE output event and ONE routing event per activation (ErrMultipleOutputs / ErrMultipleRoutingEvents). Put Routes+Output on the same event.
7. ≥2 unconditional incoming edges to non-JoinNode → build error; use JoinNode. No conditional edges into JoinNode.
8. Unconditional cycle → build error; loops need a routed edge.
9. All routed edges unmatched & no Default → silent dead-end (no error, no output).
10. Unrouted edges + Default: Default still fires (unconditional edges don't count as match).
11. ≤1 terminal node may produce output per run (ErrMultipleTerminalOutputs) — conditional branches OK since only one fires.
12. No type coercion string→int between typed function nodes (runtime json error even for "42"). Build-time schema check only if both ends declare schemas explicitly.
13. LlmAgent at graph node defaults to single_turn (sees only node input, not chat history). chat mode only right after Start; task mode never as static node.
14. Add wrapped LlmAgents to workflowagent.Config.SubAgents, else "Event from an unknown agent" logs.
15. Event.Author = workflow agent name; node identity = ev.NodeInfo.Path ("wf@1/node@1").
16. InterruptID must be unique per run (uuid or +InvocationID). Fixed literal reused in same session → Dev UI won't re-prompt.
17. HITL default is HANDOFF (reply → successors). For re-run with reply use RerunOnResume=&true + ctx.ResumedInput / ResumeOrRequestInput. Dynamic nodes default re-entry.
18. Dynamic node body re-runs from top on resume; completed RunNode children replayed from cache by path → keep body deterministic, use stable WithRunID (non-numeric, no '/' '@').
19. RunNode ctx must be the dynamic body ctx (else ErrInvalidRunNodeContext). WithMaxConcurrency doesn't limit RunNode children.
20. Parallel HITL in concurrent RunNode children unsupported.
21. RetryConfig literal: zero fields are zero (no defaults) — start from workflow.DefaultRetryConfig(). Retries only on errors, not hangs → also set NodeConfig.Timeout.
22. ParallelWorker input must be a slice; wrapped node must not have RetryConfig.
23. Persisted values (Input/Output/Payload) must be JSON-encodable; run state is rebuilt from session events. Workflow name "" disables resume; name must be unique per session.
24. Resume FunctionResponse Response map: single key "payload"|"response"|"result" gets unwrapped (string JSON-parsed if valid JSON!) — send "42" and you get float64 42.
25. zsh: `for p in $VAR` doesn't word-split; use `while read`. zsh `=word` at arg start expands (quote echo strings like "=====").
26. go doc on module requires deps downloaded (first `go list ./...` in module dir takes >2 min).
27. Fan-out parallel node order nondeterministic.
28. Default retry skips input-validation errors (deterministic per activation); set RetryConfig.ShouldRetry to override.
