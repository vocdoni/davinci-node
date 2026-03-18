package circuits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types"
)

func TestCircuitRuntime(t *testing.T) {
	artifacts := NewCircuitArtifacts("test", ecc.BN254, nil, nil, nil, nil, nil)
	qt.Assert(t, artifacts.Name(), qt.Equals, "test")
	qt.Assert(t, artifacts.Curve(), qt.Equals, ecc.BN254)

	runtime := NewCircuitRuntime("test", ecc.BN254, nil, nil, nil, nil, nil)
	qt.Assert(t, runtime.Name(), qt.Equals, "test")
	qt.Assert(t, runtime.Curve(), qt.Equals, ecc.BN254)
	qt.Assert(t, runtime.ProvingKey(), qt.IsNil)
}

type artifactsTestCircuit struct {
	X frontend.Variable `gnark:",public"`
	Y frontend.Variable `gnark:",secret"`
}

func (c *artifactsTestCircuit) Define(api frontend.API) error {
	api.AssertIsEqual(c.X, c.Y)
	return nil
}

func compileArtifactsTestCircuit(t *testing.T, circuit frontend.Circuit) constraint.ConstraintSystem {
	t.Helper()
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, circuit)
	qt.Assert(t, err, qt.IsNil)
	return ccs
}

func writeArtifactFile(t *testing.T, dir string, writeFn func(*bytes.Buffer) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := writeFn(&buf)
	qt.Assert(t, err, qt.IsNil)

	hasher := sha256.New()
	_, err = hasher.Write(buf.Bytes())
	qt.Assert(t, err, qt.IsNil)
	hash := hex.EncodeToString(hasher.Sum(nil))

	err = os.WriteFile(filepath.Join(dir, hash), buf.Bytes(), 0o644)
	qt.Assert(t, err, qt.IsNil)

	return types.HexStringToHexBytesMustUnmarshal(hash)
}

func marshalVerifyingKey(t *testing.T, vk interface {
	WriteTo(io.Writer) (int64, error)
},
) []byte {
	t.Helper()
	var buf bytes.Buffer
	_, err := vk.WriteTo(&buf)
	qt.Assert(t, err, qt.IsNil)
	return buf.Bytes()
}

func TestMatches(t *testing.T) {
	oldBaseDir := BaseDir
	BaseDir = t.TempDir()
	defer func() { BaseDir = oldBaseDir }()

	matchingCCS := compileArtifactsTestCircuit(t, &artifactsTestCircuit{})

	circuitHash := writeArtifactFile(t, BaseDir, func(buf *bytes.Buffer) error {
		_, err := matchingCCS.WriteTo(buf)
		return err
	})

	artifacts := NewCircuitArtifacts(
		"test",
		ecc.BN254,
		nil,
		nil,
		&Artifact{Hash: circuitHash},
		nil,
		nil,
	)

	matches, err := artifacts.Matches(matchingCCS)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, matches, qt.IsTrue)

	mismatchingCCS := compileArtifactsTestCircuit(t, &struct {
		artifactsTestCircuit
		Z frontend.Variable `gnark:",secret"`
	}{})
	matches, err = artifacts.Matches(mismatchingCCS)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, matches, qt.IsFalse)
}

func TestSetup(t *testing.T) {
	ccs := compileArtifactsTestCircuit(t, &artifactsTestCircuit{})
	artifacts := NewCircuitArtifacts("test", ecc.BN254, nil, nil, nil, nil, nil)

	runtime, err := artifacts.Setup(ccs)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, runtime, qt.Not(qt.IsNil))
}

func TestLoadOrSetupForCircuitUsesArtifactsWhenMatching(t *testing.T) {
	oldBaseDir := BaseDir
	BaseDir = t.TempDir()
	defer func() { BaseDir = oldBaseDir }()

	placeholder := &artifactsTestCircuit{}
	ccs := compileArtifactsTestCircuit(t, placeholder)
	pk, vk, err := prover.Setup(ccs)
	qt.Assert(t, err, qt.IsNil)

	circuitHash := writeArtifactFile(t, BaseDir, func(buf *bytes.Buffer) error {
		_, err := ccs.WriteTo(buf)
		return err
	})
	provingKeyHash := writeArtifactFile(t, BaseDir, func(buf *bytes.Buffer) error {
		_, err := pk.WriteTo(buf)
		return err
	})
	verifyingKeyHash := writeArtifactFile(t, BaseDir, func(buf *bytes.Buffer) error {
		_, err := vk.WriteTo(buf)
		return err
	})

	expectedVK := marshalVerifyingKey(t, vk)

	artifacts := NewCircuitArtifacts(
		"test",
		ecc.BN254,
		nil,
		nil,
		&Artifact{Hash: circuitHash},
		&Artifact{Hash: provingKeyHash},
		&Artifact{Hash: verifyingKeyHash},
	)

	runtime, err := artifacts.LoadOrSetupForCircuit(context.Background(), placeholder)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, runtime, qt.Not(qt.IsNil))
	qt.Assert(t, marshalVerifyingKey(t, runtime.VerifyingKey()), qt.DeepEquals, expectedVK)
}

func TestLoadOrSetupForCircuitFallsBackToSetupOnMismatch(t *testing.T) {
	placeholder := &artifactsTestCircuit{}
	mismatchingPlaceholder := &struct {
		artifactsTestCircuit
		Z frontend.Variable `gnark:",secret"`
	}{}

	mismatchingCCS := compileArtifactsTestCircuit(t, mismatchingPlaceholder)
	hasher := sha256.New()
	_, err := mismatchingCCS.WriteTo(hasher)
	qt.Assert(t, err, qt.IsNil)
	mismatchHash := types.HexStringToHexBytesMustUnmarshal(hex.EncodeToString(hasher.Sum(nil)))

	artifacts := NewCircuitArtifacts(
		"test",
		ecc.BN254,
		nil,
		nil,
		&Artifact{Hash: mismatchHash},
		nil,
		nil,
	)

	runtime, err := artifacts.LoadOrSetupForCircuit(context.Background(), placeholder)
	qt.Assert(t, err, qt.IsNil)
	qt.Assert(t, runtime, qt.Not(qt.IsNil))
	qt.Assert(t, runtime.VerifyingKey(), qt.Not(qt.IsNil))
}
