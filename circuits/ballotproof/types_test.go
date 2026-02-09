package ballotproof

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vocdoni/davinci-node/types"
)

func TestCircomInputsIncludesGroupSize(t *testing.T) {
	inputs := CircomInputs{
		GroupSize: new(types.BigInt).SetInt(3),
	}

	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(data), "\"group_size\"") {
		t.Fatalf("expected group_size field in JSON: %s", string(data))
	}
}
