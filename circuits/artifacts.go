// Package circuits provides functionality for working with zero-knowledge proof circuits
// and their associated artifacts (circuit definitions, proving keys, and verification keys).
// It includes utilities for loading, downloading, and verifying the integrity of these artifacts.
package circuits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
)

// BaseDir is the path where the artifact cache is expected to be found. If the
// artifacts are not found there, they will be downloaded and stored. It can be
// set to a different path if needed from other packages. Defaults to the
// env var DAVINCI_ARTIFACTS_DIR or the user home directory.
var BaseDir string

var (
	ErrArtifactNotFound     = errors.New("artifact not found in cache")
	ErrArtifactHashMismatch = errors.New("artifact hash mismatch")
)

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

func (k *Artifact) cachePath() (string, error) {
	if k == nil {
		return "", fmt.Errorf("artifact not configured")
	}
	if len(k.Hash) == 0 {
		return "", fmt.Errorf("artifact hash not provided")
	}
	return filepath.Join(BaseDir, hex.EncodeToString(k.Hash)), nil
}

// loadOrDownload returns the content of an artifact, loading it from cache or downloading it if not present.
// It re-downloads only on cache miss or cached artifact hash mismatch.
func (k *Artifact) loadOrDownload(ctx context.Context) ([]byte, error) {
	if k == nil {
		return nil, fmt.Errorf("artifact not configured")
	}
	if len(k.Hash) == 0 {
		return nil, fmt.Errorf("artifact hash not provided")
	}

	content, err := k.loadFromCache()
	switch {
	case err == nil:
		return content, nil
	case errors.Is(err, ErrArtifactNotFound) || errors.Is(err, ErrArtifactHashMismatch):
		if err := k.downloadToCache(ctx); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	return k.readFromCache()
}

// loadFromCache reads artifact from cache and verifies hash matches.
func (k *Artifact) loadFromCache() ([]byte, error) {
	content, err := k.readFromCache()
	if err != nil {
		return nil, err
	}
	return content, k.checkHash(content)
}

func (k *Artifact) readFromCache() ([]byte, error) {
	path, err := k.cachePath()
	if err != nil {
		return nil, err
	}
	startTime := time.Now()
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("error reading file %s: %w", path, err)
	}
	log.DebugTime("artifact read from cache", startTime, "path", path)
	return content, nil
}

func (k *Artifact) checkHash(content []byte) error {
	startTime := time.Now()
	sum := sha256.Sum256(content)
	if !bytes.Equal(sum[:], k.Hash) {
		log.Warnw("hash mismatch for cached artifact", "expected", hex.EncodeToString(k.Hash), "got", hex.EncodeToString(sum[:]))
		return fmt.Errorf("%w: expected %x, got %x", ErrArtifactHashMismatch, k.Hash, sum[:])
	}
	log.DebugTime("artifact hash verified", startTime, "hash", hex.EncodeToString(k.Hash))

	return nil
}

func (k *Artifact) checkFileHash() error {
	path, err := k.cachePath()
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrArtifactNotFound
		}
		return fmt.Errorf("open cached artifact %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	startTime := time.Now()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("hash cached artifact %s: %w", path, err)
	}
	sum := hasher.Sum(nil)
	if !bytes.Equal(sum, k.Hash) {
		log.Warnw("hash mismatch for cached artifact",
			"path", path, "expected", hex.EncodeToString(k.Hash), "got", hex.EncodeToString(sum),
		)
		return fmt.Errorf("%w: expected %x, got %x", ErrArtifactHashMismatch, k.Hash, sum)
	}
	log.DebugTime("artifact hash verified", startTime, "hash", hex.EncodeToString(k.Hash))

	return nil
}

func (k *Artifact) ensureDownloaded(ctx context.Context) error {
	err := k.checkFileHash()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrArtifactNotFound) || errors.Is(err, ErrArtifactHashMismatch):
		return k.downloadToCache(ctx)
	default:
		return err
	}
}

func (k *Artifact) downloadToCache(ctx context.Context) error {
	path, err := k.cachePath()
	if err != nil {
		return err
	}
	if k.RemoteURL == "" {
		return fmt.Errorf("artifact remote url not provided for hash %s", hex.EncodeToString(k.Hash))
	}

	log.Debugw("downloading artifact", "url", k.RemoteURL, "hash", hex.EncodeToString(k.Hash))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.RemoteURL, nil)
	if err != nil {
		return fmt.Errorf("create file request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Errorw(err, "error closing body")
		}
	}()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected http status %s for %s", res.Status, k.RemoteURL)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create base directory for caching %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, hex.EncodeToString(k.Hash)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp artifact file: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			if err := tmpFile.Close(); err != nil {
				log.Errorw(err, fmt.Sprintf("error closing temp artifact file %q", tmpFile.Name()))
			}
		}
		if tmpPath != "" {
			if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
				log.Errorw(err, fmt.Sprintf("error removing temp artifact file %q", tmpPath))
			}
		}
	}()

	hasher := sha256.New()

	size, err := io.Copy(io.MultiWriter(tmpFile, hasher), res.Body)
	if err != nil {
		return fmt.Errorf("failed to read downloaded content: %w", err)
	}

	computedHash := hasher.Sum(nil)
	if !bytes.Equal(computedHash, k.Hash) {
		return fmt.Errorf("hash mismatch for downloaded artifact: expected %s, got %s",
			hex.EncodeToString(k.Hash),
			hex.EncodeToString(computedHash),
		)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp cache file %s: %w", tmpPath, err)
	}
	tmpFile = nil

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install cached artifact %s: %w", path, err)
	}
	tmpPath = ""
	log.Debugw("artifact downloaded and cached", "path", path, "size_bytes", size)
	return nil
}

type circuitParams struct {
	name         string
	curve        ecc.ID
	proverOpts   []backend.ProverOption
	verifierOpts []backend.VerifierOption
}

// Name returns the logical name associated with this circuit.
func (c circuitParams) Name() string { return c.name }

// Curve returns the elliptic curve identifier associated with this circuit.
func (c circuitParams) Curve() ecc.ID { return c.curve }

// ProverOptions returns the prover options associated with this circuit.
func (c circuitParams) ProverOptions() []backend.ProverOption { return c.proverOpts }

// VerifierOptions returns the verifier options associated with this circuit.
func (c circuitParams) VerifierOptions() []backend.VerifierOption { return c.verifierOpts }

// CircuitArtifacts is a struct that holds the artifacts of a zkSNARK circuit
// (definition, proving and verification key). It provides a method to load the
// keys from the local cache or download them from the remote URLs provided.
type CircuitArtifacts struct {
	circuitParams
	circuitDefinition *Artifact
	provingKey        *Artifact
	verifyingKey      *Artifact
}

// NewCircuitArtifacts creates a new CircuitArtifacts struct with the circuit
// artifacts provided. It returns the struct with the artifacts set.
func NewCircuitArtifacts(name string, curve ecc.ID, proverOpts []backend.ProverOption, verifierOpts []backend.VerifierOption,
	circuit, provingKey, verifyingKey *Artifact,
) *CircuitArtifacts {
	return &CircuitArtifacts{
		circuitParams: circuitParams{
			name:         name,
			curve:        curve,
			proverOpts:   proverOpts,
			verifierOpts: verifierOpts,
		},
		circuitDefinition: circuit,
		provingKey:        provingKey,
		verifyingKey:      verifyingKey,
	}
}

// Download ensures all artifacts are available, downloading them if necessary.
func (ca *CircuitArtifacts) Download(ctx context.Context) error {
	log.Debugw("ensuring circuit artifacts are downloaded", "circuit", ca.Name())
	if ca.circuitDefinition != nil {
		if err := ca.circuitDefinition.ensureDownloaded(ctx); err != nil {
			return fmt.Errorf("download circuit definition: %w", err)
		}
	}
	if ca.provingKey != nil {
		if err := ca.provingKey.ensureDownloaded(ctx); err != nil {
			return fmt.Errorf("download proving key: %w", err)
		}
	}
	if ca.verifyingKey != nil {
		if err := ca.verifyingKey.ensureDownloaded(ctx); err != nil {
			return fmt.Errorf("download verifying key: %w", err)
		}
	}
	return nil
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

	ccs, err := ca.LoadOrDownloadCircuitDefinition(ctx)
	if err != nil {
		return nil, fmt.Errorf("load circuit definition: %w", err)
	}
	pk, err := ca.LoadOrDownloadProvingKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("load proving key: %w", err)
	}
	vk, err := ca.LoadOrDownloadVerifyingKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("load verifying key: %w", err)
	}
	return NewCircuitRuntime(ca.name, ca.curve, ca.proverOpts, ca.verifierOpts, ccs, pk, vk), nil
}

// LoadOrDownloadCircuitDefinition downloads any missing circuit definition artifact and decodes it into memory.
func (ca *CircuitArtifacts) LoadOrDownloadCircuitDefinition(ctx context.Context) (constraint.ConstraintSystem, error) {
	if ca.circuitDefinition == nil {
		return nil, fmt.Errorf("circuit definition not configured")
	}
	start := time.Now()
	content, err := ca.circuitDefinition.loadOrDownload(ctx)
	if err != nil {
		return nil, fmt.Errorf("load circuit definition: %w", err)
	}
	ccs := newConstraintSystem(ca.curve)
	if _, err := ccs.ReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("decode circuit definition: %w", err)
	}
	log.DebugTime("circuit definition ready", start, "circuit", ca.name)
	return ccs, nil
}

// LoadOrDownloadVerifyingKey downloads any missing verifying key artifact and decodes it into memory.
func (ca *CircuitArtifacts) LoadOrDownloadVerifyingKey(ctx context.Context) (groth16.VerifyingKey, error) {
	if ca.verifyingKey == nil {
		return nil, fmt.Errorf("verifying key not configured")
	}
	start := time.Now()
	content, err := ca.verifyingKey.loadOrDownload(ctx)
	if err != nil {
		return nil, fmt.Errorf("load verifying key: %w", err)
	}
	vk := newVerifyingKey(ca.curve)
	if _, err := vk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("decode verifying key: %w", err)
	}
	log.DebugTime("verifying key ready", start, "circuit", ca.name)
	return vk, nil
}

// LoadOrDownloadProvingKey downloads any missing proving key artifact and decodes it into memory.
func (ca *CircuitArtifacts) LoadOrDownloadProvingKey(ctx context.Context) (groth16.ProvingKey, error) {
	if ca.provingKey == nil {
		return nil, fmt.Errorf("proving key not configured")
	}
	start := time.Now()
	content, err := ca.provingKey.loadOrDownload(ctx)
	if err != nil {
		return nil, fmt.Errorf("load proving key: %w", err)
	}
	pk := newProvingKey(ca.curve)
	if _, err := pk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("decode proving key: %w", err)
	}
	log.DebugTime("proving key ready", start, "circuit", ca.name)
	return pk, nil
}

// RawVerifyingKey returns the content of the verifying key as types.HexBytes.
// It returns an error if the verifying key is not locally available or cannot be serialized.
func (ca *CircuitArtifacts) RawVerifyingKey() ([]byte, error) {
	if ca.verifyingKey == nil {
		return nil, fmt.Errorf("verifying key not configured")
	}
	// Cannot guarantee context is available here, so we load from cache only.
	// The caller should have called LoadOrDownloadVerifyingKey previously if remote fetching was desired.
	content, err := ca.verifyingKey.loadFromCache()
	if err != nil {
		return nil, fmt.Errorf("load verifying key: %w", err)
	}
	return content, nil
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

// Matches reports whether the provided compiled circuit definition matches the
// configured circuit artifact hash.
func (ca *CircuitArtifacts) Matches(ccs constraint.ConstraintSystem) (bool, error) {
	if ccs == nil {
		return false, fmt.Errorf("constraint system not provided")
	}
	expectedHash := ca.CircuitHash()
	if len(expectedHash) == 0 {
		return false, fmt.Errorf("circuit hash not configured for circuit %s", ca.Name())
	}

	startTime := time.Now()
	hasher := sha256.New()
	if _, err := ccs.WriteTo(hasher); err != nil {
		return false, fmt.Errorf("write ccs to hasher: %w", err)
	}
	currentHash := hasher.Sum(nil)
	log.DebugTime("circuit definition hashed", startTime,
		"circuit", ca.Name(),
		"newCircuitHash", hex.EncodeToString(currentHash),
		"oldCircuitHash", hex.EncodeToString(expectedHash),
	)
	return bytes.Equal(currentHash, expectedHash), nil
}

// Setup generates fresh proving and verifying keys for the provided compiled
// circuit definition and returns a runtime built from them.
func (ca *CircuitArtifacts) Setup(ccs constraint.ConstraintSystem) (*CircuitRuntime, error) {
	if ccs == nil {
		return nil, fmt.Errorf("constraint system not provided")
	}

	log.Infow("setting up proving and verifying keys for compiled circuit", "circuit", ca.Name())
	pk, vk, err := prover.Setup(ccs)
	if err != nil {
		return nil, fmt.Errorf("setup circuit %s: %w", ca.Name(), err)
	}
	return NewCircuitRuntime(ca.name, ca.curve, ca.proverOpts, ca.verifierOpts, ccs, pk, vk), nil
}

// LoadOrSetupForCircuit compiles the provided circuit and returns a runtime
// consistent with it. It reuses configured artifacts when the compiled circuit
// hash matches, and otherwise sets up fresh proving and verifying keys.
func (ca *CircuitArtifacts) LoadOrSetupForCircuit(ctx context.Context, circuit frontend.Circuit) (*CircuitRuntime, error) {
	if ca == nil {
		return nil, fmt.Errorf("circuit artifacts not provided")
	}
	ccs, err := frontend.Compile(ca.Curve().ScalarField(), r1cs.NewBuilder, circuit)
	if err != nil {
		return nil, fmt.Errorf("compile circuit: %w", err)
	}
	matches, err := ca.Matches(ccs)
	if err != nil {
		return nil, fmt.Errorf("match artifacts: %w", err)
	}
	if matches {
		runtime, err := ca.LoadOrDownload(ctx)
		if err != nil {
			return nil, fmt.Errorf("load artifacts: %w", err)
		}
		return runtime, nil
	}
	runtime, err := ca.Setup(ccs)
	if err != nil {
		return nil, fmt.Errorf("setup artifacts: %w", err)
	}
	return runtime, nil
}

// CircuitRuntime is a fully initialized runtime view of a circuit's decoded artifacts.
// Once constructed, its getters are infallible.
type CircuitRuntime struct {
	circuitParams
	ccs constraint.ConstraintSystem
	pk  groth16.ProvingKey
	vk  groth16.VerifyingKey
}

// NewCircuitRuntime constructs a runtime from already-decoded artifacts.
func NewCircuitRuntime(name string, curve ecc.ID, proverOpts []backend.ProverOption, verifierOpts []backend.VerifierOption,
	ccs constraint.ConstraintSystem, pk groth16.ProvingKey, vk groth16.VerifyingKey,
) *CircuitRuntime {
	return &CircuitRuntime{
		circuitParams: circuitParams{
			name:         name,
			curve:        curve,
			proverOpts:   proverOpts,
			verifierOpts: verifierOpts,
		},
		ccs: ccs,
		pk:  pk,
		vk:  vk,
	}
}

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
	publicWitness, err := fullWitness.Public()
	if err != nil {
		return nil, err
	}
	if err := cr.VerifyWithWitness(proof, publicWitness); err != nil {
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
	publicWitness, err := frontend.NewWitness(publicAssignment, cr.curve.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("create public witness: %w", err)
	}
	return cr.VerifyWithWitness(proof, publicWitness)
}

// VerifyWithWitness verifies the proof using the public witness.
func (cr *CircuitRuntime) VerifyWithWitness(proof groth16.Proof, publicWitness witness.Witness) (err error) {
	startTime := time.Now()
	defer func() {
		if err == nil {
			log.DebugTime("proof verified", startTime, "circuit", cr.Name())
		}
	}()

	return groth16.Verify(proof, cr.vk, publicWitness, cr.verifierOpts...)
}

// ConstraintSystem returns the decoded constraint system.
func (cr *CircuitRuntime) ConstraintSystem() constraint.ConstraintSystem { return cr.ccs }

// ProvingKey returns the decoded proving key.
func (cr *CircuitRuntime) ProvingKey() groth16.ProvingKey { return cr.pk }

// VerifyingKey returns the decoded verifying key.
func (cr *CircuitRuntime) VerifyingKey() groth16.VerifyingKey { return cr.vk }

func newConstraintSystem(curve ecc.ID) constraint.ConstraintSystem {
	if prover.UseGPUProver {
		return gpugroth16.NewCS(curve)
	}
	return groth16.NewCS(curve)
}

// newProvingKey instantiates an empty proving key compatible with the selected
// backend. When GPU proving is enabled this returns an ICICLE proving key so
// that serialized keys can be read directly into GPU-ready structures.
func newProvingKey(curve ecc.ID) groth16.ProvingKey {
	if prover.UseGPUProver {
		return gpugroth16.NewProvingKey(curve)
	}
	return groth16.NewProvingKey(curve)
}

func newVerifyingKey(curve ecc.ID) groth16.VerifyingKey {
	if prover.UseGPUProver {
		return gpugroth16.NewVerifyingKey(curve)
	}
	return groth16.NewVerifyingKey(curve)
}
