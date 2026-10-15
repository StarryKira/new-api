package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestInputTokenDetailsCacheCreationAliasAndCanonicalPriority(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{`{"cache_creation_tokens":17}`, 17},
		{`{"cached_creation_tokens":17}`, 17},
		{`{"cached_creation_tokens":0,"cache_creation_tokens":17}`, 0},
	} {
		var details InputTokenDetails
		require.NoError(t, kitutil.Unmarshal([]byte(tc.raw), &details))
		require.Equal(t, tc.want, details.CachedCreationTokens)
	}
	raw, err := NormalizeInputTokenDetailsJSON([]byte(`{"cache_creation_tokens":17,"reasoning_tokens":9,"extension":1e1000}`))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"cached_creation_tokens":17`)
	require.Contains(t, string(raw), `"reasoning_tokens":9`)
	require.Contains(t, string(raw), `"extension":1e1000`)
	require.NotContains(t, string(raw), `"cache_creation_tokens"`)
}
