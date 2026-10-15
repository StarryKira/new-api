package providercompat

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// GeminiErrorInfo preserves the native error and its Anthropic error type.
// Status is normalized; HTTP status selection and message disclosure are host policy.
type GeminiErrorInfo struct {
	Code          int
	Status        string
	Message       string
	AnthropicType string
}

// ParseGeminiError returns nil when the body has no recognizable Gemini error.
func ParseGeminiError(body []byte) *GeminiErrorInfo {
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := kitutil.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	native := envelope.Error
	status := strings.ToUpper(strings.TrimSpace(native.Status))
	if status == "" && native.Code == 0 && strings.TrimSpace(native.Message) == "" {
		return nil
	}
	info := &GeminiErrorInfo{Code: native.Code, Status: status, Message: native.Message}
	switch status {
	case "INVALID_ARGUMENT":
		info.AnthropicType = "invalid_request_error"
	case "PERMISSION_DENIED":
		info.AnthropicType = "permission_error"
	case "NOT_FOUND":
		info.AnthropicType = "not_found_error"
	case "RESOURCE_EXHAUSTED":
		info.AnthropicType = "rate_limit_error"
	case "UNAUTHENTICATED":
		info.AnthropicType = "authentication_error"
	case "UNAVAILABLE":
		info.AnthropicType = "overloaded_error"
	case "INTERNAL":
		info.AnthropicType = "api_error"
	case "DEADLINE_EXCEEDED":
		info.AnthropicType = "timeout_error"
	}
	return info
}
