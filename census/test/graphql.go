package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/vocdoni/davinci-node/types"
)

var (
	DefaultExpectedRoot  = types.HexStringToHexBytesMustUnmarshal("0x0b3600e19a4f5017dea4f91f03d8aa0dd6f4c797795e7a5aa542e81b2c5a9485")
	DefaultGraphQLEvents = []TestWeightChangeEvent{
		{
			AccountID:      "0xdeb8699659be5d41a0e57e179d6cb42e00b9200c",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0xa2e4d94c5923a8dd99c5792a7b0436474c54e1e1",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0xb1f05b11ba3d892edd00f2e7689779e2b8841827",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0x74d8967e812de34702ecd3d453a44bf37440b10b",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0xf3b06b503652a5e075d423f97056dfde0c4b066f",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0x74d8967e812de34702ecd3d453a44bf37440b10b",
			PreviousWeight: "1",
			NewWeight:      "0",
		},
		{
			AccountID:      "0xdeb8699659be5d41a0e57e179d6cb42e00b9200c",
			PreviousWeight: "1",
			NewWeight:      "0",
		},
		{
			AccountID:      "0xb1f05b11ba3d892edd00f2e7689779e2b8841827",
			PreviousWeight: "1",
			NewWeight:      "2",
		},
		{
			AccountID:      "0x2aed14fe7bd056212cd0ed91b57a8ec5a5e33624",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0xb1f05b11ba3d892edd00f2e7689779e2b8841827",
			PreviousWeight: "2",
			NewWeight:      "1",
		},
		{
			AccountID:      "0xdeb8699659be5d41a0e57e179d6cb42e00b9200c",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
		{
			AccountID:      "0xea96b32c78afbd1b01c3346f2c42cc2d89655b8d",
			PreviousWeight: "0",
			NewWeight:      "1",
		},
	}
)

// TestWeightChangeEvent is the dataset you pass to the server.
// Ordering rule: slice index defines ordering.
type TestWeightChangeEvent struct {
	AccountID      string
	PreviousWeight string
	NewWeight      string
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]int `json:"variables"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   any            `json:"data,omitempty"`
	Errors []graphQLError `json:"errors,omitempty"`
}

type weightChangeEventsData struct {
	WeightChangeEvents []weightChangeEventOut `json:"weightChangeEvents"`
}

type weightChangeEventOut struct {
	Account        accountOut `json:"account"`
	PreviousWeight string     `json:"previousWeight"`
	NewWeight      string     `json:"newWeight"`
}

type accountOut struct {
	ID string `json:"id"`
}

// TestGraphQLServer is a minimal HTTP-only GraphQL test server.
type TestGraphQLServer struct {
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	srv    *httptest.Server

	baseURL string
	events  []TestWeightChangeEvent
}

// NewTestGraphQLServer creates a new server controller.
func NewTestGraphQLServer(ctx context.Context) *TestGraphQLServer {
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithCancel(ctx)

	return &TestGraphQLServer{
		ctx:    cctx,
		cancel: cancel,
	}
}

// SetEvents replaces the fixture dataset.
func (s *TestGraphQLServer) SetEvents(events []TestWeightChangeEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append([]TestWeightChangeEvent(nil), events...)
}

// Start starts the HTTP server.
// The GraphQL endpoint is the server base URL.
func (s *TestGraphQLServer) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleGraphQL)

	ts := httptest.NewServer(mux)
	s.srv = ts
	s.baseURL = ts.URL

	go func() {
		<-s.ctx.Done()
		s.Stop()
	}()
}

// Stop shuts down the server.
func (s *TestGraphQLServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}

	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
		s.baseURL = ""
	}
}

// HTTPEndpoint returns the GraphQL HTTP endpoint.
func (s *TestGraphQLServer) HTTPEndpoint() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.srv == nil {
		return "", errors.New("server not started")
	}
	return s.baseURL, nil
}

// GraphQLEndpoint returns the endpoint with "graphql://" scheme.
func (s *TestGraphQLServer) GraphQLEndpoint() (string, error) {
	httpURL, err := s.HTTPEndpoint()
	if err != nil {
		return "", err
	}

	u, err := url.Parse(httpURL)
	if err != nil {
		return "", err
	}

	if u.Scheme != "http" {
		return "", errors.New("unexpected URL scheme: " + u.Scheme)
	}

	u.Scheme = "graphql"
	return u.String(), nil
}

func (s *TestGraphQLServer) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGraphQLError(w, http.StatusMethodNotAllowed, "only POST is supported")
		return
	}

	var req graphQLRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeGraphQLError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if !strings.Contains(req.Query, "weightChangeEvents") {
		writeGraphQLError(w, http.StatusBadRequest, "unsupported query")
		return
	}

	first, skip, err := parseFirstSkip(req.Variables)
	if err != nil {
		writeGraphQLError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.RLock()
	eventsCopy := append([]TestWeightChangeEvent(nil), s.events...)
	s.mu.RUnlock()

	out := paginate(eventsCopy, first, skip)

	resp := graphQLResponse{
		Data: weightChangeEventsData{
			WeightChangeEvents: out,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseFirstSkip(vars map[string]int) (first int, skip int, err error) {
	f, ok := vars["first"]
	if !ok {
		return 0, 0, errors.New(`missing variable "first"`)
	}
	s, ok := vars["skip"]
	if !ok {
		return 0, 0, errors.New(`missing variable "skip"`)
	}
	if f < 0 || s < 0 {
		return 0, 0, errors.New(`variables must be non-negative`)
	}
	return f, s, nil
}

func paginate(events []TestWeightChangeEvent, first, skip int) []weightChangeEventOut {
	if skip >= len(events) {
		return []weightChangeEventOut{}
	}

	start := skip
	end := len(events)
	if start+first < end {
		end = start + first
	}

	out := make([]weightChangeEventOut, 0, end-start)
	for _, e := range events[start:end] {
		out = append(out, weightChangeEventOut{
			Account:        accountOut{ID: e.AccountID},
			PreviousWeight: e.PreviousWeight,
			NewWeight:      e.NewWeight,
		})
	}
	return out
}

func writeGraphQLError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(graphQLResponse{
		Errors: []graphQLError{{Message: msg}},
	})
}
