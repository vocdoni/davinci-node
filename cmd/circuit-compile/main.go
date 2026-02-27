package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/algebra/native/sw_bls12377"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	flag "github.com/spf13/pflag"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

// Keeps track of files created during program execution
var createdFiles []string

func main() {
	// Configuration flags
	var destination string
	var updateConfig bool
	var configPath string
	var updateWasm bool
	var force bool
	s3Config := NewDefaultS3Config()

	// Define flags
	flag.StringVar(&destination, "destination", "artifacts", "destination folder for the artifacts")
	flag.BoolVar(&updateConfig, "update-config", false, "update circuit_artifacts.go file with new hashes")
	flag.StringVar(&configPath, "config-path", "", "path to circuit_artifacts.go file (auto-detected if not specified)")
	flag.BoolVar(&updateWasm, "update-wasm", false, "compile and update WASM files only")
	flag.BoolVar(&force, "force", false, "force recompilation even if CCS artifact already exists in destination")

	// S3 configuration flags
	flag.BoolVar(&s3Config.Enabled, "s3.enabled", false, "enable S3 uploads")
	flag.StringVar(&s3Config.HostBase, "s3.host-base", "ams3.digitaloceanspaces.com", "S3 host base")
	flag.StringVar(&s3Config.HostBucket, "s3.host-bucket", "%s.ams3.digitaloceanspaces.com", "S3 host bucket pattern")
	flag.StringVar(&s3Config.AccessKey, "s3.access-key", "", "S3 access key")
	flag.StringVar(&s3Config.SecretKey, "s3.secret-key", "", "S3 secret key")
	flag.StringVar(&s3Config.Space, "s3.space", "circuits", "S3 space (bucket name)")
	flag.StringVar(&s3Config.Bucket, "s3.bucket", "dev", "S3 bucket (folder name)")

	flag.Parse()
	log.Init("debug", "stdout", nil)

	// Test S3 connection if enabled
	if s3Config.Enabled {
		ctx := context.Background()
		if err := TestS3Connection(ctx, s3Config); err != nil {
			log.Fatalf("S3 connection test failed: %v", err)
		}
	}

	// Hash list to store the hashes of the generated artifacts
	// Using the same names as in config/circuit_artifacts.go
	hashList := map[string]string{}

	// Create the destination folder if it doesn't exist
	if err := os.MkdirAll(destination, 0o755); err != nil {
		log.Fatalf("error creating destination folder: %v", err)
	}
	log.Infow("destination folder", "path", destination)

	// Handle WASM-only compilation if flag is set
	if updateWasm {
		if err := compileAndUpdateWasm(destination, hashList, s3Config, updateConfig, configPath); err != nil {
			log.Fatalf("failed to compile and update WASM: %v", err)
		}
		return
	}

	////////////////////////////////////////
	// Ballot Proof Circom Artifacts
	////////////////////////////////////////
	if err := processBallotProofArtifacts(destination, force, hashList); err != nil {
		log.Fatalf("error processing ballot proof artifacts: %v", err)
	}

	////////////////////////////////////////
	// Vote Verifier Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling vote verifier circuit...")
	startTime := time.Now()
	// generate the placeholders for the recursion
	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(
		ballotproof.CircomVerificationKey, circuits.BallotProofNPubInputs)
	if err != nil {
		log.Fatalf("error generating circom2gnark placeholder: %v", err)
	}
	// compile the circuit
	voteVerifierCCS, err := frontend.Compile(params.VoteVerifierCurve.ScalarField(), r1cs.NewBuilder, &voteverifier.VerifyVoteCircuit{
		CircomVerificationKey: circomPlaceholder.Vk,
		CircomProof:           circomPlaceholder.Proof,
	})
	if err != nil {
		log.Fatalf("error compiling vote verifier circuit: %v", err)
	}
	logElapsed("vote verifier circuit compiled", startTime)
	voteVerifierArtifacts, err := compileCircuitArtifacts(
		"VoteVerifier",
		voteVerifierCCS,
		voteverifier.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing vote verifier artifacts: %v", err)
	}
	hashList["VoteVerifierCircuitHash"] = voteVerifierArtifacts.CircuitHash
	hashList["VoteVerifierProvingKeyHash"] = voteVerifierArtifacts.ProvingKeyHash
	hashList["VoteVerifierVerificationKeyHash"] = voteVerifierArtifacts.VerifyingKeyHash

	////////////////////////////////////////
	// Aggregator Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling aggregator circuit...")
	startTime = time.Now()
	voteVerifierFixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](voteVerifierArtifacts.VerifyingKey)
	if err != nil {
		log.Fatalf("failed to fix VoteVerifier verification key: %v", err)
	}
	// create final placeholder
	aggregatorPlaceholder := &aggregator.AggregatorCircuit{
		Proofs:          [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
		VerificationKey: voteVerifierFixedVk,
	}
	for i := range params.VotesPerBatch {
		aggregatorPlaceholder.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](voteVerifierCCS)
	}

	aggregatorCCS, err := frontend.Compile(params.AggregatorCurve.ScalarField(), r1cs.NewBuilder, aggregatorPlaceholder)
	if err != nil {
		log.Fatalf("failed to compile aggregator circuit: %v", err)
	}
	logElapsed("aggregator circuit compiled", startTime)
	aggregatorArtifacts, err := compileCircuitArtifacts(
		"Aggregator",
		aggregatorCCS,
		aggregator.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing aggregator artifacts: %v", err)
	}
	hashList["AggregatorCircuitHash"] = aggregatorArtifacts.CircuitHash
	hashList["AggregatorProvingKeyHash"] = aggregatorArtifacts.ProvingKeyHash
	hashList["AggregatorVerificationKeyHash"] = aggregatorArtifacts.VerifyingKeyHash

	////////////////////////////////////////
	// Statetransition Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling statetransition circuit...")
	startTime = time.Now()
	aggregatorFixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](aggregatorArtifacts.VerifyingKey)
	if err != nil {
		log.Fatalf("failed to fix aggregator verification key: %v", err)
	}
	// create final placeholder
	statetransitionPlaceholder := &statetransition.StateTransitionCircuit{
		AggregatorProof: stdgroth16.PlaceholderProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](aggregatorCCS),
		AggregatorVK:    aggregatorFixedVk,
	}
	statetransitionCCS, err := frontend.Compile(params.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, statetransitionPlaceholder)
	if err != nil {
		log.Fatalf("failed to compile statetransition circuit: %v", err)
	}
	logElapsed("statetransition circuit compiled", startTime)
	statetransitionArtifacts, err := compileCircuitArtifacts(
		"StateTransition",
		statetransitionCCS,
		statetransition.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing statetransition artifacts: %v", err)
	}
	hashList["StateTransitionCircuitHash"] = statetransitionArtifacts.CircuitHash
	hashList["StateTransitionProvingKeyHash"] = statetransitionArtifacts.ProvingKeyHash
	hashList["StateTransitionVerificationKeyHash"] = statetransitionArtifacts.VerifyingKeyHash

	statetransitionVkeySolFile := ""
	if statetransitionArtifacts.Recompiled {
		vkeySolFile, err := exportSolidityVerifierFile(
			"statetransition",
			statetransitionArtifacts,
			destination,
		)
		if err != nil {
			log.Fatalf("failed to export state transition verifier: %v", err)
		}
		statetransitionVkeySolFile = vkeySolFile
	}

	/*
		ResultsVerifier Circuit Compilation
	*/
	log.Infow("compiling results verifier circuit...")
	startTime = time.Now()
	// create final placeholder
	resultsverifierPlaceholder := &results.ResultsVerifierCircuit{}
	resultsverifierCCS, err := frontend.Compile(params.ResultsVerifierCurve.ScalarField(), r1cs.NewBuilder, resultsverifierPlaceholder)
	if err != nil {
		log.Fatalf("failed to compile results verifier circuit: %v", err)
	}
	logElapsed("results verifier circuit compiled", startTime)
	resultsverifierArtifacts, err := compileCircuitArtifacts(
		"ResultsVerifier",
		resultsverifierCCS,
		results.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing results verifier artifacts: %v", err)
	}
	hashList["ResultsVerifierCircuitHash"] = resultsverifierArtifacts.CircuitHash
	hashList["ResultsVerifierProvingKeyHash"] = resultsverifierArtifacts.ProvingKeyHash
	hashList["ResultsVerifierVerificationKeyHash"] = resultsverifierArtifacts.VerifyingKeyHash

	resultsverifierVkeySolFile := ""
	if resultsverifierArtifacts.Recompiled {
		vkeySolFile, err := exportSolidityVerifierFile(
			"resultsverifier",
			resultsverifierArtifacts,
			destination,
		)
		if err != nil {
			log.Fatalf("failed to export results verifier: %v", err)
		}
		resultsverifierVkeySolFile = vkeySolFile
	} else {
		log.Infow("results verifier setup skipped; circuit unchanged")
	}
	////////////////////////////////////////
	// Print hash list and upload files
	////////////////////////////////////////
	hashListData, err := json.MarshalIndent(hashList, "", "  ")
	if err != nil {
		log.Fatalf("error marshalling hash list: %v", err)
	}

	fmt.Printf("Hash list: \n%s\n", hashListData)

	// Upload the newly created artifacts to S3 if enabled
	if s3Config.Enabled {
		ctx := context.Background()
		log.Infow("starting S3 upload", "files_count", len(createdFiles))
		if err := UploadFiles(ctx, createdFiles, s3Config); err != nil {
			log.Warnw("failed to upload artifacts to S3", "error", err)
		}
	}

	// Update circuit_artifacts.go file if enabled
	if updateConfig {
		log.Infow("updating circuit artifacts config file")

		// Find the config file if path not specified
		if configPath == "" {
			var err error
			configPath, err = FindCircuitArtifactsFile()
			if err != nil {
				log.Warnw("failed to find circuit_artifacts.go file", "error", err)
				return
			}
			log.Infow("found circuit artifacts config file", "path", configPath)
		}

		// Check what changes would be made
		changes, err := CheckHashChanges(hashList, configPath)
		if err != nil {
			log.Warnw("failed to check hash changes", "error", err)
			return
		}

		if len(changes) == 0 {
			log.Infow("no changes needed for circuit artifacts config file")
			return
		}

		log.Infow("the following changes will be made to the config file", "changes", changes)

		// Update the config file
		if err := UpdateCircuitArtifactsConfig(hashList, configPath); err != nil {
			log.Warnw("failed to update circuit artifacts config file", "error", err)
			return
		}

		log.Infow("circuit artifacts config file updated successfully", "path", configPath)

		// copy the solidity files to the config directory
		configDir := filepath.Dir(configPath)
		statetransitionSolidityFile := path.Join(configDir, "statetransition_vkey.sol")
		if err := copySolidityVerifierFile(statetransitionVkeySolFile, statetransitionSolidityFile); err != nil {
			log.Warnw("failed to copy statetransition vkey.sol file", "error", err)
			return
		}

		resultsverifierSolidityFile := path.Join(configDir, "resultsverifier_vkey.sol")
		if err := copySolidityVerifierFile(resultsverifierVkeySolFile, resultsverifierSolidityFile); err != nil {
			log.Warnw("failed to copy resultsverifier vkey.sol file", "error", err)
			return
		}
	}
}

func processBallotProofArtifacts(destination string, force bool, hashList map[string]string) error {
	startTime := time.Now()
	ballotProofWASMHash, err := circuits.HashBytesSHA256(ballotproof.CircomCircuitWasm)
	if err != nil {
		return fmt.Errorf("hash ballot proof wasm: %w", err)
	}
	hashList["BallotProofCircuitHash"] = ballotProofWASMHash

	ballotProofPKHash, err := circuits.HashBytesSHA256(ballotproof.CircomProvingKey)
	if err != nil {
		return fmt.Errorf("hash ballot proof proving key: %w", err)
	}
	hashList["BallotProofProvingKeyHash"] = ballotProofPKHash

	ballotProofVKHash, err := circuits.HashBytesSHA256(ballotproof.CircomVerificationKey)
	if err != nil {
		return fmt.Errorf("hash ballot proof verification key: %w", err)
	}
	hashList["BallotProofVerificationKeyHash"] = ballotProofVKHash

	ballotProofRecompiled := force
	if !ballotProofRecompiled {
		if _, err := os.Stat(filepath.Join(destination, ballotProofWASMHash)); err != nil {
			ballotProofRecompiled = true
		}
	}
	if !ballotProofRecompiled {
		if _, err := os.Stat(filepath.Join(destination, ballotProofPKHash)); err != nil {
			ballotProofRecompiled = true
		}
	}
	if !ballotProofRecompiled {
		if _, err := os.Stat(filepath.Join(destination, ballotProofVKHash)); err != nil {
			ballotProofRecompiled = true
		}
	}

	log.Infow("processing ballot proof circom artifacts...", "recompile", ballotProofRecompiled)
	if ballotProofRecompiled {
		hash, err := writeHashedBytes(ballotproof.CircomCircuitWasm, destination)
		if err != nil {
			return fmt.Errorf("copy ballot proof wasm: %w", err)
		}
		hashList["BallotProofCircuitHash"] = hash

		hash, err = writeHashedBytes(ballotproof.CircomProvingKey, destination)
		if err != nil {
			return fmt.Errorf("copy ballot proof proving key: %w", err)
		}
		hashList["BallotProofProvingKeyHash"] = hash

		hash, err = writeHashedBytes(ballotproof.CircomVerificationKey, destination)
		if err != nil {
			return fmt.Errorf("copy ballot proof verification key: %w", err)
		}
		hashList["BallotProofVerificationKeyHash"] = hash
	} else {
		log.Infow("skipping ballot proof artifact copy; artifacts already present")
	}

	log.Infow("ballot proof circom artifacts processed", "elapsed", time.Since(startTime).String())
	return nil
}

func copySolidityVerifierFile(sourcePath, destPath string) error {
	if sourcePath == "" {
		return nil
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open vkey.sol file: %w", err)
	}
	defer func() {
		if err := sourceFile.Close(); err != nil {
			log.Warnw("failed to close source vkey.sol file", "error", err)
		}
	}()

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create destination vkey.sol file: %w", err)
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			log.Warnw("failed to close destination vkey.sol file", "error", err)
		}
	}()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("copy vkey.sol file: %w", err)
	}

	log.Infow("copied vkey.sol file to config directory", "path", destPath)
	return nil
}

func exportSolidityVerifierFile(name string, artifacts *CircuitArtifactsResult, destination string) (string, error) {
	if artifacts == nil {
		return "", fmt.Errorf("missing artifacts for %s", name)
	}
	log.Infow(fmt.Sprintf("exporting %s solidity verifier...", name))
	solidityVk, ok := artifacts.VerifyingKey.(*groth16_bn254.VerifyingKey)
	if !ok {
		return "", fmt.Errorf("unexpected verifying key type for %s", name)
	}
	if err := solidityVk.Precompute(); err != nil {
		return "", fmt.Errorf("precompute %s vk: %w", name, err)
	}
	vkeySolFile := path.Join(destination, fmt.Sprintf("%s_vkey.sol", name))
	fd, err := os.Create(vkeySolFile)
	if err != nil {
		return "", fmt.Errorf("create %s_vkey.sol: %w", name, err)
	}
	buf := bytes.NewBuffer(nil)
	if err := solidityVk.ExportSolidity(buf, solidity.WithPragmaVersion("^0.8.28")); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			log.Warnw("failed to close vkey.sol file after export error", "error", closeErr)
		}
		return "", fmt.Errorf("export %s vk to Solidity: %w", name, err)
	}
	if _, err := fd.Write(buf.Bytes()); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			log.Warnw("failed to close vkey.sol file after write error", "error", closeErr)
		}
		return "", fmt.Errorf("write %s_vkey.sol: %w", name, err)
	}
	if err := fd.Close(); err != nil {
		log.Warnw("failed to close vkey.sol file", "error", err)
	}

	if err := insertProvingKeyHashToVkeySolidity(vkeySolFile, artifacts.ProvingKeyHash); err != nil {
		log.Warnw("failed to insert proving key hash into vkey.sol", "error", err)
	}
	log.Infow(fmt.Sprintf("%s vkey.sol file created", name), "path", vkeySolFile)
	return vkeySolFile, nil
}

func logElapsed(message string, startTime time.Time) {
	log.Infow(message, "elapsed", time.Since(startTime).String())
}

type CompileCircuitArtifactsResult struct {
	VerifyingKey     groth16.VerifyingKey
	Recompiled       bool
	CircuitHash      string
	ProvingKeyHash   string
	VerifyingKeyHash string
}

func compileCircuitArtifacts(
	circuitName string,
	ccs constraint.ConstraintSystem,
	artifacts *circuits.CircuitArtifacts,
	destination string,
	force bool,
) (*CompileCircuitArtifactsResult, error) {
	if artifacts == nil {
		return nil, fmt.Errorf("missing artifacts for %s", circuitName)
	}
	expectedCircuitHash := hex.EncodeToString(artifacts.CircuitHash())
	expectedProvingKeyHash := hex.EncodeToString(artifacts.ProvingKeyHash())
	expectedVerificationKeyHash := hex.EncodeToString(artifacts.VerifyingKeyHash())

	startTime := time.Now()
	ccsHash, err := circuits.HashConstraintSystem(ccs)
	if err != nil {
		return nil, fmt.Errorf("hash %s circuit: %w", circuitName, err)
	}
	log.Infow(fmt.Sprintf("%s circuit prepared", circuitName), "elapsed", time.Since(startTime).String(),
		"ccsHash", ccsHash,
		"expectedCircuitHash", expectedCircuitHash)

	if ccsHash == expectedCircuitHash && !force {
		if err := artifacts.DownloadVerifyingKey(context.Background()); err != nil {
			return nil, fmt.Errorf("download %s verifying key: %w", circuitName, err)
		}

		vk, err := loadVerifyingKeyFromHash(destination, expectedVerificationKeyHash, artifacts.Curve())
		if err != nil {
			return nil, fmt.Errorf("load existing %s vk %s: %w", circuitName, expectedVerificationKeyHash, err)
		}
		log.Infow(fmt.Sprintf("%s setup skipped; using existing vk from destination", circuitName), "hash", expectedVerificationKeyHash)
		return &CompileCircuitArtifactsResult{
			VerifyingKey:     vk,
			Recompiled:       false,
			CircuitHash:      expectedCircuitHash,
			ProvingKeyHash:   expectedProvingKeyHash,
			VerifyingKeyHash: expectedVerificationKeyHash,
		}, nil
	}

	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		return nil, fmt.Errorf("setup %s circuit: %w", circuitName, err)
	}

	startTime = time.Now()
	log.Infof("writing %s artifacts to disk", circuitName)
	circuitHash, err := writeCS(ccs, destination)
	if err != nil {
		return nil, fmt.Errorf("write %s constraint system: %w", circuitName, err)
	}

	provingKeyHash, err := writePK(pk, destination)
	if err != nil {
		return nil, fmt.Errorf("write %s proving key: %w", circuitName, err)
	}

	verifyingKeyHash, err := writeVK(vk, destination)
	if err != nil {
		return nil, fmt.Errorf("write %s verifying key: %w", circuitName, err)
	}

	log.Infow(fmt.Sprintf("%s artifacts written to disk", circuitName), "elapsed", time.Since(startTime).String())
	return &CompileCircuitArtifactsResult{
		VerifyingKey:     vk,
		Recompiled:       true,
		CircuitHash:      circuitHash,
		ProvingKeyHash:   provingKeyHash,
		VerifyingKeyHash: verifyingKeyHash,
	}, nil
}

func loadVerifyingKeyFromHash(destination, hash string, curve ecc.ID) (groth16.VerifyingKey, error) {
	path := filepath.Join(destination, hash)
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open verifying key file for hash %s in %s: %w", hash, destination, err)
	}
	defer func() {
		if err := fd.Close(); err != nil {
			log.Warnw("failed to close verifying key file", "path", path, "error", err)
		}
	}()
	vk := groth16.NewVerifyingKey(curve)
	if _, err := vk.ReadFrom(fd); err != nil {
		return nil, fmt.Errorf("read verifying key file %s: %w", path, err)
	}
	return vk, nil
}

// writeCS writes the Constraint System to a file and returns its SHA256 hash
func writeCS(cs constraint.ConstraintSystem, to string) (string, error) {
	return writeToFile(to, func(w io.Writer) error {
		_, err := cs.WriteTo(w)
		return err
	})
}

// writePK writes the Proving Key to a file and returns its SHA256 hash
func writePK(pk groth16.ProvingKey, to string) (string, error) {
	return writeToFile(to, func(w io.Writer) error {
		_, err := pk.WriteTo(w)
		return err
	})
}

// writeVK writes the Verifying Key to a file and returns its SHA256 hash
func writeVK(vk groth16.VerifyingKey, to string) (string, error) {
	return writeToFile(to, func(w io.Writer) error {
		_, err := vk.WriteTo(w)
		return err
	})
}

// writeToFile handles efficient writing to a file and computing its SHA256 hash
// Returns the hash of the written content
func writeToFile(to string, writeFunc func(w io.Writer) error) (string, error) {
	// Create a hash writer
	hashFn := sha256.New()

	// Create a temp file for writing
	tempFile, err := os.CreateTemp(to, "temp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file in %s: %w", to, err)
	}
	tempFilename := tempFile.Name()

	// Make sure we clean up the temp file if we encounter an error
	success := false
	defer func() {
		if tempFile != nil {
			if err := tempFile.Close(); err != nil {
				log.Warnw("failed to close temp file", "error", err)
			}
		}
		if !success {
			if err := os.Remove(tempFilename); err != nil {
				log.Warnw("failed to remove temp file", "error", err, "path", tempFilename)
			}
		}
	}()

	// Create a multi-writer to write to both the hash and the file at once
	mw := io.MultiWriter(hashFn, tempFile)

	// Write content using the provided function
	if err := writeFunc(mw); err != nil {
		return "", fmt.Errorf("failed to write content: %w", err)
	}

	// Compute the hash and create the final filename
	hash := hex.EncodeToString(hashFn.Sum(nil))
	finalFilename := filepath.Join(to, hash)

	// Close the temp file before renaming
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}
	tempFile = nil

	// Rename the temp file to the final filename
	if err := os.Rename(tempFilename, finalFilename); err != nil {
		return "", fmt.Errorf("failed to rename temp file to %s: %w", finalFilename, err)
	}

	// Set success to true to avoid removing the temp file in the deferred function
	success = true

	// Add the created file to the global list
	createdFiles = append(createdFiles, finalFilename)

	return hash, nil
}

// insertProvingKeyHashToVkeySolidity reads the generated .sol file at dest/vkey.sol,
// injects a `bytes32 constant PROVING_KEY_HASH = 0x…;` line immediately
// after the `contract <Name> {` declaration, and writes it back out.
func insertProvingKeyHashToVkeySolidity(filePath, hexHash string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading vkey.sol: %w", err)
	}
	//    (?m) enables multiline mode so ^ matches at the start of each line.
	//    We match `contract <anyIdent> {` and capture it.
	re := regexp.MustCompile(`(?m)^(contract\s+\w+\s*\{)`)
	inject := fmt.Sprintf("$1\n    bytes32 constant PROVING_KEY_HASH = 0x%s;", hexHash)
	patched := re.ReplaceAll(data, []byte(inject))

	if err := os.WriteFile(filePath, patched, os.FileMode(0o644)); err != nil {
		return fmt.Errorf("writing patched vkey.sol: %w", err)
	}
	return nil
}

// executeCommand executes a command in the specified directory
func executeCommand(command, dir string) error {
	cmd := exec.Command("make")
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}

	log.Infow("command executed successfully", "command", command, "dir", dir, "output", string(output))
	return nil
}

func writeHashedBytes(content []byte, destDir string) (string, error) {
	return writeToFile(destDir, func(w io.Writer) error {
		if _, err := w.Write(content); err != nil {
			return fmt.Errorf("failed to copy artifact content: %w", err)
		}
		return nil
	})
}

// copyAndHashWasmFile copies a WASM file to the destination directory with versioned naming and returns its hash
func copyAndHashWasmFile(srcPath, destDir, baseFileName string) (string, error) {
	// Read the source file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			log.Warnw("failed to close source file", "error", err)
		}
	}()

	// First pass: compute the hash
	hashFn := sha256.New()
	if _, err := io.Copy(hashFn, srcFile); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}
	hash := hex.EncodeToString(hashFn.Sum(nil))

	// Get the last 4 hex digits for versioning
	hashSuffix := hash[len(hash)-4:]

	// Create versioned filename without extension.
	nameWithoutExt := baseFileName[:len(baseFileName)-len(filepath.Ext(baseFileName))]
	if nameWithoutExt == "" {
		nameWithoutExt = baseFileName
	}
	versionedName := fmt.Sprintf("%s_%s", nameWithoutExt, hashSuffix)

	finalFilename := filepath.Join(destDir, versionedName)

	// Reset file pointer to beginning for second pass
	if _, err := srcFile.Seek(0, 0); err != nil {
		return "", fmt.Errorf("failed to reset file pointer: %w", err)
	}

	// Second pass: copy to destination with versioned name
	destFile, err := os.Create(finalFilename)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file %s: %w", finalFilename, err)
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			log.Warnw("failed to close destination file", "error", err)
		}
	}()

	// Copy the file content
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy file content: %w", err)
	}

	// Add the created file to the global list
	createdFiles = append(createdFiles, finalFilename)

	log.Infow("WASM file copied and hashed with versioned name", "src", srcPath, "dest", finalFilename, "hash", hash, "version_suffix", hashSuffix)
	return hash, nil
}

// compileAndUpdateWasm compiles the WASM files and updates the configuration
func compileAndUpdateWasm(destination string, hashList map[string]string, s3Config *S3Config, updateConfig bool, configPath string) error {
	log.Infow("compiling WASM files...")

	// Change to the WASM directory and run make
	wasmDir := "cmd/davincicrypto-wasm"
	startTime := time.Now()

	// Execute make command in the WASM directory
	if err := executeCommand("make", wasmDir); err != nil {
		return fmt.Errorf("failed to compile WASM: %w", err)
	}

	log.Infow("WASM compilation completed", "elapsed", time.Since(startTime).String())

	// Copy and hash the WASM file with fixed name
	wasmFile := filepath.Join(wasmDir, "davinci_crypto.wasm")
	wasmHash, err := copyAndHashWasmFile(wasmFile, destination, "davinci_crypto.wasm")
	if err != nil {
		return fmt.Errorf("failed to process WASM file: %w", err)
	}
	hashList["BallotProofWasmHelperHash"] = wasmHash

	// Copy and hash the JS file with fixed name
	jsFile := filepath.Join(wasmDir, "wasm_exec.js")
	jsHash, err := copyAndHashWasmFile(jsFile, destination, "wasm_exec.js")
	if err != nil {
		return fmt.Errorf("failed to process JS file: %w", err)
	}
	hashList["BallotProofWasmExecJsHash"] = jsHash

	log.Infow("WASM files processed", "wasm_hash", wasmHash, "js_hash", jsHash)

	// Print hash list
	hashListData, err := json.MarshalIndent(hashList, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling hash list: %w", err)
	}
	fmt.Printf("Hash list: \n%s\n", hashListData)

	// Upload files to S3 if enabled
	if s3Config.Enabled {
		ctx := context.Background()
		log.Infow("starting S3 upload", "files_count", len(createdFiles))
		if err := UploadFiles(ctx, createdFiles, s3Config); err != nil {
			log.Warnw("failed to upload WASM files to S3", "error", err)
		}
	}

	// Update config file if enabled
	if updateConfig {
		log.Infow("updating circuit artifacts config file")

		// Find the config file if path not specified
		if configPath == "" {
			var err error
			configPath, err = FindCircuitArtifactsFile()
			if err != nil {
				return fmt.Errorf("failed to find circuit_artifacts.go file: %w", err)
			}
			log.Infow("found circuit artifacts config file", "path", configPath)
		}

		// Check what changes would be made
		changes, err := CheckHashChanges(hashList, configPath)
		if err != nil {
			return fmt.Errorf("failed to check hash changes: %w", err)
		}

		if len(changes) == 0 {
			log.Infow("no changes needed for circuit artifacts config file")
			return nil
		}

		log.Infow("the following changes will be made to the config file", "changes", changes)

		// Update the config file
		if err := UpdateCircuitArtifactsConfig(hashList, configPath); err != nil {
			return fmt.Errorf("failed to update circuit artifacts config file: %w", err)
		}

		log.Infow("circuit artifacts config file updated successfully", "path", configPath)
	}

	return nil
}
