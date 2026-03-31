package translator

import chatcompletions "github.com/router-for-me/CLIProxyAPI/v6/internal/translator/codex/openai/chat-completions"

// PrimeOpenAIChatCompletionsRequest stores a parsed copy of the inbound request
// so the codex request translator can reuse it instead of decoding the same body again.
func PrimeOpenAIChatCompletionsRequest(rawJSON []byte) {
	chatcompletions.PrimeOpenAIRequest(rawJSON)
}
