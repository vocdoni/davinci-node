package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/vocdoni/davinci-node/api"
)

// SequencerInfo method returns the information of the current Davinci sequencer.
func (c *Client) SequencerInfo() (api.SequencerInfo, error) {
	body, status, err := c.httpClient.Request(http.MethodGet, nil, nil, api.InfoEndpoint)
	if err != nil {
		return api.SequencerInfo{}, fmt.Errorf("error getting info from the sequencer: %w", err)
	}
	if status != http.StatusOK {
		return api.SequencerInfo{}, fmt.Errorf("failed to get info from sequencer, status code: %d", status)
	}
	var sequencerInfo api.SequencerInfo
	if err := json.Unmarshal(body, &sequencerInfo); err != nil {
		return api.SequencerInfo{}, fmt.Errorf("failed to parse sequencer info: %w", err)
	}
	return sequencerInfo, nil
}
