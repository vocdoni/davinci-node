package circuitstest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/fxamacker/cbor/v2"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/prover"
	"github.com/vocdoni/davinci-node/types"
)

// CacheableData defines the interface for data that can be cached
type CacheableData interface {
	WriteToCache(cacheDir, cacheKey string) error
	ReadFromCache(cacheDir, cacheKey string) error
}

// cacheError provides consistent error handling for cache operations
type cacheError struct {
	Op   string
	Type string
	Path string
	Err  error
}

func (e *cacheError) Error() string {
	return fmt.Sprintf("cache %s %s at %s: %v", e.Op, e.Type, e.Path, e.Err)
}

func wrapCacheError(op, typ, path string, err error) error {
	if err == nil {
		return nil
	}
	return &cacheError{Op: op, Type: typ, Path: path, Err: err}
}

// CacheFiles manages file paths for cache entries
type CacheFiles struct {
	BaseDir  string
	CacheKey string
}

// Path returns the full path for a file with the given extension
func (cf CacheFiles) Path(extension string) string {
	return filepath.Join(cf.BaseDir, cf.CacheKey+extension)
}

// Exists checks if files with any of the given extensions exist
func (cf CacheFiles) Exists(extensions ...string) bool {
	for _, ext := range extensions {
		if _, err := os.Stat(cf.Path(ext)); err == nil {
			return true
		}
	}
	return false
}

// writeToFile handles file creation and writing with proper cleanup
func writeToFile(path string, writer func(io.Writer) error) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return writer(file)
}

// readFromFile handles file opening and reading with proper cleanup
func readFromFile(path string, reader func(io.Reader) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return reader(file)
}

// writeCBORToFile writes any data structure to a CBOR file
func writeCBORToFile(path string, data any) error {
	return writeToFile(path, func(w io.Writer) error {
		encOpts := cbor.CoreDetEncOptions()
		em, err := encOpts.EncMode()
		if err != nil {
			return fmt.Errorf("create CBOR encoder: %w", err)
		}
		encoded, err := em.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal CBOR data: %w", err)
		}
		_, err = w.Write(encoded)
		return err
	})
}

// readCBORFromFile reads CBOR data into the provided data structure
func readCBORFromFile(path string, data any) error {
	return readFromFile(path, func(r io.Reader) error {
		encoded, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("read CBOR data: %w", err)
		}
		return cbor.Unmarshal(encoded, data)
	})
}

// groth16Writer interface for components that can write themselves
type groth16Writer interface {
	WriteRawTo(io.Writer) (int64, error)
}

// writeGroth16Component writes any groth16 component to file
func writeGroth16Component(path string, component groth16Writer, componentType string) error {
	err := writeToFile(path, func(w io.Writer) error {
		_, err := component.WriteRawTo(w)
		return err
	})
	return wrapCacheError("write", componentType, path, err)
}

// readGroth16Proof reads a groth16 proof from file
func readGroth16Proof(path string, curve ecc.ID) (groth16.Proof, error) {
	proof := groth16.NewProof(curve)
	err := readFromFile(path, func(r io.Reader) error {
		_, err := proof.ReadFrom(r)
		return err
	})
	return proof, wrapCacheError("read", "proof", path, err)
}

// readGroth16VerifyingKey reads a groth16 verifying key from file
func readGroth16VerifyingKey(path string, curve ecc.ID) (groth16.VerifyingKey, error) {
	vk := groth16.NewVerifyingKey(curve)
	err := readFromFile(path, func(r io.Reader) error {
		_, err := vk.ReadFrom(r)
		return err
	})
	return vk, wrapCacheError("read", "verifying key", path, err)
}

// readGroth16ProvingKey reads a groth16 proving key from file
func readGroth16ProvingKey(path string, curve ecc.ID) (groth16.ProvingKey, error) {
	pk := prover.NewProvingKey(curve)
	err := readFromFile(path, func(r io.Reader) error {
		_, err := pk.ReadFrom(r)
		return err
	})
	return pk, wrapCacheError("read", "proving key", path, err)
}

// writeConstraintSystem writes a constraint system to file
func writeConstraintSystem(path string, cs constraint.ConstraintSystem) error {
	err := writeToFile(path, func(w io.Writer) error {
		_, err := cs.WriteTo(w)
		return err
	})
	return wrapCacheError("write", "constraint system", path, err)
}

// readConstraintSystem reads a constraint system from file
func readConstraintSystem(path string, curve ecc.ID) (constraint.ConstraintSystem, error) {
	cs := groth16.NewCS(curve)
	err := readFromFile(path, func(r io.Reader) error {
		_, err := cs.ReadFrom(r)
		return err
	})
	return cs, wrapCacheError("read", "constraint system", path, err)
}

// writeWitness writes a witness to file
func writeWitness(path string, w witness.Witness) error {
	err := writeToFile(path, func(writer io.Writer) error {
		_, err := w.WriteTo(writer)
		return err
	})
	return wrapCacheError("write", "witness", path, err)
}

// readWitness reads a witness from file
func readWitness(path string, field *big.Int) (witness.Witness, error) {
	w, err := witness.New(field)
	if err != nil {
		return nil, wrapCacheError("create", "witness", path, err)
	}

	err = readFromFile(path, func(r io.Reader) error {
		_, err := w.ReadFrom(r)
		return err
	})
	return w, wrapCacheError("read", "witness", path, err)
}

// CircuitCache manages caching for circuit test data
type CircuitCache struct {
	baseDir string
}

// NewCircuitCache creates a new circuit cache instance
func NewCircuitCache() (*CircuitCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".cache", "davinci-node", "test-circuits")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	return &CircuitCache{baseDir: cacheDir}, nil
}

// GenerateCacheKey creates a deterministic cache key based on circuit type and parameters
func (c *CircuitCache) GenerateCacheKey(circuitType string, processID *types.ProcessID, params ...interface{}) string {
	// Build cache key with circuit type, ProcessID, and additional parameters
	keyData := fmt.Sprintf("%s-%s-%d-%x", circuitType, processID.Address.Hex(), processID.Nonce, processID.Version)

	// Append additional parameters
	for _, param := range params {
		keyData += fmt.Sprintf("-%v", param)
	}

	// Hash the key data for consistent length and avoid filesystem issues
	hash := sha256.Sum256([]byte(keyData))
	return hex.EncodeToString(hash[:])
}

// SaveData saves cacheable data to the cache
func (c *CircuitCache) SaveData(cacheKey string, data CacheableData) error {
	return data.WriteToCache(c.baseDir, cacheKey)
}

// LoadData loads cacheable data from the cache
func (c *CircuitCache) LoadData(cacheKey string, data CacheableData) error {
	if disabled := os.Getenv("DISABLED_CACHE"); disabled == "1" || disabled == "true" {
		return fmt.Errorf("cache is disabled via DISABLED_CACHE environment variable")
	}
	return data.ReadFromCache(c.baseDir, cacheKey)
}

// Exists checks if cached data exists for the given key
func (c *CircuitCache) Exists(cacheKey string) bool {
	cf := CacheFiles{BaseDir: c.baseDir, CacheKey: cacheKey}
	return cf.Exists(".proof", ".pk", ".vk", ".ccs", ".inputs.cbor")
}

// AggregatorCacheData holds cached aggregator circuit data
type AggregatorCacheData struct {
	Proof            groth16.Proof
	VerifyingKey     groth16.VerifyingKey
	ConstraintSystem constraint.ConstraintSystem
	Witness          witness.Witness
	Inputs           AggregatorTestResults
}

// WriteToCache implements CacheableData interface for AggregatorCacheData
func (d *AggregatorCacheData) WriteToCache(cacheDir, cacheKey string) error {
	cf := CacheFiles{BaseDir: cacheDir, CacheKey: cacheKey}

	// Save proof
	if err := writeGroth16Component(cf.Path(".proof"), d.Proof, "proof"); err != nil {
		return err
	}

	// Save verifying key
	if err := writeGroth16Component(cf.Path(".vk"), d.VerifyingKey, "verifying key"); err != nil {
		return err
	}

	// Save constraint system
	if err := writeConstraintSystem(cf.Path(".ccs"), d.ConstraintSystem); err != nil {
		return err
	}

	// Save witness if available
	if d.Witness != nil {
		if err := writeWitness(cf.Path(".witness"), d.Witness); err != nil {
			return err
		}
	}

	// Save inputs in CBOR format
	return wrapCacheError("write", "inputs", cf.Path(".inputs.cbor"),
		writeCBORToFile(cf.Path(".inputs.cbor"), d.Inputs))
}

// ReadFromCache implements CacheableData interface for AggregatorCacheData
func (d *AggregatorCacheData) ReadFromCache(cacheDir, cacheKey string) error {
	cf := CacheFiles{BaseDir: cacheDir, CacheKey: cacheKey}

	// Load proof
	proof, err := readGroth16Proof(cf.Path(".proof"), circuits.AggregatorCurve)
	if err != nil {
		return err
	}
	d.Proof = proof

	// Load verifying key
	vk, err := readGroth16VerifyingKey(cf.Path(".vk"), circuits.AggregatorCurve)
	if err != nil {
		return err
	}
	d.VerifyingKey = vk

	// Load constraint system
	cs, err := readConstraintSystem(cf.Path(".ccs"), circuits.AggregatorCurve)
	if err != nil {
		return err
	}
	d.ConstraintSystem = cs

	// Load witness if available
	if cf.Exists(".witness") {
		witness, err := readWitness(cf.Path(".witness"), circuits.AggregatorCurve.ScalarField())
		if err != nil {
			return err
		}
		d.Witness = witness
	}

	// Load inputs from CBOR file
	return wrapCacheError("read", "inputs", cf.Path(".inputs.cbor"),
		readCBORFromFile(cf.Path(".inputs.cbor"), &d.Inputs))
}

// VoteVerifierCacheData holds cached vote verifier circuit data
type VoteVerifierCacheData struct {
	ProvingKey       groth16.ProvingKey
	VerifyingKey     groth16.VerifyingKey
	ConstraintSystem constraint.ConstraintSystem
	Witness          []witness.Witness
	Inputs           VoteVerifierTestResults
}

// WriteToCache implements CacheableData interface for VoteVerifierCacheData
func (d *VoteVerifierCacheData) WriteToCache(cacheDir, cacheKey string) error {
	cf := CacheFiles{BaseDir: cacheDir, CacheKey: cacheKey}

	// Save proving key
	if err := writeGroth16Component(cf.Path(".pk"), d.ProvingKey, "proving key"); err != nil {
		return err
	}

	// Save verifying key
	if err := writeGroth16Component(cf.Path(".vk"), d.VerifyingKey, "verifying key"); err != nil {
		return err
	}

	// Save constraint system
	if err := writeConstraintSystem(cf.Path(".ccs"), d.ConstraintSystem); err != nil {
		return err
	}

	// Save witness
	for i := range d.Witness {
		name := fmt.Sprintf(".witness-%d", i)
		if err := writeWitness(cf.Path(name), d.Witness[i]); err != nil {
			return err
		}
	}

	// Save inputs in CBOR format
	if err := writeCBORToFile(cf.Path(".inputs.cbor"), d.Inputs); err != nil {
		return wrapCacheError("write", "inputs", cf.Path(".inputs.cbor"), err)
	}
	return nil
}

// ReadFromCache implements CacheableData interface for VoteVerifierCacheData
func (d *VoteVerifierCacheData) ReadFromCache(cacheDir, cacheKey string) error {
	cf := CacheFiles{BaseDir: cacheDir, CacheKey: cacheKey}

	// Load proving key
	pk, err := readGroth16ProvingKey(cf.Path(".pk"), circuits.VoteVerifierCurve)
	if err != nil {
		return err
	}
	d.ProvingKey = pk

	// Load verifying key
	vk, err := readGroth16VerifyingKey(cf.Path(".vk"), circuits.VoteVerifierCurve)
	if err != nil {
		return err
	}
	d.VerifyingKey = vk

	// Load constraint system
	cs, err := readConstraintSystem(cf.Path(".ccs"), circuits.VoteVerifierCurve)
	if err != nil {
		return err
	}
	d.ConstraintSystem = cs

	// Load all witness files matching the pattern
	d.Witness = []witness.Witness{}
	for i := 0; ; i++ {
		name := fmt.Sprintf(".witness-%d", i)
		if !cf.Exists(name) {
			break
		}
		witness, err := readWitness(cf.Path(name), circuits.VoteVerifierCurve.ScalarField())
		if err != nil {
			return err
		}
		d.Witness = append(d.Witness, witness)
	}

	// Load inputs from CBOR file
	if err := readCBORFromFile(cf.Path(".inputs.cbor"), &d.Inputs); err != nil {
		return wrapCacheError("read", "inputs", cf.Path(".inputs.cbor"), err)
	}
	return nil
}

// DeterministicGenerator provides deterministic value generation based on ProcessID
type DeterministicGenerator struct {
	ProcessID *types.ProcessID
}

// NewDeterministicGenerator creates a new deterministic generator
func NewDeterministicGenerator(processID *types.ProcessID) *DeterministicGenerator {
	return &DeterministicGenerator{ProcessID: processID}
}

// Seed creates a deterministic seed based on ProcessID and index
func (dg *DeterministicGenerator) Seed(index int) int64 {
	return GenerateDeterministicSeed(dg.ProcessID, index)
}

// BigInt creates a deterministic big.Int value based on ProcessID and parameters
func (dg *DeterministicGenerator) BigInt(nValidVoters int) *big.Int {
	return GenerateDeterministicK(dg.ProcessID, nValidVoters)
}

// GenerateDeterministicSeed creates a deterministic seed based on ProcessID and index
// This ensures the same ProcessID + index always generates the same seed
func GenerateDeterministicSeed(processID *types.ProcessID, index int) int64 {
	// Create a simple deterministic seed from ProcessID and index
	seed := int64(processID.Version[0])<<24 | int64(processID.Version[1])<<16 |
		int64(processID.Version[2])<<8 | int64(processID.Version[3])
	seed = seed*1000000 + int64(processID.Nonce)*1000 + int64(index)

	// Add some variation based on the address
	if len(processID.Address) >= 8 {
		addrSeed := int64(processID.Address[0])<<24 | int64(processID.Address[1])<<16 |
			int64(processID.Address[2])<<8 | int64(processID.Address[3])
		seed += addrSeed
	}

	// Ensure positive seed
	if seed < 0 {
		seed = -seed
	}
	if seed == 0 {
		seed = 1
	}

	return seed
}

// GenerateDeterministicK creates a deterministic big.Int value based on ProcessID and parameters
// This ensures consistent K generation for cryptographic operations
func GenerateDeterministicK(processID *types.ProcessID, nValidVoters int) *big.Int {
	// Create a deterministic seed from ProcessID and parameters
	seed := GenerateDeterministicSeed(processID, nValidVoters)

	// Create a deterministic k value based on the seed
	k := big.NewInt(seed)

	// Ensure k is within a valid range for elliptic curve operations
	// Take modulo with a reasonable bound to avoid overly large values
	maxK := big.NewInt(1)
	maxK.Lsh(maxK, 128) // 2^128
	k.Mod(k, maxK)

	// Ensure k is not zero
	if k.Sign() == 0 {
		k = big.NewInt(1)
	}

	return k
}
