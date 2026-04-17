package schema

type Usage struct {
	UserTurns      int `json:"user_turns"`
	AssistantTurns int `json:"assistant_turns"`
	ToolCalls      int `json:"tool_calls"`
	InputTokens    int `json:"input_tokens"`
	OutputTokens   int `json:"output_tokens"`
}
