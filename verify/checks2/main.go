package main

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/workflow"
)

func main() {
	s := workflow.NewFunctionNode("s", func(_ agent.Context, x string) (string, error) { return x, nil }, workflow.NodeConfig{})
	i := workflow.NewFunctionNode("i", func(_ agent.Context, x int) (int, error) { return x, nil }, workflow.NodeConfig{})
	wa, _ := workflowagent.New(workflowagent.Config{Name: "w", Edges: workflow.Chain(workflow.Start, s, i)})
	r, _ := runner.NewInMemory("app", wa)
	for _, in := range []string{"42", "abc"} {
		for ev, err := range r.Run(context.Background(), "u", "s"+in, genai.NewContentFromText(in, genai.RoleUser), agent.RunConfig{}) {
			if err != nil {
				fmt.Println(in, "ERR:", err)
				continue
			}
			fmt.Printf("%s out=%v (%T)\n", in, ev.Output, ev.Output)
		}
	}
}
