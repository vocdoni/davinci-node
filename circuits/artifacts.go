// Package circuits provides functionality for working with zero-knowledge proof circuits
// and their associated artifacts (circuit definitions, proving keys, and verification keys).
// It includes utilities for loading, downloading, and verifying the integrity of these artifacts.
package circuits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types"
)

// BaseDir is the path where the artifact cache is expected to be found. If the
// artifacts are not found there, they will be downloaded and stored. It can be
// set to a different path if needed from other packages. Defaults to the
// env var DAVINCI_ARTIFACTS_DIR or the user home directory.
var BaseDir string

func init() {
	if BaseDir == "" {
		if dir := os.Getenv("DAVINCI_ARTIFACTS_DIR"); dir != "" {
			BaseDir = dir
		} else {
			userHomeDir, err := os.UserHomeDir()
			if err != nil {
				userHomeDir = "."
			}
			BaseDir = filepath.Join(userHomeDir, ".davinci", "artifacts")
		}
	}
}

// Artifact describes a cached/downloadable circuit artifact by hash and source URL.
type Artifact struct {
	RemoteURL string
	Hash      []byte
}

// loadOrDownload returns the content of an artifact, loading it from cache or downloading it if not present.
func (k *Artifact) loadOrDownload(ctx context.Context) ([]byte, error) {
	if k == nil {
		return nil, fmt.Errorf("artifact not configured")
	}
	if len(k.Hash) == 0 {
		return nil, fmt.Errorf("artifact hash not provided")
	}

	if content, err := k.loadFromCache(); err == nil {
		return content, nil
	}

	return k.download(ctx)
}

func (k *Artifact) loadFromCache() ([]byte, error) {
	// append the name to the base directory and check if the file exists
	path := filepath.Join(BaseDir, hex.EncodeToString(k.Hash))

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %w", path, err)
	}

	// check if the hash of the content matches the expected hash
	startTime := time.Now()
	hasher := sha256.New()
	hasher.Write(content)
	fileHash := hasher.Sum(nil)
	if !bytes.Equal(fileHash, k.Hash) {
		log.Warnw("hash mismatch for cached artifact", "path", path)
		return nil, fmt.Errorf("hash mismatch for file %s: expected %x, got %x", path, k.Hash, fileHash)
	}

	log.DebugTime("artifact loaded from cache", startTime, "hash", hex.EncodeToString(k.Hash), "path", path)
	return content, nil
}

func (k *Artifact) download(ctx context.Context) ([]byte, error) {
	if k.RemoteURL == "" {
		return nil, fmt.Errorf("artifact not in cache and remote url not provided for hash %s", hex.EncodeToString(k.Hash))
	}

	log.Debugw("downloading artifact", "url", k.RemoteURL, "hash", hex.EncodeToString(k.Hash))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.RemoteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create file request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Errorw(err, "error closing body")
		}
	}()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected http status %s for %s", res.Status, k.RemoteURL)
	}

	// Read content into memory and hash it simultaneously
	var content bytes.Buffer
	hasher := sha256.New()
	mw := io.MultiWriter(&content, hasher)

	if _, err := io.Copy(mw, res.Body); err != nil {
		return nil, fmt.Errorf("failed to read downloaded content: %w", err)
	}

	// Verify hash
	computedHash := hasher.Sum(nil)
	if !bytes.Equal(computedHash, k.Hash) {
		return nil, fmt.Errorf("hash mismatch for downloaded artifact: expected %s, got %s", hex.EncodeToString(k.Hash), hex.EncodeToString(computedHash))
	}

	downloadedContent := content.Bytes()

	// 3. Store in cache
	path := filepath.Join(BaseDir, hex.EncodeToString(k.Hash))
	if err := os.MkdirAll(BaseDir, 0o755); err != nil {
		log.Warnw("failed to create base directory for caching, cannot store artifact", "dir", BaseDir, "error", err)
	} else if err := os.WriteFile(path, downloadedContent, 0o644); err != nil {
		log.Warnw("failed to cache downloaded artifact", "path", path, "error", err)
	}

	log.Debugw("artifact downloaded and cached", "path", path, "size_bytes", len(downloadedContent))

	return downloadedContent, nil
}

// CircuitArtifacts is a struct that holds the artifacts of a zkSNARK circuit
// (definition, proving and verification key). It provides a method to load the
// keys from the local cache or download them from the remote URLs provided.
type CircuitArtifacts struct {
	name              string
	curve             ecc.ID
	circuitDefinition *Artifact
	provingKey        *Artifact
	verifyingKey      *Artifact
}

func (ca *CircuitArtifacts) newConstraintSystem() constraint.ConstraintSystem {
	if types.UseGPUProver {
		return gpugroth16.NewCS(ca.curve)
	}
	return groth16.NewCS(ca.curve)
}

// NewProvingKey instantiates an empty proving key compatible with the selected
// backend. When GPU proving is enabled this returns an ICICLE proving key so
// that serialized keys can be read directly into GPU-ready structures.
func (ca *CircuitArtifacts) newProvingKey() groth16.ProvingKey {
	if types.UseGPUProver {
		return gpugroth16.NewProvingKey(ca.curve)
	}
	return groth16.NewProvingKey(ca.curve)
}

func (ca *CircuitArtifacts) newVerifyingKey() groth16.VerifyingKey {
	if types.UseGPUProver {
		return gpugroth16.NewVerifyingKey(ca.curve)
	}
	return groth16.NewVerifyingKey(ca.curve)
}

// NewCircuitArtifacts creates a new CircuitArtifacts struct with the circuit
// artifacts provided. It returns the struct with the artifacts set.
func NewCircuitArtifacts(name string, curve ecc.ID, circuit, provingKey, verifyingKey *Artifact) *CircuitArtifacts {
	return &CircuitArtifacts{
		name:              name,
		curve:             curve,
		circuitDefinition: circuit,
		provingKey:        provingKey,
		verifyingKey:      verifyingKey,
	}
}

// Name returns the logical circuit name associated with these artifacts.
func (ca *CircuitArtifacts) Name() string {
	if ca == nil {
		return ""
	}
	return ca.name
}

// Download ensures all artifacts are available, downloading them if necessary.
func (ca *CircuitArtifacts) Download(ctx context.Context) error {
	_, err := ca.LoadOrDownload(ctx)
	return err
}

// LoadOrDownload ensures all artifacts are available, downloading them if necessary,
// and returns a ready-to-use CircuitRuntime.
func (ca *CircuitArtifacts) LoadOrDownload(ctx context.Context) (cr *CircuitRuntime, err error) {
	log.Debugw("loading circuit artifacts", "circuit", ca.Name())
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("circuit runtime ready", startTime, "circuit", cr.Name())
		}
	}()

	ccs := ca.newConstraintSystem()
	pk := ca.newProvingKey()
	vk := ca.newVerifyingKey()

	if ca.circuitDefinition != nil {
		stepStart := time.Now()
		content, err := ca.circuitDefinition.loadOrDownload(ctx)
		if err != nil {
			return nil, fmt.Errorf("load circuit definition: %w", err)
		}
		if _, err := ccs.ReadFrom(bytes.NewReader(content)); err != nil {
			return nil, fmt.Errorf("decode circuit definition: %w", err)
		}
		log.DebugTime("circuit definition ready", stepStart, "circuit", ca.name)
	}

	if ca.provingKey != nil {
		stepStart := time.Now()
		content, err := ca.provingKey.loadOrDownload(ctx)
		if err != nil {
			return nil, fmt.Errorf("load proving key: %w", err)
		}
		if _, err := pk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
			return nil, fmt.Errorf("decode proving key: %w", err)
		}
		log.DebugTime("proving key ready", stepStart, "circuit", ca.name)
	}

	if ca.verifyingKey != nil {
		stepStart := time.Now()
		content, err := ca.verifyingKey.loadOrDownload(ctx)
		if err != nil {
			return nil, fmt.Errorf("load verifying key: %w", err)
		}
		if _, err := vk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
			return nil, fmt.Errorf("decode verifying key: %w", err)
		}
		log.DebugTime("verifying key ready", stepStart, "circuit", ca.name)
	}

	return NewCircuitRuntime(ca.name, ca.curve, ccs, pk, vk, nil, nil), nil
}

// LoadOrDownloadVerifyingKey downloads any missing verifying key artifact and decodes it into memory.
func (ca *CircuitArtifacts) LoadOrDownloadVerifyingKey(ctx context.Context) (groth16.VerifyingKey, error) {
	if ca.verifyingKey == nil {
		return nil, fmt.Errorf("verifying key not configured")
	}

	content, err := ca.verifyingKey.loadOrDownload(ctx)
	if err != nil {
		return nil, fmt.Errorf("ensure verifying key: %w", err)
	}

	vk := ca.newVerifyingKey()
	if _, err := vk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("decode verifying key: %w", err)
	}
	return vk, nil
}

// Curve returns the elliptic curve identifier associated with this artifact set.
func (ca *CircuitArtifacts) Curve() ecc.ID {
	return ca.curve
}

// CircuitHash returns the circuit-definition hash.
func (ca *CircuitArtifacts) CircuitHash() []byte {
	if ca.circuitDefinition == nil {
		return nil
	}
	return ca.circuitDefinition.Hash
}

// ProvingKeyHash returns the proving-key hash.
func (ca *CircuitArtifacts) ProvingKeyHash() []byte {
	if ca.provingKey == nil {
		return nil
	}
	return ca.provingKey.Hash
}

// VerifyingKeyHash returns the verifying-key hash.
func (ca *CircuitArtifacts) VerifyingKeyHash() []byte {
	if ca.verifyingKey == nil {
		return nil
	}
	return ca.verifyingKey.Hash
}

// RawVerifyingKey returns the content of the verifying key as types.HexBytes.
// It returns an error if the verifying key is not locally available or cannot be serialized.
func (ca *CircuitArtifacts) RawVerifyingKey() ([]byte, error) {
	// Cannot guarantee context is available here, so we load from cache only.
	// The caller should have called LoadOrDownloadVerifyingKey previously if remote fetching was desired.
	content, err := ca.verifyingKey.loadFromCache()
	if err != nil {
		return nil, fmt.Errorf("load verifying key: %w", err)
	}
	return content, nil
}

// CircuitRuntime is a fully initialized runtime view of a circuit's decoded artifacts.
// Once constructed, its getters are infallible.
type CircuitRuntime struct {
	name         string
	curve        ecc.ID
	ccs          constraint.ConstraintSystem
	pk           groth16.ProvingKey
	vk           groth16.VerifyingKey
	proverOpts   []backend.ProverOption
	verifierOpts []backend.VerifierOption
}

// NewCircuitRuntime constructs a runtime from already-decoded artifacts.
func NewCircuitRuntime(name string, curve ecc.ID, ccs constraint.ConstraintSystem, pk groth16.ProvingKey, vk groth16.VerifyingKey,
	proverOpts []backend.ProverOption, verifierOpts []backend.VerifierOption,
) *CircuitRuntime {
	return &CircuitRuntime{
		name: name, curve: curve, ccs: ccs, pk: pk, vk: vk,
		proverOpts: proverOpts, verifierOpts: verifierOpts,
	}
}

// Name returns the circuit name associated with this runtime.
func (cr *CircuitRuntime) Name() string { return cr.name }

// Curve returns the elliptic curve identifier associated with this runtime.
func (cr *CircuitRuntime) Curve() ecc.ID { return cr.curve }

// ConstraintSystem returns the decoded constraint system.
func (cr *CircuitRuntime) ConstraintSystem() constraint.ConstraintSystem { return cr.ccs }

// ProvingKey returns the decoded proving key.
func (cr *CircuitRuntime) ProvingKey() groth16.ProvingKey { return cr.pk }

// VerifyingKey returns the decoded verifying key.
func (cr *CircuitRuntime) VerifyingKey() groth16.VerifyingKey { return cr.vk }

// ProveAndVerify generates a proof from the assignment and verifies it immediately.
func (cr *CircuitRuntime) ProveAndVerify(assignment frontend.Circuit) (groth16.Proof, error) {
	proof, err := cr.Prove(assignment)
	if err != nil {
		return nil, err
	}
	if err := cr.Verify(proof, assignment); err != nil {
		return nil, err
	}
	return proof, nil
}

// ProveAndVerifyWithWitness generates a proof from a full witness and verifies it immediately.
func (cr *CircuitRuntime) ProveAndVerifyWithWitness(fullWitness witness.Witness) (groth16.Proof, error) {
	proof, err := cr.ProveWithWitness(fullWitness)
	if err != nil {
		return nil, err
	}
	if err := cr.VerifyWithWitness(proof, fullWitness); err != nil {
		return nil, err
	}
	return proof, nil
}

// Prove generates a proof from the assignment.
func (cr *CircuitRuntime) Prove(assignment frontend.Circuit) (proof groth16.Proof, err error) {
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("proof generated", startTime, "circuit", cr.Name())
		}
	}()

	return prover.Prove(cr.curve, cr.ccs, cr.pk, assignment, cr.proverOpts...)
}

// ProveWithWitness generates a proof from a full witness.
func (cr *CircuitRuntime) ProveWithWitness(fullWitness witness.Witness) (proof groth16.Proof, err error) {
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("proof generated", startTime, "circuit", cr.Name())
		}
	}()

	return prover.ProveWithWitness(cr.curve, cr.ccs, cr.pk, fullWitness, cr.proverOpts...)
}

// Verify builds a public witness from the public assignment and verifies the proof.
func (cr *CircuitRuntime) Verify(proof groth16.Proof, publicAssignment frontend.Circuit) (err error) {
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("proof verified", startTime, "circuit", cr.Name())
		}
	}()

	publicWitness, err := frontend.NewWitness(publicAssignment, cr.curve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("create public witness: %w", err)
	}
	return cr.VerifyWithWitness(proof, publicWitness)
}

// VerifyWithWitness derives the public witness from a witness (either full or already public) and verifies the proof.
func (cr *CircuitRuntime) VerifyWithWitness(proof groth16.Proof, witness witness.Witness) (err error) {
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("proof verified", startTime, "circuit", cr.Name())
		}
	}()

	publicWitness, err := witness.Public()
	if err != nil {
		return fmt.Errorf("extract public witness: %w", err)
	}
	return groth16.Verify(proof, cr.vk, publicWitness, cr.verifierOpts...)
}
