// Recipes compiled against adk/v2 v2.4.0. Referenced by 50-recipes.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/workflow"
)

// R2: LLM classify -> route -> handlers, + structured-output LLM node.
func llmRouterGraph(m model.LLM) (agent.Agent, error) {
	classifier, err := llmagent.New(llmagent.Config{
		Name: "classify", Model: m,
		Instruction: "Classify the message as exactly one lowercase word: billing, tech, or other.",
	})
	if err != nil {
		return nil, err
	}
	classifyNode, err := workflow.NewAgentNode(classifier, workflow.NodeConfig{
		RetryConfig: workflow.DefaultRetryConfig(), Timeout: 60 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	route := workflow.NewFunctionNode("route", func(ctx agent.Context, label string) (*session.Event, error) {
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Routes = []string{label}
		ev.Output = label
		return ev, nil
	}, workflow.NodeConfig{})
	billing := workflow.NewFunctionNode("billing", func(ctx agent.Context, _ string) (string, error) { return "billing team", nil }, workflow.NodeConfig{})
	tech := workflow.NewFunctionNode("tech", func(ctx agent.Context, _ string) (string, error) { return "tech team", nil }, workflow.NodeConfig{})
	other := workflow.NewFunctionNode("other", func(ctx agent.Context, _ string) (string, error) { return "general", nil }, workflow.NodeConfig{})

	edges := workflow.NewEdgeBuilder().
		Add(workflow.Start, classifyNode).
		Add(classifyNode, route).
		AddRoute(route, billing, workflow.StringRoute("billing")).
		AddRoute(route, tech, workflow.StringRoute("tech")).
		AddRoute(route, other, workflow.Default).
		Build()
	return workflowagent.New(workflowagent.Config{
		Name: "router", Edges: edges, SubAgents: []agent.Agent{classifier},
	})
}

// R3: LLM agent with tool + structured output used as a node.
type Weather struct {
	City string `json:"city"`
}
type WeatherRes struct {
	TempC float64 `json:"temp_c"`
}

func toolAgentNode(m model.LLM) (*workflow.AgentNode, agent.Agent, error) {
	wt, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Current temperature for a city"},
		func(ctx agent.Context, a Weather) (WeatherRes, error) { return WeatherRes{TempC: 21}, nil })
	if err != nil {
		return nil, nil, err
	}
	a, err := llmagent.New(llmagent.Config{
		Name: "weather_reporter", Model: m,
		Instruction: "Use get_weather for the city in the input and report.",
		Tools:       []tool.Tool{wt},
		OutputSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{
			"city": {Type: genai.TypeString}, "summary": {Type: genai.TypeString},
		}, Required: []string{"city", "summary"}},
		OutputKey: "weather_report",
	})
	if err != nil {
		return nil, nil, err
	}
	n, err := workflow.NewAgentNode(a, workflow.NodeConfig{})
	return n, a, err
}

// R4: dynamic fan-out with errgroup + HITL approval with re-entry.
func dynamicApproval() workflow.Node {
	worker := workflow.NewFunctionNode("work", func(_ agent.Context, s string) (string, error) { return "done:" + s, nil }, workflow.NodeConfig{})
	return workflow.NewDynamicNode[string, []string]("orchestrator",
		func(ctx agent.Context, in string, emit func(*session.Event) error) ([]string, error) {
			reply, err := workflow.ResumeOrRequestInput(ctx, emit, session.RequestInput{
				InterruptID: "approve-" + ctx.InvocationID(),
				Message:     "Approve processing of: " + in + "? (yes/no)",
			})
			if err != nil {
				return nil, err // ErrNodeInterrupted on first pass => pause
			}
			if reply != "yes" {
				return []string{"rejected"}, nil
			}
			items := []string{"a", "b", "c"}
			out := make([]string, len(items))
			g, _ := errgroup.WithContext(ctx)
			for i, it := range items {
				g.Go(func() error {
					v, err := workflow.RunNode[string](ctx, worker, it, workflow.WithRunID("item-"+it), workflow.WithUseSubBranch())
					out[i] = v
					return err
				})
			}
			if err := g.Wait(); err != nil {
				var nre *workflow.NodeRunError
				if errors.As(err, &nre) {
					return nil, fmt.Errorf("child %s failed: %w", nre.ChildPath, err)
				}
				return nil, err
			}
			return out, nil
		}, workflow.NodeConfig{})
}

// R5: custom node implementing Node directly.
type echoNode struct{ workflow.BaseNode }

func (n *echoNode) Run(ctx agent.Context, in any) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Output = in
		ev.Content = genai.NewContentFromText(fmt.Sprint(in), genai.RoleModel)
		yield(ev, nil)
	}
}

// R1: run programmatically, detect pause, resume.
func runProgrammatic(ctx context.Context, root agent.Agent) error {
	r, err := runner.NewInMemory("app", root)
	if err != nil {
		return err
	}
	var pendingID string
	for ev, err := range r.Run(ctx, "user1", "sess1", genai.NewContentFromText("hello", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			return err
		}
		if ev.RequestedInput != nil {
			pendingID = ev.RequestedInput.InterruptID
		}
		if ev.Output != nil {
			fmt.Println("output:", ev.Output)
		}
	}
	if pendingID != "" {
		resume := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: pendingID, Name: workflow.WorkflowInputFunctionCallName, Response: map[string]any{"payload": "yes"},
		}}}}
		for ev, err := range r.Run(ctx, "user1", "sess1", resume, agent.RunConfig{}) {
			if err != nil {
				return err
			}
			if ev.Output != nil {
				fmt.Println("output:", ev.Output)
			}
		}
	}
	return nil
}

func main() {
	ctx := context.Background()
	// No-LLM path: exercise R4 + R5 + R1 end to end.
	echo := &echoNode{BaseNode: workflow.NewBaseNode("echo", "echo input", workflow.NodeConfig{})}
	root, err := workflowagent.New(workflowagent.Config{Name: "demo", Edges: workflow.Chain(workflow.Start, dynamicApproval(), echo)})
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "selftest" {
		if err := runProgrammatic(ctx, root); err != nil {
			log.Fatal(err)
		}
		if _, err := llmRouterGraph(nil); err != nil {
			log.Fatal("router: ", err)
		}
		if _, _, err := toolAgentNode(nil); err != nil {
			log.Fatal("toolnode: ", err)
		}
		fmt.Println("llm graphs build OK")
		return
	}
	// LLM path (needs GOOGLE_API_KEY): launcher with router graph.
	m, err := gemini.NewModel(ctx, "gemini-flash-latest", &genai.ClientConfig{APIKey: os.Getenv("GOOGLE_API_KEY")})
	if err != nil {
		log.Fatal(err)
	}
	router, err := llmRouterGraph(m)
	if err != nil {
		log.Fatal(err)
	}
	_, _, _ = toolAgentNode(m)
	_ = uuid.NewString
	l := full.NewLauncher()
	if err := l.Execute(ctx, &launcher.Config{AgentLoader: agent.NewSingleLoader(router)}, os.Args[1:]); err != nil {
		log.Fatalf("%v\n\n%s", err, l.CommandLineSyntax())
	}
}
