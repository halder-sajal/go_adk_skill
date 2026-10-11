# 30 RUNTIME — runner, session, HITL protocol, launcher, servers, plugins
API: api/runner.txt, api/session*.txt, api/cmd.launcher*.txt, api/plugin*.txt, api/server.*.txt

## 1. runner
- runner.NewInMemory(appName, rootAgent) (*Runner, error) — in-mem session/artifact/memory, AutoCreateSession=true. Dev/tests.
- runner.New(runner.Config{AppName, Agent, SessionService (required), ArtifactService, MemoryService, PluginConfig{Plugins []*plugin.Plugin, CloseTimeout}, AutoCreateSession bool, Compaction *compaction.Config})
- r.Run(ctx, userID, sessionID, msg *genai.Content, agent.RunConfig{}, opts...) iter.Seq2[*session.Event, error]
  - opts: runner.WithStateDelta(map[string]any) (seed state), runner.WithYieldUserMessage()
  - without AutoCreateSession: create first: sessSvc.Create(ctx, &session.CreateRequest{AppName, UserID, SessionID?, State?}).
- r.RunLive(ctx, userID, sessionID, agent.LiveRunConfig) (bidi streaming; examples/bidi)
- Every event yielded is already appended to session (partial events aside). LlmAgent roots (must be chat mode) run via node runtime (single-node workflow wrapper); other roots run directly.
- Message helper: genai.NewContentFromText(s, genai.RoleUser).

## 2. session
- session.Event{LLMResponse (Content, Partial, TurnComplete, ErrorCode...), ID, Timestamp, InvocationID, Branch, IsolationScope, Author, Actions EventActions, LongRunningToolIDs, Routes, RequestedInput *RequestInput, Output any, NodeInfo *NodeInfo}
- session.NewEvent(ctx, invocationID) — allocs Actions.StateDelta & ArtifactDelta maps (write directly).
- ev.IsFinalResponse().
- EventActions{StateDelta, ArtifactDelta, RequestedToolConfirmations, SkipSummarization, TransferToAgent, Escalate, Compaction(framework-only)}.
- State writes: ctx.State().Set(k,v) in callbacks/tools, or ev.Actions.StateDelta[k]=v on an emitted event; runner merges on AppendEvent. State key prefixes: session.KeyPrefixApp "app:" (app-wide), KeyPrefixUser "user:" (per user), KeyPrefixTemp "temp:" (not persisted; stripped on AppendEvent); unprefixed = per session.
- Services: session.InMemoryService(); database.NewSessionService(gorm.Dialector) (+AutoMigrate(svc)); e.g. sqlite: github.com/glebarez/sqlite `sqlite.Open("file.db")`; session/vertexai.
- Session iface: ID, AppName, UserID, State() State{Get,Set,All}, Events() Events{All, Len, At}, LastUpdateTime.
- Get with ErrStateKeyNotExist on missing key.

## 3. HITL resume protocol (graph RequestInput & tool confirmation)
Pause turn: event with RequestedInput!=nil (+ Content FunctionCall{Name:"adk_request_input", ID: interruptID}, LongRunningToolIDs=[id]).
Resume turn: r.Run(ctx, u, sameSession, &genai.Content{Role:"user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: interruptID, Name: "adk_request_input" /*workflow.WorkflowInputFunctionCallName*/, Response: map[string]any{"payload": value}}}}}, agent.RunConfig{})
Tool confirmation: FunctionCall "adk_request_confirmation" (toolconfirmation.FunctionCallName); reply FR with confirmed bool (see examples/toolconfirmation, api/tool.toolconfirmation.txt).
Console launcher & Dev UI handle both automatically.

## 4. Launcher (cmd/launcher/full)
```go
l := full.NewLauncher()
err := l.Execute(ctx, &launcher.Config{AgentLoader: agent.NewSingleLoader(root) /*, SessionService, ArtifactService, MemoryService, PluginConfig, Authenticator, Authorizer, Compaction, TelemetryOptions*/}, os.Args[1:])
```
CLI: `go run . console [-streaming_mode none|sse]`; `go run . web [-port 8080] api webui [a2a] [pubsub] [eventarc]` (api flags: -include_debug_api, -path_prefix /api; webui: -api_server_address http://localhost:8080/api). Dev UI then at http://localhost:8080 (graph visualization, HITL prompts).
Other launchers: cmd/launcher/{console,web,prod,universal,agentengine}. Deploy: `adkgo deploy cloudrun|agentengine` (cmd/adkgo).
Servers: server/adkrest.NewServer(ServerConfig) (http.Handler), server/authn (API key / bearer), server/authz (Noop/Strict).

## 5. Plugins
plugin.New(plugin.Config{Name, OnUserMessageCallback, OnEventCallback, BeforeRunCallback, AfterRunCallback, Before/AfterAgentCallback, Before/After/OnModelError, Before/After/OnToolError, CloseFunc}) → runner.Config.PluginConfig.Plugins. Built-ins: loggingplugin.New(name), retryandreflect, functioncallmodifier. plugin/agentanalytics separate module.

## 6. Compaction
runner.Config.Compaction = &compaction.Config{...} (sliding window + tail retention; LLMSummarizer default uses root agent model → root must be LLM agent). api/session.compaction.txt; examples/compaction, compactionrecall.

## 7. Telemetry
telemetry.New(ctx, opts...) (*Providers, error); .SetGlobalOtelProviders(); Shutdown. Options WithOtelToCloud, WithSpanProcessors... Workflow nodes emit "invoke_node" spans (unless EmitsOwnSpan). examples/telemetry.

## 8. Determinism
platform.WithTimeProvider / WithUUIDProvider on ctx control event IDs/timestamps (replay tests).
