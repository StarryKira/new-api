package kitutil

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

func GetPointer[T any](v T) *T {
	return &v
}

func Interface2String(inter any) string {
	switch inter.(type) {
	case string:
		return inter.(string)
	case int:
		return fmt.Sprintf("%d", inter.(int))
	case float64:
		return strconv.FormatFloat(inter.(float64), 'f', -1, 64)
	case bool:
		if inter.(bool) {
			return "true"
		} else {
			return "false"
		}
	case nil:
		return ""
	}
	return fmt.Sprintf("%v", inter)
}

func String2Int(str string) int {
	num, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return num
}

// GetUUID generates transient protocol identifiers. Failure of the entropy
// source must not panic while processing an otherwise valid model response.
func GetUUID() string { return protocolID(uuid.NewRandom) }

var protocolIDFallbackCounter atomic.Uint64

func protocolID(randomUUID func() (uuid.UUID, error)) string {
	id, err := randomUUID()
	if err == nil {
		return hex.EncodeToString(id[:])
	}
	// The fallback is for correlation IDs, never authentication tokens. The
	// counter prevents collisions even when concurrent calls share a timestamp.
	var seed [16]byte
	binary.BigEndian.PutUint64(seed[:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(seed[8:], protocolIDFallbackCounter.Add(1))
	digest := sha256.Sum256(seed[:])
	return hex.EncodeToString(digest[:16])
}

func GetTimestamp() int64 {
	return time.Now().Unix()
}
