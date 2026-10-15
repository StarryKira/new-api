package toolconv

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func preserveOpenAITool(set Set, options *convmeta.Options, kind string) bool {
	return options != nil && kind != "" &&
		(set.Source == types.RelayFormatOpenAI || set.Source == types.RelayFormatOpenAIResponses) &&
		slices.Contains(options.OpenAICompatibleToolTypes, kind)
}

// A provider may use the same native hosted-tool choice on both OpenAI-like
// endpoints. Retain it only when the host opted in and that tool was declared.
func openAIDialectChoice(set Set, options *convmeta.Options) (json.RawMessage, bool) {
	if set.Choice == nil || len(set.Choice.Raw) == 0 {
		return nil, false
	}
	raw := set.Choice.Raw
	var name string
	if kitutil.Unmarshal(raw, &name) != nil {
		var object struct {
			Type string `json:"type"`
		}
		if kitutil.Unmarshal(raw, &object) != nil {
			return nil, false
		}
		name = object.Type
	}
	name = strings.TrimSpace(name)
	if !preserveOpenAITool(set, options, name) {
		return nil, false
	}
	for _, tool := range set.Definitions {
		if tool.NativeType == name {
			return append(json.RawMessage(nil), raw...), true
		}
	}
	return nil, false
}
