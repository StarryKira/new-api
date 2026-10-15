package relayconvert

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ValidateResponseJSON rejects upstream error envelopes before decoding into a
// protocol DTO can discard an unknown error field. A transport success marker
// received later must not turn such an error into a successful completion.
func ValidateResponseJSON(format types.RelayFormat, data []byte) error {
	var object map[string]json.RawMessage
	if err := kitutil.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("decode %s response: %w", format, err)
	}
	if object == nil {
		return fmt.Errorf("%s response must be an object", format)
	}
	if failure := bytes.TrimSpace(object["error"]); len(failure) > 0 && !bytes.Equal(failure, []byte("null")) {
		return fmt.Errorf("upstream %s error: %s", format, failure)
	}
	return nil
}
