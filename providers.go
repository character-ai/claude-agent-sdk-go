package claudeagent

import "os"

// OpenAIProvider returns an LLMProvider for OpenAI's API.
// Uses OPENAI_API_KEY environment variable.
//
//	agent := claude.NewAPIAgent(claude.APIAgentConfig{
//	    Provider: claude.OpenAIProvider("gpt-4o"),
//	})
func OpenAIProvider(model string) LLMProvider {
	return NewOpenAICompatProvider(OpenAICompatConfig{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		Model:   model,
	}).WithName("openai")
}

// MistralProvider returns an LLMProvider for Mistral AI's API.
// Uses MISTRAL_API_KEY environment variable.
//
//	agent := claude.NewAPIAgent(claude.APIAgentConfig{
//	    Provider: claude.MistralProvider("mistral-large-latest"),
//	})
func MistralProvider(model string) LLMProvider {
	return NewOpenAICompatProvider(OpenAICompatConfig{
		BaseURL: "https://api.mistral.ai/v1",
		APIKey:  os.Getenv("MISTRAL_API_KEY"),
		Model:   model,
	}).WithName("mistral")
}

// DeepSeekProvider returns an LLMProvider for DeepSeek's API.
// Uses DEEPSEEK_API_KEY environment variable.
//
//	agent := claude.NewAPIAgent(claude.APIAgentConfig{
//	    Provider: claude.DeepSeekProvider("deepseek-chat"),
//	})
func DeepSeekProvider(model string) LLMProvider {
	return NewOpenAICompatProvider(OpenAICompatConfig{
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		Model:   model,
	}).WithName("deepseek")
}

// QwenProvider returns an LLMProvider for Alibaba's Qwen models via DashScope.
// Uses DASHSCOPE_API_KEY environment variable.
//
//	agent := claude.NewAPIAgent(claude.APIAgentConfig{
//	    Provider: claude.QwenProvider("qwen-max"),
//	})
func QwenProvider(model string) LLMProvider {
	return NewOpenAICompatProvider(OpenAICompatConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey:  os.Getenv("DASHSCOPE_API_KEY"),
		Model:   model,
	}).WithName("qwen")
}
