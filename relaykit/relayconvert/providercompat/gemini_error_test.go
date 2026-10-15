package providercompat

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGeminiError(t *testing.T) {
	for _, test := range []struct {
		status string
		kind   string
	}{
		{"INVALID_ARGUMENT", "invalid_request_error"},
		{"PERMISSION_DENIED", "permission_error"},
		{"NOT_FOUND", "not_found_error"},
		{"RESOURCE_EXHAUSTED", "rate_limit_error"},
		{"UNAUTHENTICATED", "authentication_error"},
		{"UNAVAILABLE", "overloaded_error"},
		{"INTERNAL", "api_error"},
		{"DEADLINE_EXCEEDED", "timeout_error"},
		{"UNKNOWN", ""},
	} {
		t.Run(test.status, func(t *testing.T) {
			info := ParseGeminiError([]byte(fmt.Sprintf(`{"error":{"status":%q,"code":400,"message":"provider detail"}}`, test.status)))
			require.NotNil(t, info)
			require.Equal(t, test.kind, info.AnthropicType)
			require.Equal(t, test.status, info.Status)
			require.Equal(t, 400, info.Code)
			require.Equal(t, "provider detail", info.Message)
		})
	}
	info := ParseGeminiError([]byte(`{"error":{"status":" invalid_argument "}}`))
	require.NotNil(t, info)
	require.Equal(t, "INVALID_ARGUMENT", info.Status)
	require.Equal(t, "invalid_request_error", info.AnthropicType)
	for _, body := range []string{"", "{broken", "null", "[]", `{}`, `{"error":null}`, `{"error":{}}`, `{"error":{"status":" "}}`} {
		require.Nil(t, ParseGeminiError([]byte(body)), body)
	}
}
