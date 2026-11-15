package chat_completions

import sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"

func init() {
	sdktranslator.Register(sdktranslator.FormatOpenAI, sdktranslator.FormatOpenAIResponse, ConvertRequest, sdktranslator.ResponseTransform{
		NonStream: ConvertResponse,
		Stream:    ConvertStream,
	})
}
