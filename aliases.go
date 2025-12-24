package gai

// SetAlias adds or updates a model alias at runtime.
// Aliases allow using short names instead of full model strings:
//
//	client.SetAlias("fast", "groq/llama3-70b-8192")
//	text, _ := client.Text(ctx, gai.Request("fast").User("Hello"))
func (c *Client) SetAlias(alias, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aliases[alias] = model
}

// GetAlias returns the model string for an alias.
// Returns the model string and true if found, empty string and false otherwise.
func (c *Client) GetAlias(alias string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	model, ok := c.aliases[alias]
	return model, ok
}

// RemoveAlias removes an alias.
func (c *Client) RemoveAlias(alias string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.aliases, alias)
}

// Aliases returns a copy of all defined aliases.
func (c *Client) Aliases() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]string, len(c.aliases))
	for k, v := range c.aliases {
		result[k] = v
	}
	return result
}

// Common aliases that users might want to use.
// These are not set by default but can be used as reference.
const (
	// AliasOpenAIGPT4o is the current best OpenAI model.
	AliasOpenAIGPT4o = "openai/gpt-4o"

	// AliasOpenAIGPT4oMini is the fast/cheap OpenAI model.
	AliasOpenAIGPT4oMini = "openai/gpt-4o-mini"

	// AliasAnthropicSonnet is the Claude 3.5 Sonnet model.
	AliasAnthropicSonnet = "anthropic/claude-3-5-sonnet"

	// AliasAnthropicHaiku is the fast Claude model.
	AliasAnthropicHaiku = "anthropic/claude-3-haiku"

	// AliasGeminiFlash is the fast Gemini model.
	AliasGeminiFlash = "gemini/gemini-2.0-flash"

	// AliasGroqLlama is a fast Llama model via Groq.
	AliasGroqLlama = "groq/llama3-70b-8192"
)
