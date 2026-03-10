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
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	gpugroth16 "github.com/consensys/gnark/backend/accelerated/icicle/groth16"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/log"
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
	Name      string
	RemoteURL string
	Hash      []byte
}

// Load reads the artifact content from the local cache using its configured hash.
func (k *Artifact) Load() ([]byte, error) {
	if k == nil {
		return nil, fmt.Errorf("artifact not loaded")
	}
	if len(k.Hash) == 0 {
		return nil, fmt.Errorf("artifact hash not provided")
	}
	content, err := load(k.Hash)
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, fmt.Errorf("no content found")
	}
	return content, nil
}

// Download downloads the content of the artifact from the remote URL,
// checks the hash of the content and stores it locally. It returns an error if
// the remote URL is not provided or the content cannot be downloaded, or if the
// hash of the content does not match. If the content is already loaded, it will
// return.
func (k *Artifact) Download(ctx context.Context) error {
	// if the remote url is not provided, the artifact cannot be loaded so
	// it will return an error
	if k.RemoteURL == "" {
		return fmt.Errorf("key not loaded and remote url not provided")
	}
	// download the content of the artifact from the remote URL
	return downloadAndStore(ctx, k.Hash, k.RemoteURL)
}

// CircuitArtifacts is a struct that holds the artifacts of a zkSNARK circuit
// (definition, proving and verification key). It provides a method to load the
// keys from the local cache or download them from the remote URLs provided.
type CircuitArtifacts struct {
	curve             ecc.ID
	circuitDefinition *Artifact
	provingKey        *Artifact
	verifyingKey      *Artifact
	ccs               constraint.ConstraintSystem
	pk                groth16.ProvingKey
	vk                groth16.VerifyingKey
}

func (ca *CircuitArtifacts) artifactContent(artifact *Artifact, label string) ([]byte, error) {
	if artifact == nil {
		return nil, fmt.Errorf("%s not loaded", label)
	}
	content, err := artifact.Load()
	if err != nil {
		return nil, fmt.Errorf("error loading %s: %w", label, err)
	}
	return content, nil
}

func (ca *CircuitArtifacts) newConstraintSystem() constraint.ConstraintSystem {
	if types.UseGPUProver {
		return gpugroth16.NewCS(ca.curve)
	}
	return groth16.NewCS(ca.curve)
}

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

func (ca *CircuitArtifacts) decodeCircuitDefinition() (constraint.ConstraintSystem, error) {
	content, err := ca.artifactContent(ca.circuitDefinition, "circuit definition")
	if err != nil {
		return nil, err
	}
	ccs := ca.newConstraintSystem()
	if _, err := ccs.ReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("error decoding circuit definition: %w", err)
	}
	return ccs, nil
}

func (ca *CircuitArtifacts) decodeProvingKey() (groth16.ProvingKey, error) {
	content, err := ca.artifactContent(ca.provingKey, "proving key")
	if err != nil {
		return nil, err
	}
	pk := ca.newProvingKey()
	if _, err := pk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("error decoding proving key: %w", err)
	}
	return pk, nil
}

func (ca *CircuitArtifacts) decodeVerifyingKey() (groth16.VerifyingKey, error) {
	content, err := ca.artifactContent(ca.verifyingKey, "verifying key")
	if err != nil {
		return nil, err
	}
	vk := ca.newVerifyingKey()
	if _, err := vk.UnsafeReadFrom(bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("error decoding verifying key: %w", err)
	}
	return vk, nil
}

// NewCircuitArtifacts creates a new CircuitArtifacts struct with the circuit
// artifacts provided. It returns the struct with the artifacts set.
func NewCircuitArtifacts(curve ecc.ID, circuit, provingKey, verifyingKey *Artifact) *CircuitArtifacts {
	return &CircuitArtifacts{
		curve:             curve,
		circuitDefinition: circuit,
		provingKey:        provingKey,
		verifyingKey:      verifyingKey,
	}
}

// LoadAll loads the circuit artifacts into memory.
func (ca *CircuitArtifacts) LoadAll() error {
	if ca.circuitDefinition != nil {
		ccs, err := ca.decodeCircuitDefinition()
		if err != nil {
			return err
		}
		ca.ccs = ccs
	}
	if ca.provingKey != nil {
		pk, err := ca.decodeProvingKey()
		if err != nil {
			return err
		}
		ca.pk = pk
	}
	if ca.verifyingKey != nil {
		vk, err := ca.decodeVerifyingKey()
		if err != nil {
			return err
		}
		ca.vk = vk
	}
	return nil
}

// DownloadAll method downloads the circuit artifacts with the provided context.
// It returns an error if any of the artifacts cannot be downloaded.
func (ca *CircuitArtifacts) DownloadAll(ctx context.Context) error {
	if ca.circuitDefinition != nil {
		if err := ca.circuitDefinition.Download(ctx); err != nil {
			return fmt.Errorf("error downloading circuit definition: %w", err)
		}
	}
	if ca.provingKey != nil {
		if err := ca.provingKey.Download(ctx); err != nil {
			return fmt.Errorf("error downloading proving key: %w", err)
		}
	}
	if ca.verifyingKey != nil {
		if err := ca.verifyingKey.Download(ctx); err != nil {
			return fmt.Errorf("error downloading verifying key: %w", err)
		}
	}
	return nil
}

// DownloadVerifyingKey downloads only the verifying key artifact.
func (ca *CircuitArtifacts) DownloadVerifyingKey(ctx context.Context) error {
	if ca.verifyingKey == nil {
		return fmt.Errorf("verifying key not configured")
	}
	if err := ca.verifyingKey.Download(ctx); err != nil {
		return fmt.Errorf("error downloading verifying key: %w", err)
	}
	return nil
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

// CircuitDefinition returns the content of the circuit definition as
// constraint.ConstraintSystem. It returns an error if the circuit definition is
// not loaded.
func (ca *CircuitArtifacts) CircuitDefinition() (constraint.ConstraintSystem, error) {
	if ca.ccs == nil {
		return nil, fmt.Errorf("circuit definition not loaded")
	}
	return ca.ccs, nil
}

// ProvingKey returns the content of the proving key as groth16.ProvingKey. If
// the proving key is not loaded or cannot be read, it returns an error.
func (ca *CircuitArtifacts) ProvingKey() (groth16.ProvingKey, error) {
	if ca.pk == nil {
		return nil, fmt.Errorf("proving key not loaded")
	}
	return ca.pk, nil
}

// VerifyingKey returns the content of the verifying key as groth16.VerifyingKey.
// It returns an error if the verifying key is not loaded.
func (ca *CircuitArtifacts) VerifyingKey() (groth16.VerifyingKey, error) {
	if ca.vk == nil {
		return nil, fmt.Errorf("verifying key not loaded")
	}
	return ca.vk, nil
}

// RawVerifyingKey returns the content of the verifying key as types.HexBytes.
// It returns an error if the verifying key is not available or cannot be serialized.
func (ca *CircuitArtifacts) RawVerifyingKey() ([]byte, error) {
	if ca.vk != nil {
		var raw bytes.Buffer
		if _, err := ca.vk.WriteTo(&raw); err != nil {
			return nil, fmt.Errorf("write verifying key: %w", err)
		}
		return raw.Bytes(), nil
	}
	if ca.verifyingKey == nil {
		return nil, fmt.Errorf("verifying key not loaded")
	}
	content, err := ca.verifyingKey.Load()
	if err != nil {
		return nil, fmt.Errorf("load verifying key: %w", err)
	}
	return content, nil
}

func load(hash []byte) ([]byte, error) {
	// check if BaseDir exists and create it if it does not
	if _, err := os.Stat(BaseDir); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(BaseDir, os.ModePerm); err != nil {
				return nil, fmt.Errorf("error creating the base directory: %w", err)
			}
		} else {
			return nil, fmt.Errorf("error checking the base directory: %w", err)
		}
	}
	// append the name to the base directory and check if the file exists
	path := filepath.Join(BaseDir, hex.EncodeToString(hash))
	if _, err := os.Stat(path); err != nil {
		// if the file does not exists return nil content and nil error, but if
		// the error is not a not exists error, return the error
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("error checking file %s: %w", path, err)
	}
	// if it exists, read the content of the file and return it
	content, err := os.ReadFile(path)
	if err != nil {
		if err == os.ErrNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("error reading file %s: %w", path, err)
	}

	// check if the hash of the content matches the expected hash
	hasher := sha256.New()
	hasher.Write(content)
	fileHash := hasher.Sum(nil)
	if !bytes.Equal(fileHash, hash) {
		return nil, fmt.Errorf("hash mismatch for file %s: expected %x, got %x", path, hash, fileHash)
	}

	return content, nil
}

// progressReader wraps an io.Reader and keeps track of the total bytes read.
type progressReader struct {
	reader        io.Reader
	total         int64 // updated atomically
	contentLength int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	atomic.AddInt64(&pr.total, int64(n))
	return n, err
}

// downloadAndStore downloads a file from a URL and stores it in the local cache.
func downloadAndStore(ctx context.Context, expectedHash []byte, fileUrl string) error {
	if _, err := url.Parse(fileUrl); err != nil {
		return fmt.Errorf("error parsing the file URL provided: %w", err)
	}

	// Create a SHA256 hasher for integrity check
	hasher := sha256.New()

	// Create BaseDir if it doesn't exist.
	if err := os.MkdirAll(BaseDir, 0o755); err != nil {
		log.Errorf("failed to create base directory for storing circuit artifacts %s: %v", BaseDir, err)
	}

	// Destination file paths
	path := filepath.Join(BaseDir, hex.EncodeToString(expectedHash))

	// If file exists, read it and write to the hasher, then jump to the hash check
	if _, err := os.Stat(path); err == nil {
		existingFile, err := os.Open(path)
		if err == nil {
			if _, err := io.Copy(hasher, existingFile); err != nil {
				if err := existingFile.Close(); err != nil {
					return fmt.Errorf("error closing existing file: %w", err)
				}
				return fmt.Errorf("error hashing existing file: %w", err)
			}
			if err := existingFile.Close(); err != nil {
				return fmt.Errorf("error closing existing file: %w", err)
			}
			computedHash := hasher.Sum(nil)
			if !bytes.Equal(computedHash, expectedHash) {
				log.Warnf("hash mismatch: expected %x, got %x", expectedHash, computedHash)
			} else {
				log.Debugw("artifact found", "hash", hex.EncodeToString(expectedHash), "path", filepath.Dir(path))
				return nil
			}
		}
	}

	partialPath := path + ".partial"
	parentDir := filepath.Dir(path)
	if _, err := os.Stat(parentDir); err != nil {
		return fmt.Errorf("destination path parent folder does not exist")
	}

	// Check if a partial download exists
	var startByte int64 = 0
	if info, err := os.Stat(partialPath); err == nil {
		startByte = info.Size()
	}

	// Create the HTTP request with a Range header for resuming
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileUrl, nil)
	if err != nil {
		return fmt.Errorf("error creating the file request: %w", err)
	}
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error performing the request: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Warnw("error closing response body", "error", err)
		}
	}()

	// Handle response codes
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("error downloading file %q: http status: %d", fileUrl, res.StatusCode)
	}

	// Open file in append mode if resuming, otherwise create new file
	var fileMode int
	if startByte > 0 && res.StatusCode == http.StatusPartialContent {
		fileMode = os.O_APPEND | os.O_WRONLY
	} else {
		fileMode = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}

	fd, err := os.OpenFile(partialPath, fileMode, 0o644)
	if err != nil {
		return fmt.Errorf("error opening artifact file: %w", err)
	}
	defer func() {
		if err := fd.Close(); err != nil {
			log.Warnw("error closing file", "error", err)
		}
	}()

	if startByte > 0 {
		// Hash existing content to continue validation
		existingFile, err := os.Open(partialPath)
		if err == nil {
			if _, err := io.Copy(hasher, existingFile); err != nil {
				if err := existingFile.Close(); err != nil {
					return fmt.Errorf("error closing existing file: %w", err)
				}
				return fmt.Errorf("error hashing existing file: %w", err)
			}
			if err := existingFile.Close(); err != nil {
				return fmt.Errorf("error closing existing file: %w", err)
			}
		}
	}

	// Wrap the response body with a progress tracker
	pr := &progressReader{
		reader:        res.Body,
		contentLength: res.ContentLength + startByte,
	}

	mw := io.MultiWriter(fd, hasher)

	// Copy data in a goroutine
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(mw, pr)
		done <- err
	}()

	// Log progress every 10 seconds
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	u, err := url.Parse(fileUrl)
	if err != nil {
		return fmt.Errorf("error parsing URL: %w", err)
	}
	for {
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("error copying data to file: %w", err)
			}
			goto finished
		case <-ticker.C:
			total := atomic.LoadInt64(&pr.total)
			downloadedMiB := float64(total) / (1024 * 1024)
			var percentage float64
			if pr.contentLength > 0 {
				percentage = (float64(total) / float64(pr.contentLength)) * 100
			}
			log.Debugw("downloading...", "host", u.Host, "path", u.Path,
				"dir", parentDir,
				"downloaded", fmt.Sprintf("%.2fMiB", downloadedMiB),
				"progress", fmt.Sprintf("%.2f%%", percentage))
		}
	}

finished:
	computedHash := hasher.Sum(nil)
	if !bytes.Equal(computedHash, expectedHash) {
		_ = os.Remove(partialPath) // Delete invalid file
		return fmt.Errorf("hash mismatch: expected %x, got %x", expectedHash, computedHash)
	}

	// Rename .partial file to final destination
	if _, err := os.Stat(partialPath); err == nil {
		if err := os.Rename(partialPath, path); err != nil {
			return fmt.Errorf("error renaming file: %w", err)
		}
	}
	log.Debugw("downloaded artifact",
		"path", path,
		"hash", hex.EncodeToString(expectedHash),
		"progress", "100%",
		"size", fmt.Sprintf("%.2fMiB", float64(pr.contentLength)/(1024*1024)),
	)

	return nil
}
