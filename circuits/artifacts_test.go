package circuits

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	qt "github.com/frankban/quicktest"
)

type failingWriteVerifyingKey struct {
	groth16.VerifyingKey
}

func (vk failingWriteVerifyingKey) WriteTo(_ io.Writer) (int64, error) {
	return 0, fmt.Errorf("write verifying key")
}

type artifactTestCircuit struct {
	X frontend.Variable `gnark:",public"`
	Y frontend.Variable
}

func (c *artifactTestCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(api.Add(c.X, 1), c.Y)
	return nil
}

func TestCircuitArtifactsLoadAllFromCacheCachesDecodedArtifacts(t *testing.T) {
	c := qt.New(t)

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &artifactTestCircuit{})
	c.Assert(err, qt.IsNil)

	pk, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)

	var ccsBuf bytes.Buffer
	_, err = ccs.WriteTo(&ccsBuf)
	c.Assert(err, qt.IsNil)

	var pkBuf bytes.Buffer
	_, err = pk.WriteTo(&pkBuf)
	c.Assert(err, qt.IsNil)

	var vkBuf bytes.Buffer
	_, err = vk.WriteTo(&vkBuf)
	c.Assert(err, qt.IsNil)

	oldBaseDir := BaseDir
	BaseDir = t.TempDir()
	t.Cleanup(func() {
		BaseDir = oldBaseDir
	})

	writeArtifact := func(content []byte) *Artifact {
		hash := sha256.Sum256(content)
		err := os.WriteFile(filepath.Join(BaseDir, hex.EncodeToString(hash[:])), content, 0o644)
		c.Assert(err, qt.IsNil)
		return &Artifact{Hash: hash[:]}
	}

	ca := NewCircuitArtifacts(
		"test",
		ecc.BN254,
		writeArtifact(ccsBuf.Bytes()),
		writeArtifact(pkBuf.Bytes()),
		writeArtifact(vkBuf.Bytes()),
	)

	err = ca.LoadAllFromCache()
	c.Assert(err, qt.IsNil)

	firstCCS, err := ca.CircuitDefinition()
	c.Assert(err, qt.IsNil)
	secondCCS, err := ca.CircuitDefinition()
	c.Assert(err, qt.IsNil)
	c.Assert(firstCCS, qt.Equals, secondCCS)

	firstPK, err := ca.ProvingKey()
	c.Assert(err, qt.IsNil)
	secondPK, err := ca.ProvingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(firstPK, qt.Equals, secondPK)

	firstVK, err := ca.VerifyingKey()
	c.Assert(err, qt.IsNil)
	secondVK, err := ca.VerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(firstVK, qt.Equals, secondVK)

	rawVK, err := ca.RawVerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(rawVK, qt.DeepEquals, vkBuf.Bytes())
}

func TestCircuitArtifactsEnsureVerifyingKeyDownloadsAndDecodes(t *testing.T) {
	c := qt.New(t)

	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &artifactTestCircuit{})
	c.Assert(err, qt.IsNil)

	_, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)

	var vkBuf bytes.Buffer
	_, err = vk.WriteTo(&vkBuf)
	c.Assert(err, qt.IsNil)

	oldBaseDir := BaseDir
	BaseDir = t.TempDir()
	t.Cleanup(func() {
		BaseDir = oldBaseDir
	})

	hash := sha256.Sum256(vkBuf.Bytes())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(vkBuf.Bytes())
	}))
	t.Cleanup(server.Close)

	ca := NewCircuitArtifacts(
		"test",
		ecc.BN254,
		nil,
		nil,
		&Artifact{
			RemoteURL: server.URL,
			Hash:      hash[:],
		},
	)

	err = ca.EnsureVerifyingKey(t.Context())
	c.Assert(err, qt.IsNil)

	gotVK, err := ca.VerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(gotVK, qt.Not(qt.IsNil))

	gotRawVK, err := ca.RawVerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(gotRawVK, qt.DeepEquals, vkBuf.Bytes())
}

func TestCircuitArtifactsRawVerifyingKeyWithoutDecodedVK(t *testing.T) {
	c := qt.New(t)

	rawVK := []byte(`{"protocol":"groth16","curve":"bn128"}`)

	oldBaseDir := BaseDir
	BaseDir = t.TempDir()
	t.Cleanup(func() {
		BaseDir = oldBaseDir
	})

	hash := sha256.Sum256(rawVK)
	err := os.WriteFile(filepath.Join(BaseDir, hex.EncodeToString(hash[:])), rawVK, 0o644)
	c.Assert(err, qt.IsNil)

	ca := NewCircuitArtifacts(
		"test",
		ecc.BN254,
		nil,
		nil,
		&Artifact{Hash: hash[:]},
	)

	gotRawVK, err := ca.RawVerifyingKey()
	c.Assert(err, qt.IsNil)
	c.Assert(gotRawVK, qt.DeepEquals, rawVK)
}

func TestCircuitArtifactsRawVerifyingKeyReturnsWriteError(t *testing.T) {
	c := qt.New(t)

	ca := &CircuitArtifacts{
		vk: failingWriteVerifyingKey{},
	}

	rawVK, err := ca.RawVerifyingKey()
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(err.Error(), qt.Contains, "write verifying key")
	c.Assert(rawVK, qt.IsNil)
}
