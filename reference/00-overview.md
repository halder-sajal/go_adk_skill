# 00 OVERVIEW — adk-go v2.4.0

## Package map (import prefix google.golang.org/adk/v2/)
agent                     Agent iface, Context/InvocationContext, agent.New(custom), Loader (NewSingleLoader/NewMultiLoader), RunConfig, StrictContextMock
agent/llmagent            LLM agent: llmagent.New(Config). Modes chat/task/single_turn
agent/workflowagent       workflowagent.New(Config{Name,Description,SubAgents,Before/AfterAgentCallbacks,Edges}) → graph as agent.Agent
agent/workflowagents/{sequentialagent,parallelagent,loopagent}   legacy-style composite agents (still present in v2)
agent/remoteagent(+/v2)   A2A remote agent client
workflow                  GRAPH ENGINE: Node, Edge, Route, Workflow, FunctionNode, AgentNode, ToolNode, JoinNode, ParallelWorker, WorkflowNode, DynamicNode, RunNode
runner                    runner.New(Config)/NewInMemory(app, agent); Run(ctx,userID,sessionID,*genai.Content,agent.RunConfig,...RunOption) iter.Seq2[*session.Event,error]; RunLive
session                   Event, EventActions, NodeInfo, RequestInput, Session, State, Service, InMemoryService()
session/{database,vertexai,compaction,sessiontestsuite}   gorm/sqlite DB sessions, Vertex sessions, context compaction
memory(+/vertexai), artifact(+/gcsartifact)   services
model                     LLM iface {Name(); GenerateContent(ctx,*LLMRequest,stream bool) iter.Seq2[*LLMResponse,error]}, LLMRequest/LLMResponse
model/gemini              gemini.NewModel(ctx, "gemini-flash-latest", &genai.ClientConfig{APIKey:...})
model/openaimodel         openaimodel.NewModel(ctx, name, *ClientConfig)
model/apigee              Apigee proxy model
tool                      Tool, Toolset, FilterToolset, WithConfirmation, Predicate
tool/functiontool         functiontool.New[Args,Res](Config{Name,Description,...}, func(agent.Context, Args)(Res,error)); NewStreaming
tool/{agenttool,mcptoolset,geminitool,exitlooptool,loadartifactstool,loadmemorytool,preloadmemorytool,skilltoolset,toolconfirmation,exampletool}
plugin(+/loggingplugin,retryandreflect,functioncallmodifier)   cross-cutting callbacks; plugin/agentanalytics = separate module
server/adkrest            REST API (primary) incl agent graph viz endpoint; server/adka2a(+v2) A2A; server/agentengine; server/authn, authz
cmd/launcher(+/full,console,web,web/api,web/webui,web/a2a,prod,universal,agentengine)   CLI launchers; full.NewLauncher().Execute(ctx,&launcher.Config{AgentLoader:...}, os.Args[1:])
cmd/adkgo                 `adkgo` CLI (deploy cloudrun/agentengine)
platform                  WithTimeProvider/WithUUIDProvider on ctx (deterministic events)
telemetry                 OTel setup (option style)
agentregistry, auth(+/gcp), util/{instructionutil,aiplatform,vertexai}
internal/*                NOT public

## Key deps
google.golang.org/genai v1.70.0 (Content/Part/Schema types), github.com/google/jsonschema-go v0.4.3 (workflow schemas), modelcontextprotocol/go-sdk v1.7.0, a2a-go(/v2), openai-go/v3, otel 1.46.

## v2 breaking changes (README-v2.md)
- session.NewEvent(ctx context.Context, invocationID string) — ctx REQUIRED (was NewEvent(invocationID)). NewEventWithContext removed.
- ToolContext + CallbackContext merged into single agent.Context (PR #945). Callbacks/tools all get agent.Context.
  Mocks: embed agent.StrictContextMock{Ctx: ctx} (unimplemented methods panic) or agent.ContextMock.
- New: workflow graph engine. Runner: an LlmAgent ROOT runs through the node runtime (wrapped in synthetic START->node workflow, WithRootWrapper) and MUST resolve to chat mode (else error "root agent X must be a chat LlmAgent"); non-LLM roots (e.g. workflowagent) run their own Run. [S runner/runner.go:566, runner/run_node.go]
- LlmAgent Mode (chat/task/single_turn); task agents + finish_task tool.

## Idioms / conventions
- Streams: iter.Seq2[*session.Event, error]; `for ev, err := range r.Run(...)`. Don't collect.
- Config-struct constructors (runner.New, llmagent.New, agenttool.New); workflow.New & telemetry.New use functional options.
- Before* callback returning non-nil result OR error short-circuits (model/tool). BeforeAgentCallback: only non-nil content short-circuits.
- Errors wrap %w; sentinels tested with errors.Is.
- Custom agents: agent.New(agent.Config{Name, Run: func(agent.InvocationContext) iter.Seq2[...]}) — don't implement agent.Agent directly (has unexported methods).
- Never log user/model data (repo rule).
- Tests replay HTTP via internal/httprr (testdata/*.httprr).
