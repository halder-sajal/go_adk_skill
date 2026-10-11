# 40 TOOLS & MODELS
API: api/tool*.txt, api/model*.txt

## tool
- tool.Tool iface {Name, Description, IsLongRunning} (+ internal run/declaration via concrete types). Toolset {Name, Tools(agent.ReadonlyContext)([]Tool,error)}.
- tool.FilterToolset(ts, pred), tool.StringPredicate([]string), tool.AllowedToolsPredicate, tool.WithConfirmation(ts, bool, ConfirmationProvider).
## functiontool
```go
type Args struct{ City string `json:"city" jsonschema:"city name"` }
type Res struct{ Temp float64 `json:"temp"` }
t, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "..."},
    func(ctx agent.Context, a Args) (Res, error) { return Res{21}, nil })
```
Config{Name, Description, InputSchema, OutputSchema *jsonschema.Schema (inferred if nil), IsLongRunning, RequireConfirmation, RequireConfirmationProvider (func(Args) bool)}. functiontool.NewStreaming(cfg, func(agent.Context, Args) iter.Seq2[string,error]). ErrInvalidArgument. Non-struct results wrapped as {"result": X}.
Tool state: ctx.State().Set / ctx.Actions().StateDelta; ctx.Actions().SkipSummarization / TransferToAgent / Escalate.
## other tools
- agenttool.New(agent, &agenttool.Config{SkipSummarization}) — agent as a tool.
- geminitool.GoogleSearch{} (Gemini built-in search; Gemini 2+), geminitool.New(name, desc, *genai.Tool) for other built-ins.
- mcptoolset.New(mcptoolset.Config{Transport mcp.Transport | Endpoint string (streamable HTTP), Client, Auth auth.CredentialProvider, RequireConfirmation(+Provider)}) (tool.Toolset, error). stdio: &mcp.CommandTransport{Command: exec.Command(...)} (modelcontextprotocol/go-sdk/mcp). examples/mcp.
- exitlooptool.New(), loadartifactstool, loadmemorytool, preloadmemorytool, exampletool, skilltoolset.New(ctx, Config) (Agent Skills dirs; examples/skills), toolconfirmation.
- In graph: workflow.NewToolNode(t, cfg) runs a tool directly as a node (no LLM).

## models
- model.LLM iface {Name(); GenerateContent(ctx, *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error]} — wrap for retries/timeouts (see src/examples/workflow/complex/resilient_model.go).
- gemini.NewModel(ctx, "gemini-flash-latest"|"gemini-3.5-flash"|..., &genai.ClientConfig{APIKey: os.Getenv("GOOGLE_API_KEY")}) — empty ClientConfig reads env (GOOGLE_API_KEY / GOOGLE_GENAI_USE_VERTEXAI + GOOGLE_CLOUD_PROJECT/LOCATION). examples/vertexai.
- openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{APIKey, BaseURL /*OpenAI-compatible*/, HTTPClient, Options []option.RequestOption}) — empty → OPENAI_API_KEY/OPENAI_BASE_URL env. examples/openai.
- apigee model; model.NewLLM(ctx, name) registry.
- LLMRequest{Model, Contents []*genai.Content, Config *genai.GenerateContentConfig, Tools...}; LLMResponse{Content, Partial, TurnComplete, UsageMetadata, ErrorCode, ErrorMessage...}.
