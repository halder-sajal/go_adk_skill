package main

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/workflow"
)

func main() {
	s := workflow.NewFunctionNode("s", func(_ agent.Context, x string) (string, error) { return x, nil }, workflow.NodeConfig{})
	i := workflow.NewFunctionNode("i", func(_ agent.Context, x int) (int, error) { return x, nil }, workflow.NodeConfig{})
	_, err := workflow.New("m", workflow.Chain(workflow.Start, s, i))
	fmt.Println("string->int typed edge:", err)
	aa := workflow.NewFunctionNode("aa", func(_ agent.Context, x any) (any, error) { return x, nil }, workflow.NodeConfig{})
	_, err = workflow.New("m2", workflow.Chain(workflow.Start, s, aa, i))
	fmt.Println("string->any->int:", err)

	chat, _ := llmagent.New(llmagent.Config{Name: "chatty", Mode: llmagent.ModeChat})
	cn, _ := workflow.NewAgentNode(chat, workflow.NodeConfig{})
	_, err = workflow.New("m3", workflow.Chain(workflow.Start, s, cn))
	fmt.Println("chat after non-start:", err)
	task, _ := llmagent.New(llmagent.Config{Name: "tasky", Mode: llmagent.ModeTask})
	tn, _ := workflow.NewAgentNode(task, workflow.NodeConfig{})
	_, err = workflow.New("m4", workflow.Chain(workflow.Start, tn))
	fmt.Println("task node:", err)
	fmt.Println(full.NewLauncher().CommandLineSyntax())
}
