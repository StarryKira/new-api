package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestStringConstPreservesIntersectionAndPropertyNames(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`{"const":"browser"}`, `{"type":"STRING","enum":["browser"]}`},
		{`{"type":"string","const":"","enum":[""]}`, `{"type":"STRING","enum":[""]}`},
		{`{"type":"string","const":"browser","enum":["shell"]}`, `{"type":"STRING","enum":[]}`},
		{`{"type":"object","properties":{"const":{"const":"browser"}}}`, `{"type":"OBJECT","properties":{"const":{"type":"STRING","enum":["browser"]}}}`},
	} {
		var schema any
		require.NoError(t, kitutil.Unmarshal([]byte(test.input), &schema))
		field, value, losses := FunctionParameters(schema, false)
		require.Equal(t, "parameters", field)
		require.Empty(t, losses)
		data, err := kitutil.Marshal(value)
		require.NoError(t, err)
		require.JSONEq(t, test.want, string(data))
	}
}

func TestIntegerExclusiveMinimumAndOptionalScalarEnumPolicy(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"weak":       map[string]any{"type": "integer", "minimum": 1, "exclusiveMinimum": 2},
		"strict":     map[string]any{"type": "integer", "minimum": 5, "exclusiveMinimum": 2},
		"fractional": map[string]any{"type": "integer", "exclusiveMinimum": 0.5},
		"enum":       map[string]any{"enum": []any{"on", false, float64(1), nil}},
	}}
	cleaned := CleanFunctionParameters(schema, true).(map[string]any)["properties"].(map[string]any)
	require.EqualValues(t, 3, cleaned["weak"].(map[string]any)["minimum"])
	require.EqualValues(t, 5, cleaned["strict"].(map[string]any)["minimum"])
	require.NotContains(t, cleaned["fractional"], "minimum")
	require.Equal(t, []any{"on", "false", "1", "null"}, cleaned["enum"].(map[string]any)["enum"])
	ordinary := CleanFunctionParameters(schema).(map[string]any)["properties"].(map[string]any)
	require.NotContains(t, ordinary["enum"], "enum")
}
