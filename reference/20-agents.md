# 20 AGENTS — agent, agent/llmagent, workflowagents
API: api/agent.txt, api/agent.llmagent.txt, api/agent.workflowagents.*.txt, api/agent.remoteagent*.txt

## 1. agent.Agent iface
Name(), Description(), Run(InvocationContext) iter.Seq2[*session.Event,error], SubAgents(), FindAgent(name), FindSubAgent(name) + unexported → build via constructors only.
Custom: `agent.New(agent.Config{Name, Description, SubAgents, BeforeAgentCallbacks, Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error], AfterAgentCallbacks})`.
Name: non-empty, unique in tree, not "user".
Loader: agent.NewSingleLoader(a) | agent.NewMultiLoader(root, others...) (Loader,error).

## 2. Contexts
- agent.InvocationContext: session/invocation-level (Session(), InvocationID(), Branch(), UserContent(), Agent(), Artifacts(), Memory(), RunConfig(), EndInvocation(), Ended(), IsolationScope()...) + embeds context.Context.
- agent.Context (unified; used by callbacks, tools, workflow nodes) = ReadonlyContext + InvocationContext + State() session.State, Artifacts(), Actions() *session.EventActions, FunctionCallID(), SearchMemory(ctx,q), ToolConfirmation(), RequestConfirmation(hint, payload), ResumedInput(id), Path(), RunID(), SubScheduler(), WithAgentTimeout/Cancel/Context, WithDelta, OutputForAncestors.
- ReadonlyContext: AppName, UserID, SessionID, AgentName, InvocationID, ReadonlyState, UserContent, Branch.
- Conversions: agent.NewContext(ic), agent.Promote(ic), agent.NewCallbackContext(ic, actions), agent.NewToolContext(ic, fcID, actions, confirmation).
- Tests: embed agent.StrictContextMock{Ctx: ctx} (panics on un-overridden) or agent.ContextMock.
- agent.RunConfig{StreamingMode: agent.StreamingModeNone|StreamingModeSSE, SaveInputBlobsAsArtifacts}.

## 3. llmagent.New(llmagent.Config) (agent.Agent, error)
Fields: Name, Description, SubAgents, Model model.LLM, Instruction (template {state_key}, {key?} optional, {artifact.name}), InstructionProvider func(agent.ReadonlyContext)(string,error) (no templating; use util/instructionutil.InjectSessionState), GlobalInstruction(+Provider; root only), GenerateContentConfig *genai.GenerateContentConfig (temperature, safety; NOT tools), Tools []tool.Tool, Toolsets []tool.Toolset, InputSchema *genai.Schema (as tool), OutputSchema *genai.Schema (structured output; injects set_model_response tool so other tools still usable), OutputKey (write final reply to session state key), IncludeContents (IncludeContentsNone|IncludeContentsDefault), DisallowTransferToParent/Peers, Mode, callbacks:
- BeforeAgentCallbacks/AfterAgentCallbacks []agent.BeforeAgentCallback = func(agent.Context)(*genai.Content, error)
- BeforeModelCallbacks func(agent.Context, *model.LLMRequest)(*model.LLMResponse, error) — non-nil → skip model
- AfterModelCallbacks func(agent.Context, *model.LLMResponse, error)(*model.LLMResponse, error)
- OnModelErrorCallbacks func(agent.Context, *model.LLMRequest, error)(*model.LLMResponse, error)
- BeforeToolCallbacks func(agent.Context, tool.Tool, args map[string]any)(map[string]any, error) — non-nil → skip tool; mutate args in place + (nil,nil) to modify
- AfterToolCallbacks func(agent.Context, tool.Tool, args, result map[string]any, err error)(map[string]any, error)
- OnToolErrorCallbacks func(agent.Context, tool.Tool, args map[string]any, err error)(map[string]any, error)

### Modes (llmagent.ModeUnset|ModeChat|ModeTask|ModeSingleTurn)
Default: ModeChat as sub-agent / root; ModeSingleTurn as workflow graph node.
- chat: conversational, reachable via transfer_to_agent; builds prompt from session history; ignores node input. In graph only directly after Start.
- single_turn: no user chat; seeded with node input as user content; history scoped to current turn (unless IncludeContents=Default explicitly); reply → node Output.
- task: multi-turn with user until it calls finish_task tool (validated output → terminal Output). Used as sub-agent of chat coordinator (delegated via FunctionCall) or via workflow.RunNode. NOT static graph node.
- Chat coordinator with task sub-agents: coordinator dispatches task FCs, runs each via RunNode with WithRunID(fc.ID), feeds FR back. examples/multiagent/{task_sub_agent,single_turn,collaboration}.
Helpers: llmagent.RunLLMAgentAsNode(a, ctx, input), PrepareLLMAgentInput, ProcessLLMAgentOutput.

## 4. Composite agents (agent/workflowagents/*)
- sequentialagent.New(sequentialagent.Config{AgentConfig: agent.Config{Name, SubAgents: [...]}})
- parallelagent.New(parallelagent.Config{AgentConfig: ...}) (branch-isolated)
- loopagent.New(loopagent.Config{AgentConfig: ..., MaxIterations uint (0=until escalate)}); exit with tool/exitlooptool (exitlooptool.New()) or ctx.Actions().Escalate=true.
- Data passing between them: OutputKey → state → {key} in next Instruction.
- examples: src/examples/workflowagents/{sequential,sequentialCode,parallel,loop}.

## 5. Graph agent: agent/workflowagent
workflowagent.New(workflowagent.Config{Name, Description, SubAgents []agent.Agent, BeforeAgentCallbacks, AfterAgentCallbacks, Edges []workflow.Edge}) (agent.Agent, error). One instance serves concurrent sessions (state in session). See 10-graph-workflow.md.

## 6. Remote (A2A)
remoteagent.NewA2A(remoteagent.A2AConfig{...}) (+ remoteagent/v2 for a2a-go v2). Server side: server/adka2a(+v2), launcher `web a2a`.
