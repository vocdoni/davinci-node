package census

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
	leanimt "github.com/vocdoni/lean-imt-go"
	leancensus "github.com/vocdoni/lean-imt-go/census"
)

type testErrReader struct {
	err error
}

func (r *testErrReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

type testReadCloser struct {
	io.Reader
	closeErr error
}

func (rc *testReadCloser) Close() error {
	return rc.closeErr
}

var testDumpNonce atomic.Uint64

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testMakeImportAllDumpJSON(c *qt.C) ([]byte, types.HexBytes) {
	c.Helper()

	tree, err := leancensus.NewCensusIMT(nil, leanimt.PoseidonHasher)
	c.Assert(err, qt.IsNil)

	nonce := testDumpNonce.Add(1)
	addr := common.BigToAddress(new(big.Int).SetUint64(nonce))
	weight := new(big.Int).Add(big.NewInt(1), new(big.Int).SetUint64(nonce))
	c.Assert(tree.Add(addr, weight), qt.IsNil)

	dump, err := tree.DumpAll()
	c.Assert(err, qt.IsNil)

	dumpJSON, err := json.Marshal(dump)
	c.Assert(err, qt.IsNil)

	return dumpJSON, types.HexBytes(dump.Root.Bytes())
}

func testMakeImportJSONL(c *qt.C) (io.Reader, types.HexBytes) {
	c.Helper()

	participants := []leancensus.CensusParticipant{
		{
			Index:   1,
			Address: testutil.RandomAddress(),
			Weight:  big.NewInt(2),
		},
		{
			Index:   0,
			Address: testutil.RandomAddress(),
			Weight:  big.NewInt(1),
		},
	}

	// Compute expected root for indices 0 and 1.
	tree, err := leancensus.NewCensusIMT(nil, leanimt.PoseidonHasher)
	c.Assert(err, qt.IsNil)
	c.Assert(tree.Add(participants[1].Address, participants[1].Weight), qt.IsNil)
	c.Assert(tree.Add(participants[0].Address, participants[0].Weight), qt.IsNil)
	root, ok := tree.Root()
	c.Assert(ok, qt.IsTrue)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, p := range participants {
		c.Assert(enc.Encode(p), qt.IsNil)
	}

	return bytes.NewReader(buf.Bytes()), types.HexBytes(root.Bytes())
}

func TestJSONFormatString(t *testing.T) {
	c := qt.New(t)

	c.Assert(JSONL.String(), qt.Equals, "jsonl")
	c.Assert(JSONArray.String(), qt.Equals, "json")
	c.Assert(UnknownJSON.String(), qt.Equals, "unknown")
	c.Assert(JSONFormat(99).String(), qt.Equals, "unknown")
}

func TestJSONImporterValidURI(t *testing.T) {
	c := qt.New(t)
	ji := JSONImporter()
	c.Assert(ji.ValidURI("http://example.com/dump"), qt.IsTrue)
	c.Assert(ji.ValidURI("https://example.com/dump"), qt.IsTrue)
	c.Assert(ji.ValidURI("ftp://example.com/dump"), qt.IsFalse)
	c.Assert(ji.ValidURI("file:///tmp/dump"), qt.IsFalse)
	c.Assert(ji.ValidURI("HTTPS://example.com/dump"), qt.IsFalse)
}

func TestRequestRawDump(t *testing.T) {
	c := qt.New(t)

	c.Run("SetsAcceptHeader", func(c *qt.C) {
		oldTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			c.Assert(r.Method, qt.Equals, http.MethodGet)
			c.Assert(r.Header.Get("Accept"), qt.Equals, "application/x-ndjson, application/json;q=0.9, */*;q=0.1")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/x-ndjson"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}` + "\n")),
				Request:    r,
			}, nil
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		res, err := requestRawDump(c.Context(), "https://example.invalid/census")
		c.Assert(err, qt.IsNil)
		c.Assert(res.StatusCode, qt.Equals, http.StatusOK)
		c.Cleanup(func() {
			c.Assert(res.Body.Close(), qt.IsNil)
		})

		body, err := io.ReadAll(res.Body)
		c.Assert(err, qt.IsNil)
		c.Assert(string(body), qt.Equals, `{"ok":true}`+"\n")
	})

	c.Run("Non200IncludesBody", func(c *qt.C) {
		oldTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     http.Header{},
				Body:       io.NopCloser(bytes.NewBufferString("nope")),
				Request:    r,
			}, nil
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		res, err := requestRawDump(c.Context(), "https://example.invalid/missing")
		c.Assert(res, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "status code 404")
		c.Assert(err.Error(), qt.Contains, "body: nope")
	})

	c.Run("RequestCreationError", func(c *qt.C) {
		res, err := requestRawDump(c.Context(), "http://[::1")
		c.Assert(res, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to create HTTP request")
	})

	c.Run("DoError", func(c *qt.C) {
		oldTransport := http.DefaultTransport
		doErr := fmt.Errorf("dial failed")
		http.DefaultTransport = roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, doErr
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		res, err := requestRawDump(c.Context(), "https://example.invalid/fail")
		c.Assert(res, qt.IsNil)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to download JSON dump")
		c.Assert(err.Error(), qt.Contains, doErr.Error())
	})
}

func TestJSONReader(t *testing.T) {
	c := qt.New(t)

	c.Run("ContentTypeJSONL", func(c *qt.C) {
		const body = `{"a":1}` + "\n"
		res := &http.Response{
			Header: http.Header{"Content-Type": []string{"application/x-ndjson"}},
			Body:   io.NopCloser(strings.NewReader(body)),
		}

		reader, format, err := jsonReader(res)
		c.Assert(err, qt.IsNil)
		c.Assert(format, qt.Equals, JSONL)

		got, err := io.ReadAll(reader)
		c.Assert(err, qt.IsNil)
		c.Assert(string(got), qt.Equals, body)
	})

	c.Run("ContentTypeJSONArray", func(c *qt.C) {
		const body = `[{"a":1}]`
		res := &http.Response{
			Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:   io.NopCloser(strings.NewReader(body)),
		}

		reader, format, err := jsonReader(res)
		c.Assert(err, qt.IsNil)
		c.Assert(format, qt.Equals, JSONArray)

		got, err := io.ReadAll(reader)
		c.Assert(err, qt.IsNil)
		c.Assert(string(got), qt.Equals, body)
	})

	c.Run("ContentBasedDetectsJSONL", func(c *qt.C) {
		const body = `{"a":1}` + "\n" + `{"b":2}` + "\n"
		res := &http.Response{
			Header: http.Header{},
			Body:   io.NopCloser(strings.NewReader(body)),
		}

		reader, format, err := jsonReader(res)
		c.Assert(err, qt.IsNil)
		c.Assert(format, qt.Equals, JSONL)

		got, err := io.ReadAll(reader)
		c.Assert(err, qt.IsNil)
		c.Assert(string(got), qt.Equals, body)
	})

	c.Run("ContentBasedDetectsJSONArray", func(c *qt.C) {
		const body = `[{"a":1},{"b":2}]`
		res := &http.Response{
			Header: http.Header{},
			Body:   io.NopCloser(strings.NewReader(body)),
		}

		reader, format, err := jsonReader(res)
		c.Assert(err, qt.IsNil)
		c.Assert(format, qt.Equals, JSONArray)

		got, err := io.ReadAll(reader)
		c.Assert(err, qt.IsNil)
		c.Assert(string(got), qt.Equals, body)
	})

	c.Run("InvalidJSON", func(c *qt.C) {
		const body = "not json"
		res := &http.Response{
			Header: http.Header{},
			Body:   io.NopCloser(strings.NewReader(body)),
		}

		reader, format, err := jsonReader(res)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(format, qt.Equals, UnknownJSON)

		got, readErr := io.ReadAll(reader)
		c.Assert(readErr, qt.IsNil)
		c.Assert(string(got), qt.Equals, body)
	})
}

func TestImportJSONDump(t *testing.T) {
	c := qt.New(t)

	c.Run("UnknownFormat", func(c *qt.C) {
		expectedRoot := types.HexBytes{0x01, 0x02}
		err := importJSONDump(nil, UnknownJSON, expectedRoot, strings.NewReader(""))
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "unknown JSON format: unknown")
	})

	c.Run("JSONArrayReadError", func(c *qt.C) {
		expectedRoot := types.HexBytes{0x01}
		readErr := fmt.Errorf("read failure")
		err := importJSONDump(nil, JSONArray, expectedRoot, &testErrReader{err: readErr})
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to read json census dump")
		c.Assert(err.Error(), qt.Contains, expectedRoot.String())
		c.Assert(err.Error(), qt.Contains, readErr.Error())
	})

	c.Run("JSONLSuccess", func(c *qt.C) {
		censusDB := testNewCensusDB(c)
		reader, expectedRoot := testMakeImportJSONL(c)

		err := importJSONDump(censusDB, JSONL, expectedRoot, reader)
		c.Assert(err, qt.IsNil)
	})

	c.Run("JSONArraySuccess", func(c *qt.C) {
		censusDB := testNewCensusDB(c)
		dumpJSON, expectedRoot := testMakeImportAllDumpJSON(c)

		err := importJSONDump(censusDB, JSONArray, expectedRoot, bytes.NewReader(dumpJSON))
		c.Assert(err, qt.IsNil)
	})

	c.Run("JSONLImportError", func(c *qt.C) {
		censusDB := testNewCensusDB(c)
		expectedRoot := types.HexBytes{0x01}
		err := importJSONDump(censusDB, JSONL, expectedRoot, strings.NewReader("not json\n"))
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to import jsonl census dump")
		c.Assert(err.Error(), qt.Contains, expectedRoot.String())
	})

	c.Run("JSONArrayImportError", func(c *qt.C) {
		censusDB := testNewCensusDB(c)
		expectedRoot := types.HexBytes{0x01}
		err := importJSONDump(censusDB, JSONArray, expectedRoot, strings.NewReader("{not-json"))
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to import json census dump")
		c.Assert(err.Error(), qt.Contains, expectedRoot.String())
	})

	c.Run("JSONArrayRootMismatch", func(c *qt.C) {
		censusDB := testNewCensusDB(c)
		dumpJSON, expectedRoot := testMakeImportAllDumpJSON(c)

		mismatchedRoot := make(types.HexBytes, len(expectedRoot))
		copy(mismatchedRoot, expectedRoot)
		mismatchedRoot[0] ^= 0xff

		err := importJSONDump(censusDB, JSONArray, mismatchedRoot, bytes.NewReader(dumpJSON))
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "imported census root mismatch")
	})
}

func TestJSONDownloadAndImportCensus(t *testing.T) {
	c := qt.New(t)

	censusDB := testNewCensusDB(c)
	expectedRoot := types.HexBytes{0x01}
	ji := JSONImporter()

	c.Run("Success", func(c *qt.C) {
		dumpJSON, expectedRoot := testMakeImportAllDumpJSON(c)

		oldTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: &testReadCloser{
					Reader:   bytes.NewReader(dumpJSON),
					closeErr: fmt.Errorf("close error"),
				},
				Request: r,
			}, nil
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		err := ji.DownloadAndImportCensus(c.Context(), censusDB, "https://example.invalid/dump.json", expectedRoot)
		c.Assert(err, qt.IsNil)
	})

	c.Run("DownloadErrorWrapped", func(c *qt.C) {
		oldTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("bad gateway")),
				Request:    r,
			}, nil
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		err := ji.DownloadAndImportCensus(c.Context(), censusDB, "https://example.invalid/dump", expectedRoot)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to download JSON dump")
	})

	c.Run("JSONReaderErrorWrapped", func(c *qt.C) {
		oldTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{}, // force content-based detection
				Body:       io.NopCloser(strings.NewReader("not json")),
				Request:    r,
			}, nil
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		err := ji.DownloadAndImportCensus(c.Context(), censusDB, "https://example.invalid/dump", expectedRoot)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to download census merkle tree")
	})

	c.Run("ImportErrorWrapped", func(c *qt.C) {
		oldTransport := http.DefaultTransport
		http.DefaultTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/x-ndjson"}},
				Body:       io.NopCloser(strings.NewReader("not json\n")),
				Request:    r,
			}, nil
		})
		c.Cleanup(func() { http.DefaultTransport = oldTransport })

		err := ji.DownloadAndImportCensus(c.Context(), censusDB, "https://example.invalid/dump.jsonl", expectedRoot)
		c.Assert(err, qt.Not(qt.IsNil))
		c.Assert(err.Error(), qt.Contains, "failed to import census merkle tree")
	})
}
