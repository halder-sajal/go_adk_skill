# 10 GRAPH WORKFLOW ENGINE — pkg workflow (+ agent/workflowagent)
src: src/workflow/*.go (6.2k LOC). API: api/workflow.txt. Tests: tests-workflow/workflow__*.go
Legend: [V]=verified by running verify/main.go on v2.4.0; [S]=read from source.

## 1. Model
- Graph = []workflow.Edge{From Node, To Node, Route Route}. Nodes are identified by Name() (must be unique; ErrDuplicateNodeName).
- Entry = sentinel `workflow.Start` (name "START"). Must appear as From; nothing may point to Start.
- Build graph: `workflow.New(name, edges, opts...) (*Workflow, error)` (validates) OR `workflowagent.New(workflowagent.Config{Name, Description, Edges, SubAgents, Before/AfterAgentCallbacks})` → agent.Agent runnable by runner/launcher (normal usage).
- Workflow input = concatenated text parts of user message (string) → given to Start's successors. nil if no UserContent. [S workflow.go userInput]
- Data flow: each node activation emits ≤1 event with Event.Output; that value is the input of successors. [V]
- Execution: each scheduled node runs in own goroutine; single consumer loop yields events in arrival order, applies state, schedules successors on completion. Parallel branches really run concurrently (fan-out order nondeterministic [V: b before a]).
- Break from range loop → engine cancels all in-flight nodes.
- Workflow Options: WithMaxConcurrency(n) (graph-scheduled nodes only; not RunNode children), WithStateSchema(*jsonschema.Resolved), WithRootWrapper() (internal-ish).
- Name="" → no persistence/resume. Name must be unique per session across workflows.

## 2. Node types (all implement Node: Name, Description, Config, InputSchema, OutputSchema, ValidateInput, ValidateOutput, Run(agent.Context, any) iter.Seq2[*session.Event,error])
### 2.1 FunctionNode
- `NewFunctionNode[IN,OUT](name, func(agent.Context, IN)(OUT,error), NodeConfig) *FunctionNode` (no error return!)
- `NewFunctionNodeWithSchema[IN,OUT](name, fn, inSchema, outSchema *jsonschema.Schema, cfg) (*FunctionNode, error)`
- `NewEmittingFunctionNode[IN,OUT](name, func(ctx agent.Context, in IN, emit func(*session.Event) error)(OUT,error), cfg) *FunctionNode` — can emit intermediate events (routes, state deltas, HITL, progress).
- `NewEmittingFunctionNodeWithSchema[...]`
- `NewFunctionNodeFromState[Params,OUT](name, func(agent.InvocationContext, Params)(OUT,error), cfg) (*FunctionNode, error)` — Params struct filled from session state; field name = key, override tag `state:"key"`; tag `state:"node_input"` or field named NodeInput = predecessor output. Missing key → error. [V]
- Input coercion: if input not assertable to IN, JSON-converts (map→struct etc.) via schema. nil input → zero IN.
- Plain fn output handling [S]: returns *session.Event → yielded as-is (use to set Routes w/o emitting; InvocationID auto-filled) [V]; returns *genai.Content/genai.Content → Event.Content (NO Output!); else → Event.Output=value (no Content text) [V: text=""].
- Emitting fn [S]: return (nil, nil) → NO terminal event (use when you already emitted event with Output/Routes); return (x, nil) → terminal event Output=x, and if x is string also Content text; return (_, ErrNodeInterrupted) → pause (swallowed); other err → fail.
- Schemas inferred from IN/OUT via jsonschema.For[T].
### 2.2 JoinNode (fan-in barrier)
- `NewJoinNode(name)`, `NewJoinNodeWithSchema(name, *jsonschema.Resolved)` (schema applied per predecessor output).
- Fires ONCE after ALL declared predecessors completed; input=output= map[string]any{predName: predOutput}. [V]
- Conditional (routed) edges INTO a JoinNode = config error (route-skipped predecessor never fires → barrier never releases).
- Only way to converge ≥2 unconditional edges (else ErrUnsupportedFanIn). [V]
- Join branch = common dot-prefix of predecessor branches.
### 2.3 AgentNode (wrap agent.Agent, usually LlmAgent)
- `NewAgentNode(a, cfg) (*AgentNode, error)`, `NewAgentNodeTyped[In,Out](a,cfg)`, `NewAgentNodeWithSchemas(a, in, out *jsonschema.Schema, cfg)`. Node name = agent name. Sets cfg.EmitsOwnSpan=true.
- Input→user content: nil→none; string→text; *genai.Content→as user; else JSON-marshal to text.
- LlmAgent mode at a graph node: unset ⇒ ModeSingleTurn (history scoped to current turn unless IncludeContents=IncludeContentsDefault explicitly). ModeChat allowed ONLY directly after Start (ignores node input, uses session history) else New() errors. ModeTask FORBIDDEN as static graph node → use RunNode from dynamic/function node or chat coordinator.
- Output: final model text (non-thought parts concatenated) auto-synthesized into Event.Output (NodeInfo.MessageAsOutput=true), unless task mode. With llmagent OutputSchema → parsed structured value. OutputKey also writes to session state.
- Pass agents also in workflowagent.Config.SubAgents to avoid "Event from an unknown agent" log noise.
- Composite agents: only events authored by node's own agent name (or empty author) get output synthesized.
### 2.4 ToolNode
- `NewToolNode(t tool.Tool, cfg)`, `NewNamedToolNode(name, t, cfg)`, `NewToolNodeTyped[In,Out]`, `NewToolNodeWithSchemas`. Node name = tool name (or explicit).
- Input string that parses as JSON object → map. Output: Event.Output=tool result; string also → Content. ValidateOutput unwraps {"result": X} convention. Tool's StateDelta/ArtifactDelta carried on event.
### 2.5 ParallelWorker (map over list)
- `NewParallelWorker(name, wrapped Node, maxConcurrency int (<=0 unlimited), cfg) (*ParallelWorker, error)`.
- Input MUST be a slice (reflect) else error. Runs wrapped per item concurrently; output = []any in input order [V: [2 3 1]]. Intermediate events of wrapped suppressed. RetryConfig only on the ParallelWorker (applied per item independently); wrapped node must not have RetryConfig. Non-retryable error → fail fast, cancel others.
- Also NodeConfig.ParallelWorker bool flag exists (engine-side per-item run).
### 2.6 WorkflowNode (nested graph)
- `NewWorkflowNode(name, edges) (*WorkflowNode, error)`; output = the single terminal node output of sub-graph (>1 → error). Sub-workflow name ≠ parent name (ErrSubWorkflowNameCollision).
### 2.7 DynamicNode (code-driven orchestration) — see §7
- `NewDynamicNode[IN,OUT](name, DynamicFn[IN,OUT], cfg) Node`; DynamicFn = func(ctx agent.Context, in IN, emit func(*session.Event) error)(OUT,error). `NewDynamicNodeWithSchema(...) (Node, error)`.
### 2.8 Custom node
```go
type MyNode struct{ workflow.BaseNode }
func NewMy() *MyNode { return &MyNode{BaseNode: workflow.NewBaseNode("my", "desc", workflow.NodeConfig{})} }
func (n *MyNode) Run(ctx agent.Context, in any) iter.Seq2[*session.Event, error] {
  return func(yield func(*session.Event, error) bool) {
    ev := session.NewEvent(ctx, ctx.InvocationID()); ev.Output = in; yield(ev, nil)
  }
}
```
NewBaseNodeWithSchemas(name, desc, cfg, in, out *jsonschema.Resolved) for schemas.

## 3. Edge building
- literal: `[]workflow.Edge{{From: a, To: b}, {From: r, To: x, Route: workflow.StringRoute("x")}}`
- `workflow.Chain(n1, n2, n3...) []Edge` (sequential)
- `workflow.Concat(items ...any) []Edge` (accepts Edge and []Edge)
- `workflow.NewEdgeBuilder().Add(f,t).AddRoute(f,t,route).AddFanOut(f, t...).AddFanIn(t, f...).AddRoutes(f, map[string]Node).Build()`
- Duplicate (From,To) rejected regardless of route → use MultiRoute for alternatives to same target.

## 4. Routing
- A node selects routes by emitting an event with `ev.Routes = []string{...}` (≤1 routing event per activation else ErrMultipleRoutingEvents). Usually same event carries Output.
- Route types: StringRoute("x"); IntRoute(3) & BoolRoute(true) compare vs fmt.Sprint string; MultiRoute[T]{...} any-of; workflow.Default (fires iff no concrete route matched; ≤1 per node, ErrMultipleDefaultRoutes).
- findSuccessors rules [S scheduler.go:986]:
  - Route==nil edges ALWAYS fire (and do NOT count as match → Default still fires alongside).
  - concrete route fires iff Matches(routingEvent).
  - duplicate targets deduped.
  - all edges routed, none matched, no Default → silent dead-end (NOT error).
- Successor input = node's output (routes don't change payload).
- LLM routing pattern: LlmAgent node (classifier, one word) → emitting FunctionNode converts text → ev.Routes. (src/examples/workflow/routing/llm)
- Custom Route: implement `Matches(*session.Event) bool`.

## 5. Loops / cycles
- Cycles allowed only if they contain a conditional edge (else ErrUnconditionalCycle at New) [V]. Self-loop OK: AddRoute(n, n, StringRoute("again")) [V: inc ran 3×].
- Completed node can be re-triggered (NodeCompleted → re-run). Each loop iteration event path stays "<name>@1".
- Alternative: DynamicNode with Go for-loop + RunNode (use WithRunID for distinct ids).

## 6. Fan-out / branches
- Multiple unconditional edges from one node → parallel activations. Each gets sub-branch "<parentBranch>.<name>@1" (Event.Branch) so LLM agents don't see siblings' history.
- Fan-in requires JoinNode.
- Workflow final output: at most one terminal node (no outgoing edges) may produce output per run → else ErrMultipleTerminalOutputs at finalize.

## 7. Dynamic nodes + RunNode
- `workflow.RunNode[OUT](ctx, child Node, input any, opts...) (OUT, error)` — ctx MUST be the DynamicFn ctx (has SubScheduler) else ErrInvalidRunNodeContext. Child need NOT be in edges.
- Child event path: "<parentPath>/<child>@<runID>"; runID auto-counter "1","2".. or WithRunID("item-1") (must contain non-digit, no '/' '@'; ErrInvalidRunID) [V].
- Options: WithRunID, WithUseSubBranch() (isolate history per child), WithOverrideBranch(b), WithIsolationScope(s)/WithIsolationScopeFromNodePath() (restrict LLM history by Event.IsolationScope), WithUseAsOutput() (child's output becomes parent's terminal output; parent return discarded; ≤1 per activation → ErrOutputAlreadyDelegated), WithRaiseOnWait() (child ending w/ unresolved long-running call ⇒ interruption).
- Errors: errors.Is(err, ErrNodeInterrupted) (child paused for HITL — return it up to pause parent), ErrNodeFailed (errors.As *NodeRunError{ChildName,ChildPath,RunID,Cause}), ErrNodeWaitingForOutput (wraps ErrNodeInterrupted), ctx.Err().
- RerunOnResume defaults &true for dynamic nodes: on resume orchestrator re-runs from top; completed children served from cache (keyed by path) — so make body deterministic & use stable WithRunID. HITL/interrupted children NOT cached.
- Parallel HITL across concurrent children unsupported (ErrParallelHITLUnsupported).
- Concurrency: RunNode blocks; run children in goroutines (errgroup) for parallelism (MaxConcurrency not applied).
- emit(ev) inside body: state deltas (ev.Actions.StateDelta[k]=v — persisted, visible to later nodes via session state [V]), progress Content, HITL requests.
- Task-mode LlmAgents may be run via RunNode.

## 8. Human-in-the-loop (HITL)
- Request: emit `workflow.NewRequestInputEvent(ctx, session.RequestInput{InterruptID, Message, ResponseSchema *jsonschema.Schema, Payload any})` then return (zero, workflow.ErrNodeInterrupted) (emitting/dynamic node). Custom node: yield it and return. Event carries FunctionCall name "adk_request_input" (WorkflowInputFunctionCallName), ID=InterruptID, LongRunningToolIDs → IsFinalResponse()=true → run ends; node → NodeWaiting.
- InterruptID: unique per run (uuid or "prefix-"+ctx.InvocationID()); empty → auto UUID. Reusing fixed literal across runs in same session breaks Dev UI re-prompt.
- Two resume modes (NodeConfig.RerunOnResume *bool):
  - nil/&false = HANDOFF (default for function nodes): reply becomes the asker's OUTPUT → routed to successors; asker doesn't re-run. [V: greet got "Alice"]
  - &true = RE-ENTRY: node re-run from scratch; read reply via `ctx.ResumedInput(interruptID) (any, bool)` or helper `workflow.ResumeOrRequestInput(ctx, emit, req) (reply any, err)` (returns ErrNodeInterrupted first pass). Multiple sequential asks accumulate in NodeState.ResumedInputs.
- Resume wire protocol (next user turn via runner): user Content part FunctionResponse{ID: interruptID, Name: "adk_request_input", Response: map[string]any{"payload": v}} (also accepted single keys "result"/"response"; string values JSON-parsed if possible). Runner reuses paused invocation ID. [V]
- ResponseSchema mismatch → ErrInvalidResumeResponse, node stays waiting. No match → ErrNothingToResume.
- RunState reconstructed from session event history (event-sourced: Workflow.ReconstructRunState(sess, invocationID)), not a blob. NodeState/Payload must be JSON-encodable (use artifacts for binary).
- Tools can also do HITL confirmation: ctx.RequestConfirmation(hint, payload) / functiontool Config.RequireConfirmation.
- Programmatic: Workflow.Resume(ctx, *RunState, map[interruptID]any) (handoff semantics).

## 9. NodeConfig (all optional)
ParallelWorker bool | RerunOnResume *bool | WaitForOutput *bool (RunNode path: child w/o output parks parent) | RetryConfig *RetryConfig (nil=no retry) | Timeout time.Duration (per activation) | EmitsOwnSpan bool
- RetryConfig{MaxAttempts(incl. first; 0/1=no retry), InitialDelay, MaxDelay, BackoffFactor, Jitter [0,1], ShouldRetry func(error) bool}. Use `workflow.DefaultRetryConfig()` (5 attempts, 1s, 60s cap, 2x, full jitter, retry all) then override. Struct literal zero fields = zero, not defaults!
- Retry only on returned errors (not stalls) → combine with Timeout.
- Node statuses: NodeInactive, NodePending, NodeRunning, NodeCompleted, NodeWaiting, NodeFailed, NodeCancelled.

## 10. Validation at New()/workflowagent.New() (errors)
ErrDuplicateNodeName, ErrNoStartNode, ErrNodePointsToStart, ErrDuplicateEdge, ErrMultipleDefaultRoutes, ErrNodesNotReachable (all nodes must be reachable from Start), ErrUnconditionalCycle, ErrUnsupportedFanIn (≥2 unconditional in-edges to non-Join), ErrSubWorkflowNameCollision, task-mode agent node error, chat-mode agent not after Start error, static schema mismatch on edge (only when BOTH ends have OutputSchema/InputSchema non-nil; compared canonically). NOTE [V]: plain NewFunctionNode string→int chain is NOT rejected at build; it fails at RUNTIME: "new function node: invalid input type, expected *int: json: cannot unmarshal string..." (no string→number coercion, even "42"). Use explicit *WithSchema constructors if you want build-time checks. State-schema consistency (WithStateSchema vs NewFunctionNodeFromState keys).
Runtime errors: ErrMultipleOutputs, ErrMultipleRoutingEvents, ErrMultipleTerminalOutputs, ErrInputValidation, node errors surfaced via iterator err.

## 11. Events emitted by graph (what consumer sees) [V]
- Author = workflow agent name (not node name). Node identity in ev.NodeInfo.Path: "<wf>@1/<node>@1"; dynamic child "<wf>@1/<dyn>@1/<child>@<runID>". (After HITL resume, observed path lacked wf prefix: "greet@1".)
- ev.Output (node output), ev.Routes, ev.RequestedInput, ev.NodeInfo.{Path, MessageAsOutput, OutputFor}, ev.Branch, ev.IsolationScope, ev.Actions.StateDelta.
- Content.Role defaulted (model; user for FunctionResponse parts).

## 12. Graph visualization
server/adkrest exposes agent graph endpoint (routers/agentgraph.go, services/agentgraphgenerator.go uses gographviz) — Dev UI renders it. `go run . web api webui` (see 30 §4).

## 13. Composite agents alternative (pre-graph style, still available)
sequentialagent/parallelagent/loopagent (Config{AgentConfig agent.Config{Name, SubAgents...}, MaxIterations for loop}). Loop exit via exitlooptool or Actions.Escalate. Prefer workflow graph for data-passing, routing, HITL, retries.

## 14. Remaining public symbols (completeness pass)
- type EmittingFunctionFn[IN,OUT] = func(ctx agent.Context, in IN, emit func(*session.Event) error)(OUT, error)   (alias; body type for NewEmittingFunctionNode)
- type DynamicFn[IN,OUT] = same shape; body for NewDynamicNode (ctx carries SubScheduler).
- type RunNodeOption func(*runNodeOptions) — WithRunID/WithUseSubBranch/WithOverrideBranch/WithIsolationScope/WithIsolationScopeFromNodePath/WithUseAsOutput/WithRaiseOnWait.
- type NodeStatus uint8 — NodeInactive(0) NodePending NodeRunning NodeCompleted NodeWaiting NodeFailed NodeCancelled. NodeWaiting covers HITL pause AND WaitForOutput park. Completed nodes can be re-triggered (loops).
- type NodeState{Status, Input, Output, TriggeredBy, Branch, Interrupts []string, Attempt, ResumedInputs map[string]any} (JSON-persisted).
- type RunState{Nodes map[string]*NodeState}; NewRunState(); (*RunState).EnsureNode(name) *NodeState. Used with Workflow.Resume / ReconstructRunState.
- type StateParamsAware interface{ StateFieldNames() []string } — implemented by FunctionNode (from NewFunctionNodeFromState); WithStateSchema validation checks these keys exist in the state schema.
- func CalculateDelay(cfg *RetryConfig, failedAttempts int) time.Duration — delay=InitialDelay*BackoffFactor^(n-1) (factor<=0 → 1), capped at MaxDelay, ±Jitter*delay.
- func ShouldRetry(cfg *RetryConfig, err error, failedAttempts int) bool — false if cfg nil, MaxAttempts<=1, or failedAttempts>=MaxAttempts; custom cfg.ShouldRetry wins; DEFAULT predicate does NOT retry input-validation errors (ErrInputValidation — deterministic), retries everything else.
- (*NodeRunError).Error() "workflow: dynamic child <path>: <cause>"; Unwrap() → Cause (so errors.Is(err, ErrNodeFailed) works).
- (*Workflow).RunNode(ctx, input) — run a Workflow directly with explicit input (what WorkflowNode uses); (*Workflow).Run(ic) — input from UserContent.
- (*FunctionNode).StateFieldNames(); (*JoinNode).ValidateInput per-predecessor; (*ToolNode).ValidateOutput {"result":X} unwrap.

## 15. Graph visualization / debug endpoints (server/adkrest; enable with `web api` [+ -include_debug_api])
- GET /dev/apps/{app_name}/build_graph  and  /dev/apps/{app_name}/build_graph_image  (agent graph; dot via gographviz; dark_mode query → theme)
- GET /dev/apps/{app_name}/users/{user_id}/sessions/{session_id}/events/{event_id}/graph  and  /apps/{app_name}/users/{u}/sessions/{s}/events/{e}/graph (graph highlighted for an event; debug API)
- builder: GET/POST /dev/apps/{app_name}/builder (agent builder config).
- src: server/adkrest/internal/routers/{agentgraph,debug}.go, services/agentgraphgenerator.go.

## 16. Task mode internals (internal/workflowinternal — not public, behavior matters)
- Task-mode LlmAgent gets auto tool "finish_task". Args schema = agent OutputSchema (object used directly; non-object wrapped under "result" key); no OutputSchema → {"result": string summary} required.
- Successful finish_task FR → its args (or unwrapped value) become terminal Output. Validation failure → error FR back to model, which retries.
- Chat coordinator delegates to task sub-agents via FunctionCall (TaskAgentTool), each dispatched with workflow.RunNode(WithRunID(fc.ID)); single_turn sub-agents via SingleTurnTool. See examples/multiagent/{task_sub_agent,single_turn,collaboration}.
- Runner: LlmAgent root with task sub-agents triggers the coordinator dispatch loop (runner.go hasTaskSubAgent).

## 17. Test files index (tests-workflow/) — consult for edge cases
agent_node_mode_{mutation,placement}, agent_node_modes, branch_isolation, conformance_parity (python parity), dynamic_node, dynamic_scheduler, edgebuilder, hitl, join_node, max_concurrency, parallel_worker(_branch), persistence, retry(_loop), run_node(_raise_on_wait), scheduler(_branch), state, synthesize_agent_output, tool_node, validation(_taskmode), workflow_node; agent_workflowagent__{hitl,reentry,workflow}; runner__*.
