# 50 RECIPES — all compiled+run on v2.4.0
Full source (authoritative, compiles): verify/recipes/main.go ; semantics harness: verify/main.go ; output: verify/OUTPUT.txt
Project setup:
```
go mod init X && go get google.golang.org/adk/v2@v2.4.0   # go 1.26.6+
imports: google.golang.org/adk/v2/{agent,agent/llmagent,agent/workflowagent,workflow,session,runner,model/gemini,tool,tool/functiontool,cmd/launcher,cmd/launcher/full}, google.golang.org/genai
```

## R0 skeleton main (launcher: console / web api webui)
```go
root, err := workflowagent.New(workflowagent.Config{Name: "wf", Edges: edges, SubAgents: llmAgents})
l := full.NewLauncher()
if err := l.Execute(ctx, &launcher.Config{AgentLoader: agent.NewSingleLoader(root)}, os.Args[1:]); err != nil { log.Fatalf("%v\n\n%s", err, l.CommandLineSyntax()) }
// go run . console   |   go run . web api webui  → http://localhost:8080
```
## R1 programmatic run + HITL resume
see verify/recipes/main.go runProgrammatic: runner.NewInMemory → r.Run(ctx,u,s,genai.NewContentFromText(..., genai.RoleUser), agent.RunConfig{}) → capture ev.RequestedInput.InterruptID → r.Run same session with FunctionResponse{ID, Name: workflow.WorkflowInputFunctionCallName, Response: {"payload": v}}.
## R2 sequence
`workflow.Chain(workflow.Start, n1, n2, n3)`
## R3 routing (no emit needed)
```go
route := workflow.NewFunctionNode("route", func(ctx agent.Context, label string) (*session.Event, error) {
    ev := session.NewEvent(ctx, ctx.InvocationID()); ev.Routes = []string{label}; ev.Output = label; return ev, nil
}, workflow.NodeConfig{})
edges := workflow.NewEdgeBuilder().Add(workflow.Start, classifyNode).Add(classifyNode, route).
    AddRoute(route, billing, workflow.StringRoute("billing")).AddRoute(route, other, workflow.Default).Build()
```
## R4 fan-out / join
`NewEdgeBuilder().AddFanOut(workflow.Start, a, b).AddFanIn(join, a, b).Add(join, next)`; join := workflow.NewJoinNode("join"); next input map[string]any{"a":..., "b":...}
## R5 loop
`AddRoute(n, n, workflow.StringRoute("again")).AddRoute(n, exit, workflow.StringRoute("done"))` — node emits Routes each pass; output fed back as its own input.
## R6 map over list
`pw, _ := workflow.NewParallelWorker("each", perItemNode, 4, workflow.NodeConfig{RetryConfig: workflow.DefaultRetryConfig()})` ; predecessor must output a slice; output []any ordered.
## R7 LLM agent node (single_turn) with tool + structured output
```go
wt, _ := functiontool.New(functiontool.Config{Name: "get_weather", Description: "..."}, func(ctx agent.Context, a Weather) (WeatherRes, error) {...})
a, _ := llmagent.New(llmagent.Config{Name: "weather_reporter", Model: m, Instruction: "...", Tools: []tool.Tool{wt},
    OutputSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"city": {Type: genai.TypeString}}, Required: []string{"city"}},
    OutputKey: "weather_report"})
n, _ := workflow.NewAgentNode(a, workflow.NodeConfig{RetryConfig: workflow.DefaultRetryConfig(), Timeout: 60*time.Second})
// remember: SubAgents: []agent.Agent{a} in workflowagent.Config
```
## R8 dynamic orchestrator + HITL approval (re-entry) + parallel children
```go
workflow.NewDynamicNode[string, []string]("orchestrator", func(ctx agent.Context, in string, emit func(*session.Event) error) ([]string, error) {
    reply, err := workflow.ResumeOrRequestInput(ctx, emit, session.RequestInput{InterruptID: "approve-" + ctx.InvocationID(), Message: "Approve?"})
    if err != nil { return nil, err }            // pause first pass
    g, _ := errgroup.WithContext(ctx)
    for i, it := range items { g.Go(func() error {
        v, err := workflow.RunNode[string](ctx, worker, it, workflow.WithRunID("item-"+it), workflow.WithUseSubBranch()); out[i] = v; return err }) }
    return out, g.Wait()
}, workflow.NodeConfig{})
```
## R9 HITL handoff (two nodes)
asker: NewEmittingFunctionNode → emit(workflow.NewRequestInputEvent(ctx, session.RequestInput{InterruptID: "ask-"+uuid.NewString(), Message: "..."})); return nil, workflow.ErrNodeInterrupted. Successor gets reply as input.
## R10 state
write: emit ev with ev.Actions.StateDelta["k"]=v (or ctx.State().Set in tool/callback); read: ctx.Session().State().Get("k") / NewFunctionNodeFromState Params{ K int `state:"k"`; NodeInput T `state:"node_input"` } / Instruction "{k}".
seed: r.Run(..., runner.WithStateDelta(map[string]any{"k": v}))
## R11 custom node
struct embeds workflow.BaseNode (NewBaseNode(name, desc, cfg)) + Run(ctx agent.Context, in any) iter.Seq2[*session.Event, error].
## R12 nested workflow
sub, _ := workflow.NewWorkflowNode("sub", subEdges) ; use as node (name ≠ parent workflow name).
## R13 resilient model wrapper (timeouts on stalled LLM calls)
src/examples/workflow/complex/resilient_model.go (implements model.LLM; per-attempt context.WithTimeout + retry + fallback).
## R14 persistent sessions
svc, _ := database.NewSessionService(sqlite.Open("adk.db")); database.AutoMigrate(svc); runner.New(runner.Config{AppName, Agent, SessionService: svc, AutoCreateSession: true}) (sqlite = github.com/glebarez/sqlite)
