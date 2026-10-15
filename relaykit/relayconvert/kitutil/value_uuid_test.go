package kitutil

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProtocolIDEntropyFailureDoesNotPanicAndKeepsUniqueIDs(t *testing.T) {
	fail := func() (uuid.UUID, error) { return uuid.Nil, errors.New("entropy unavailable") }
	before := protocolIDFallbackCounter.Load()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := protocolID(fail)
		require.Regexp(t, `^[a-f0-9]{32}$`, id)
		require.False(t, seen[id])
		seen[id] = true
	}
	require.Equal(t, before+100, protocolIDFallbackCounter.Load())
}

func TestProtocolIDConcurrentFallbackUniqueness(t *testing.T) {
	const count = 100
	fail := func() (uuid.UUID, error) { return uuid.Nil, errors.New("entropy unavailable") }
	ids := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ids <- protocolID(fail) }()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		require.False(t, seen[id])
		seen[id] = true
	}
	require.Len(t, seen, count)
}

func TestProtocolIDPreservesRandomUUIDBytes(t *testing.T) {
	id := uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")
	require.Equal(t, "00112233445566778899aabbccddeeff", protocolID(func() (uuid.UUID, error) { return id, nil }))
}
