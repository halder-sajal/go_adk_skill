---
name: adk-go
description: Verified reference for Google ADK Go v2 (google.golang.org/adk/v2 v2.4.0), focused on graph workflows (package workflow + agent/workflowagent) - FunctionNode/AgentNode/ToolNode/JoinNode/ParallelWorker/WorkflowNode/DynamicNode, edges and routing, loops, fan-out/fan-in, RunNode, human-in-the-loop pause/resume, retries, state, validation errors - plus llmagent modes, runner, session, tools, models and launcher. Use when writing, reviewing or debugging Go code that imports google.golang.org/adk/v2, or when asked about ADK graphs/workflows in Go.
---

# ADK Go v2 reference (graph workflows)

Source of truth: `google.golang.org/adk/v2` **v2.4.0** (github.com/google/adk-go tag v2.4.0, commit 4e57df40). v1 lives at a different import path (`google.golang.org/adk`, last v1.7.0) and has no graph engine. Do not mix them, and do not answer from Python ADK memory: the Go API differs.

All paths below are relative to this skill's directory. Read the file that matches the task instead of guessing an API. Grep `api/` for exact signatures.

## Files
| path | use it for |
|---|---|
| reference/10-graph-workflow.md | **graph engine semantics**: node types, edges, routing rules, loops, JoinNode, ParallelWorker, dynamic RunNode, HITL, NodeConfig/retry, validation errors, events, remaining symbols, debug endpoints, task mode |
| reference/50-recipes.md | copy-paste templates (compiled and run on v2.4.0; full source in verify/recipes/main.go) |
| reference/60-gotchas.md | non-obvious behaviours and pitfalls. Check before finalizing code |
| reference/00-overview.md | package map, v2 breaking changes, idioms |
| reference/20-agents.md | agent.Agent/Context, llmagent.Config + modes (chat/task/single_turn), callbacks, sequential/parallel/loop agents |
| reference/30-runtime.md | runner, session/Event/State, HITL resume wire protocol, launcher CLI, plugins, compaction, telemetry |
| reference/40-tools-models.md | functiontool, mcptoolset, agenttool, geminitool, gemini/openai models |
| api/<pkg>.txt | `go doc -all` for all 73 public packages (`/` becomes `.`, e.g. api/workflow.txt, api/agent.llmagent.txt) |
| src/ | ADK v2.4.0 non-test source + docs (Apache-2.0, see LICENSE-ADK). Official graph examples: src/examples/workflow/ |
| tests-workflow/ | upstream tests for workflow, workflowagent, runner: the best evidence for edge cases |
| verify/ | runnable harness that proves the graph semantics without an LLM (`cd verify && go run .`; output in verify/OUTPUT.txt) |

## Lookup
- Build a graph → reference/10 §1-3, reference/50 R0-R6
- Conditional routing → reference/10 §4 (src/workflow/scheduler.go `findSuccessors`)
- LLM agent as a node → reference/10 §2.3 and reference/20 §3 (modes!)
- Code-driven orchestration → reference/10 §7 (src/workflow/dynamic_node.go, run_node.go)
- Pause for human input and resume → reference/10 §8, reference/30 §3, reference/50 R1/R8/R9
- Build-time errors → reference/10 §10 (src/workflow/validation.go)
- Running it → reference/30 §1 (runner) and §4 (launcher: `go run . console` / `go run . web api webui`)

## Core facts (quick recall)
- `workflowagent.New(workflowagent.Config{Name, Edges, SubAgents})` turns a graph into an agent.Agent for the runner/launcher. The graph starts at `workflow.Start`, and the user's message text is the first input.
- Each node activation emits at most one `Event.Output` (this becomes the successor's input) and at most one routing event (`Event.Routes`).
- Two or more unconditional edges into one node require a `JoinNode`. A cycle needs a routed edge. If no route matches and there is no `workflow.Default`, the run silently stops at that node.
- An LlmAgent used as a graph node defaults to single_turn mode. chat mode is allowed only directly after Start. task mode is never allowed as a static node (use RunNode).
- Human input: `workflow.NewRequestInputEvent`, then return `workflow.ErrNodeInterrupted`. Resume with a FunctionResponse{ID: interruptID, Name: "adk_request_input", Response: {"payload": v}}.
- `session.NewEvent(ctx, invocationID)` requires ctx in v2.

## Verifying claims / newer versions
- Check the latest version: `go list -m -versions google.golang.org/adk/v2`. If it is newer than v2.4.0, say so and confirm behaviour against the module source (`$(go env GOMODCACHE)/google.golang.org/adk/v2@vX.Y.Z`) before relying on this reference.
- Regenerate the API docs from the module directory:
  `go list ./... | grep -v /internal | grep -v /examples | grep -v /cmd/adkgo | while read p; do f=$(echo ${p#google.golang.org/adk/v2} | sed 's|^/||;s|/|.|g'); go doc -all $p > api/$f.txt; done`
