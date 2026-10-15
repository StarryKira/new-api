package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesUsageJSONNestedCanonicalPriority(t *testing.T) {
	for _, test := range []struct {
		body string
		want int
	}{
		{`{"prompt_tokens":20,"completion_tokens":3,"cache_creation_input_tokens":99,"input_tokens_details":{"cache_write_tokens":6,"cache_creation_tokens":8}}`, 6},
		{`{"input_tokens":20,"cache_creation_input_tokens":99,"input_tokens_details":{"cache_write_tokens":0,"cache_creation_tokens":8}}`, 0},
		{`{"input_tokens":20,"cache_creation_input_tokens":99,"input_tokens_details":{"cache_creation_tokens":6},"prompt_tokens_details":{"cache_write_tokens":4}}`, 4},
		{`{"input_tokens":20,"cache_write_input_tokens":5}`, 5},
	} {
		raw, err := NormalizeResponsesUsageJSON([]byte(test.body))
		require.NoError(t, err)
		var usage Usage
		require.NoError(t, kitutil.Unmarshal(raw, &usage))
		require.Equal(t, 20, usage.InputTokens)
		require.Equal(t, test.want, usage.LegacyCacheCreationInputTokens)
		require.NotNil(t, usage.InputTokensDetails)
		require.Equal(t, test.want, usage.InputTokensDetails.CacheCreationTokensTotal())
	}
}
