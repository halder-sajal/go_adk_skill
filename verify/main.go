// Verification harness: exercises ADK v2 graph semantics without an LLM.
package main

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
)

func run(name string, a agent.Agent, msgs ...*genai.Content) {
	fmt.Println("=====", name)
	r, err := runner.NewInMemory("app", a)
	if err != nil {
		panic(err)
	}
	for _, m := range msgs {
		for ev, err := range r.Run(context.Background(), "u", "s-"+name, m, agent.RunConfig{}) {
			if err != nil {
				fmt.Println("  ERR:", err)
				continue
			}
			path := ""
			if ev.NodeInfo != nil {
				path = ev.NodeInfo.Path
			}
			txt := ""
			if ev.Content != nil {
				for _, p := range ev.Content.Parts {
					txt += p.Text
				}
			}
			fmt.Printf("  author=%s path=%s out=%v routes=%v text=%q req=%v delta=%v\n",
				ev.Author, path, ev.Output, ev.Routes, txt, ev.RequestedInput != nil, ev.Actions.StateDelta)
		}
	}
}

func text(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleUser) }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func main() {
	// 1. Chain + typed function nodes.
	up := workflow.NewFunctionNode("upper", func(_ agent.Context, s string) (string, error) { return strings.ToUpper(s), nil }, workflow.NodeConfig{})
	sfx := workflow.NewFunctionNode("suffix", func(_ agent.Context, s string) (string, error) { return s + "!", nil }, workflow.NodeConfig{})
	run("chain", must(workflowagent.New(workflowagent.Config{Name: "chain", Edges: workflow.Chain(workflow.Start, up, sfx)})), text("hi"))

	// 2. Loop via conditional back-edge; routing by returning *session.Event from plain FunctionNode.
	inc := workflow.NewFunctionNode("inc", func(ctx agent.Context, n int) (*session.Event, error) {
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Output = n + 1
		if n+1 < 3 {
			ev.Routes = []string{"again"}
		} else {
			ev.Routes = []string{"done"}
		}
		return ev, nil
	}, workflow.NodeConfig{})
	seed := workflow.NewFunctionNode("seed", func(_ agent.Context, _ string) (int, error) { return 0, nil }, workflow.NodeConfig{})
	fin := workflow.NewFunctionNode("fin", func(_ agent.Context, n int) (string, error) { return fmt.Sprint("final=", n), nil }, workflow.NodeConfig{})
	loopEdges := workflow.NewEdgeBuilder().Add(workflow.Start, seed).Add(seed, inc).
		AddRoute(inc, inc, workflow.StringRoute("again")).AddRoute(inc, fin, workflow.StringRoute("done")).Build()
	run("loop", must(workflowagent.New(workflowagent.Config{Name: "loop", Edges: loopEdges})), text("go"))

	// 3. Fan-out / JoinNode fan-in + Default route.
	a := workflow.NewFunctionNode("a", func(_ agent.Context, s string) (string, error) { return "A:" + s, nil }, workflow.NodeConfig{})
	b := workflow.NewFunctionNode("b", func(_ agent.Context, s string) (string, error) { return "B:" + s, nil }, workflow.NodeConfig{})
	j := workflow.NewJoinNode("join")
	sum := workflow.NewFunctionNode("sum", func(_ agent.Context, m map[string]any) (string, error) { return fmt.Sprint(m["a"], "|", m["b"]), nil }, workflow.NodeConfig{})
	fe := workflow.NewEdgeBuilder().AddFanOut(workflow.Start, a, b).AddFanIn(j, a, b).Add(j, sum).Build()
	run("fanin", must(workflowagent.New(workflowagent.Config{Name: "fanin", Edges: fe})), text("x"))

	// 4. ParallelWorker over a list.
	split := workflow.NewFunctionNode("split", func(_ agent.Context, s string) ([]string, error) { return strings.Fields(s), nil }, workflow.NodeConfig{})
	per := workflow.NewFunctionNode("len", func(_ agent.Context, s string) (int, error) { return len(s), nil }, workflow.NodeConfig{})
	pw := must(workflow.NewParallelWorker("lens", per, 2, workflow.NodeConfig{}))
	run("parallel", must(workflowagent.New(workflowagent.Config{Name: "parallel", Edges: workflow.Chain(workflow.Start, split, pw)})), text("aa bbb c"))

	// 5. Dynamic node + RunNode + state delta via emit.
	child := workflow.NewFunctionNode("child", func(_ agent.Context, n int) (int, error) { return n * 10, nil }, workflow.NodeConfig{})
	dyn := workflow.NewDynamicNode[string, int]("orch", func(ctx agent.Context, _ string, emit func(*session.Event) error) (int, error) {
		total := 0
		for i := 1; i <= 3; i++ {
			v, err := workflow.RunNode[int](ctx, child, i, workflow.WithRunID(fmt.Sprintf("item-%d", i)))
			if err != nil {
				return 0, err
			}
			total += v
		}
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Actions.StateDelta["total"] = total
		if err := emit(ev); err != nil {
			return 0, err
		}
		return total, nil
	}, workflow.NodeConfig{})
	readState := must(workflow.NewFunctionNodeFromState("read_state", func(_ agent.InvocationContext, p struct {
		Total     int `state:"total"`
		NodeInput int `state:"node_input"`
	}) (string, error) {
		return fmt.Sprintf("state.total=%d input=%d", p.Total, p.NodeInput), nil
	}, workflow.NodeConfig{}))
	run("dynamic", must(workflowagent.New(workflowagent.Config{Name: "dynamic", Edges: workflow.Chain(workflow.Start, dyn, readState)})), text("go"))

	// 6. HITL handoff via runner: pause then resume with FunctionResponse.
	ask := workflow.NewEmittingFunctionNode[any, any]("ask", func(ctx agent.Context, _ any, emit func(*session.Event) error) (any, error) {
		if err := emit(workflow.NewRequestInputEvent(ctx, session.RequestInput{InterruptID: "ask-fixed-id", Message: "name?"})); err != nil {
			return nil, err
		}
		return nil, workflow.ErrNodeInterrupted
	}, workflow.NodeConfig{})
	greet := workflow.NewFunctionNode("greet", func(_ agent.Context, n string) (string, error) { return "hello " + n, nil }, workflow.NodeConfig{})
	resume := &genai.Content{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: "ask-fixed-id", Name: workflow.WorkflowInputFunctionCallName, Response: map[string]any{"payload": "Alice"}}}}}
	run("hitl", must(workflowagent.New(workflowagent.Config{Name: "hitl", Edges: workflow.Chain(workflow.Start, ask, greet)})), text("start"), resume)

	// 7. Validation errors.
	_, err := workflow.New("bad", workflow.NewEdgeBuilder().Add(workflow.Start, a).Add(workflow.Start, b).Add(a, sum).Add(b, sum).Build())
	fmt.Println("===== fan-in without JoinNode:", err)
	_, err = workflow.New("bad2", workflow.NewEdgeBuilder().Add(workflow.Start, a).Add(a, b).Add(b, a).Build())
	fmt.Println("===== unconditional cycle:", err)
}
