package api

// Message represents an OpenAI chat completion message.
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []interface{}
	Name    string      `json:"name,omitempty"`
}

// ChatCompletionRequest represents an OpenAI chat completion request.
type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// TokenUsage represents unified token usage across providers, including prompt cache breakdown.
type TokenUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`     // Discounted reads (0.1x - 0.5x)
	CacheCreationTokens int `json:"cache_creation_tokens"` // Anthropic cache writes (1.25x)
}

// OpenAIUsage represents the usage object in OpenAI stream chunks and responses.
type OpenAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// OpenAIStreamChunk is the structural representation of an OpenAI stream payload.
type OpenAIStreamChunk struct {
	Usage *OpenAIUsage `json:"usage,omitempty"`
}

// AnthropicUsage represents the usage object in Anthropic stream events and responses.
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// AnthropicMessageStart represents the message_start event payload.
type AnthropicMessageStart struct {
	Message struct {
		Usage AnthropicUsage `json:"usage"`
	} `json:"message"`
}

// AnthropicMessageDelta represents the message_delta event payload.
type AnthropicMessageDelta struct {
	Usage AnthropicUsage `json:"usage"`
}

// AnthropicCountResponse holds the token count from Anthropic's count_tokens API.
type AnthropicCountResponse struct {
	InputTokens int `json:"input_tokens"`
}
