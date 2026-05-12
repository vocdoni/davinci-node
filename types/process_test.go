package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

func TestNestedMetadata(t *testing.T) {
	metadata := Metadata{
		Meta: GenericMetadata{
			"submeta": GenericMetadata{
				"nested": GenericMetadata{
					"key": "value",
				},
			},
		},
	}
	marshalled, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	expected := `{"title":{},"description":{},"media":{"header":"","logo":""},"questions":null,"type":{"name":"","properties":{}},"version":"","meta":{"submeta":{"nested":{"key":"value"}}}}`
	if string(marshalled) != expected {
		t.Errorf("expected %s, got %s", expected, marshalled)
	}
	var unmarshalled Metadata
	if err := json.Unmarshal(marshalled, &unmarshalled); err != nil {
		t.Fatalf("failed to unmarshal metadata: %v", err)
	}
	submeta, ok := unmarshalled.Meta["submeta"].(GenericMetadata)
	if !ok {
		t.Fatalf("expected submeta to be GenericMetadata, got %T", unmarshalled.Meta["submeta"])
	}
	nested, ok := submeta["nested"].(GenericMetadata)
	if !ok {
		t.Fatalf("expected nested to be GenericMetadata, got %T", submeta["nested"])
	}
	keyValue, ok := nested["key"].(string)
	if !ok {
		t.Fatalf("expected key to be string, got %T", nested["key"])
	}
	if keyValue != "value" {
		t.Errorf("expected key to be 'value', got %s", keyValue)
	}
}

func TestProcessStatusIsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status ProcessStatus
		want   bool
	}{
		{name: "ready", status: ProcessStatusReady, want: false},
		{name: "ended", status: ProcessStatusEnded, want: false},
		{name: "paused", status: ProcessStatusPaused, want: false},
		{name: "canceled", status: ProcessStatusCanceled, want: true},
		{name: "results", status: ProcessStatusResults, want: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.IsTerminal(); got != tc.want {
				t.Fatalf("IsTerminal() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestProcessIsActive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		process *Process
		want    bool
	}{
		{
			name: "ready and before deadline",
			process: &Process{
				Status:    ProcessStatusReady,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: true,
		},
		{
			name: "ready but past deadline",
			process: &Process{
				Status:    ProcessStatusReady,
				StartTime: now.Add(-2 * time.Hour),
				Duration:  time.Hour,
			},
			want: false,
		},
		{
			name: "ended",
			process: &Process{
				Status:    ProcessStatusEnded,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name: "paused and before deadline",
			process: &Process{
				Status:    ProcessStatusPaused,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: true,
		},
		{
			name: "canceled",
			process: &Process{
				Status:    ProcessStatusCanceled,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name: "results",
			process: &Process{
				Status:    ProcessStatusResults,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name:    "nil process",
			process: nil,
			want:    false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.process.IsActive(); got != tc.want {
				t.Fatalf("IsActive() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestProcessIsAcceptingVotes(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		process *Process
		want    bool
	}{
		{
			name: "ready within window",
			process: &Process{
				Status:    ProcessStatusReady,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: true,
		},
		{
			name: "ready before start time",
			process: &Process{
				Status:    ProcessStatusReady,
				StartTime: now.Add(time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name: "ready past deadline",
			process: &Process{
				Status:    ProcessStatusReady,
				StartTime: now.Add(-2 * time.Hour),
				Duration:  time.Hour,
			},
			want: false,
		},
		{
			name: "ended within window",
			process: &Process{
				Status:    ProcessStatusEnded,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name: "canceled within window",
			process: &Process{
				Status:    ProcessStatusCanceled,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name: "results within window",
			process: &Process{
				Status:    ProcessStatusResults,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name: "paused within window",
			process: &Process{
				Status:    ProcessStatusPaused,
				StartTime: now.Add(-time.Minute),
				Duration:  2 * time.Hour,
			},
			want: false,
		},
		{
			name:    "nil process",
			process: nil,
			want:    false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.process.IsAcceptingVotes(); got != tc.want {
				t.Fatalf("IsAcceptingVotes() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestProcessCBORDecodeIgnoresRemovedRegisteredForSequencingField(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()

	type legacyProcess struct {
		Status                  ProcessStatus `cbor:"1,keyasint,omitempty"`
		StartTime               time.Time     `cbor:"6,keyasint,omitempty"`
		Duration                time.Duration `cbor:"7,keyasint,omitempty"`
		RegisteredForSequencing bool          `cbor:"17,keyasint,omitempty"`
	}

	encoded, err := cbor.Marshal(legacyProcess{
		Status:                  ProcessStatusReady,
		StartTime:               now,
		Duration:                time.Hour,
		RegisteredForSequencing: true,
	})
	if err != nil {
		t.Fatalf("failed to marshal legacy process: %v", err)
	}

	var decoded Process
	if err := cbor.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to unmarshal legacy process: %v", err)
	}
	if decoded.Status != ProcessStatusReady {
		t.Fatalf("decoded Status = %v, want %v", decoded.Status, ProcessStatusReady)
	}
	if !decoded.StartTime.Equal(now) {
		t.Fatalf("decoded StartTime = %v, want %v", decoded.StartTime, now)
	}
	if decoded.Duration != time.Hour {
		t.Fatalf("decoded Duration = %v, want %v", decoded.Duration, time.Hour)
	}
}

func TestProcessMaxVotersReached(t *testing.T) {
	tests := []struct {
		name    string
		process *Process
		want    bool
	}{
		{
			name: "below limit",
			process: &Process{
				MaxVoters:   NewInt(3),
				VotersCount: NewInt(2),
			},
			want: false,
		},
		{
			name: "at limit",
			process: &Process{
				MaxVoters:   NewInt(3),
				VotersCount: NewInt(3),
			},
			want: true,
		},
		{
			name: "above limit",
			process: &Process{
				MaxVoters:   NewInt(3),
				VotersCount: NewInt(4),
			},
			want: true,
		},
		{
			name: "no voters yet",
			process: &Process{
				MaxVoters: NewInt(3),
			},
			want: false,
		},
		{
			name: "nil max voters with voters",
			process: &Process{
				VotersCount: NewInt(5),
			},
			want: false,
		},
		{
			name:    "nil process",
			process: nil,
			want:    false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.process.MaxVotersReached(); got != tc.want {
				t.Fatalf("MaxVotersReached() = %t, want %t", got, tc.want)
			}
		})
	}
}
