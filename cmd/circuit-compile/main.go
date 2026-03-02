package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	var force bool
	s3Config := NewDefaultS3Config()

	// Define flags
	flag.StringVar(&destination, "destination", circuits.BaseDir, "destination folder for the artifacts")
	flag.BoolVar(&updateConfig, "update-config", false, "update circuit_artifacts.go file with new hashes")
	flag.StringVar(&configPath, "config-path", "", "path to circuit_artifacts.go file (auto-detected if not specified)")
	flag.BoolVar(&force, "force", false, "force recompilation of artifacts even if CCS is unchanged")

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

	////////////////////////////////////////
	// Ballot Proof Circom Artifacts
	////////////////////////////////////////
	ballotProofArtifacts, err := processBallotProofArtifacts(destination, force)
	if err != nil {
		log.Fatalf("error processing BallotProof artifacts: %v", err)
	}
	hashList["BallotProofCircuitHash"] = ballotProofArtifacts.CircuitHash
	hashList["BallotProofProvingKeyHash"] = ballotProofArtifacts.ProvingKeyHash
	hashList["BallotProofVerificationKeyHash"] = ballotProofArtifacts.VerifyingKeyHash

	////////////////////////////////////////
	// Vote Verifier Circuit Compilation
	////////////////////////////////////////
	startTime := time.Now()
	log.Infow("compiling VoteVerifier circuit...")
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
		log.Fatalf("error compiling VoteVerifier circuit: %v", err)
	}
	logElapsed("VoteVerifier circuit compiled", startTime)
	voteVerifierArtifacts, err := compileCircuitArtifacts(
		"VoteVerifier",
		voteVerifierCCS,
		voteverifier.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing VoteVerifier artifacts: %v", err)
	}
	hashList["VoteVerifierCircuitHash"] = voteVerifierArtifacts.CircuitHash
	hashList["VoteVerifierProvingKeyHash"] = voteVerifierArtifacts.ProvingKeyHash
	hashList["VoteVerifierVerificationKeyHash"] = voteVerifierArtifacts.VerifyingKeyHash

	////////////////////////////////////////
	// Aggregator Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling Aggregator circuit...")
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
		log.Fatalf("failed to compile Aggregator circuit: %v", err)
	}
	logElapsed("Aggregator circuit compiled", startTime)
	aggregatorArtifacts, err := compileCircuitArtifacts(
		"Aggregator",
		aggregatorCCS,
		aggregator.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing Aggregator artifacts: %v", err)
	}
	hashList["AggregatorCircuitHash"] = aggregatorArtifacts.CircuitHash
	hashList["AggregatorProvingKeyHash"] = aggregatorArtifacts.ProvingKeyHash
	hashList["AggregatorVerificationKeyHash"] = aggregatorArtifacts.VerifyingKeyHash

	////////////////////////////////////////
	// Statetransition Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling StateTransition circuit...")
	startTime = time.Now()
	aggregatorFixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](aggregatorArtifacts.VerifyingKey)
	if err != nil {
		log.Fatalf("failed to fix Aggregator verification key: %v", err)
	}
	// create final placeholder
	statetransitionPlaceholder := &statetransition.StateTransitionCircuit{
		AggregatorProof: stdgroth16.PlaceholderProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](aggregatorCCS),
		AggregatorVK:    aggregatorFixedVk,
	}
	statetransitionCCS, err := frontend.Compile(params.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, statetransitionPlaceholder)
	if err != nil {
		log.Fatalf("failed to compile StateTransition circuit: %v", err)
	}
	logElapsed("StateTransition circuit compiled", startTime)
	statetransitionArtifacts, err := compileCircuitArtifacts(
		"StateTransition",
		statetransitionCCS,
		statetransition.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing StateTransition artifacts: %v", err)
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
			log.Fatalf("failed to export StateTransition verifier: %v", err)
		}
		statetransitionVkeySolFile = vkeySolFile
	}

	/*
		ResultsVerifier Circuit Compilation
	*/
	log.Infow("compiling ResultsVerifier circuit...")
	startTime = time.Now()
	// create final placeholder
	resultsverifierPlaceholder := &results.ResultsVerifierCircuit{}
	resultsverifierCCS, err := frontend.Compile(params.ResultsVerifierCurve.ScalarField(), r1cs.NewBuilder, resultsverifierPlaceholder)
	if err != nil {
		log.Fatalf("failed to compile ResultsVerifier circuit: %v", err)
	}
	logElapsed("ResultsVerifier circuit compiled", startTime)
	resultsverifierArtifacts, err := compileCircuitArtifacts(
		"ResultsVerifier",
		resultsverifierCCS,
		results.Artifacts,
		destination,
		force,
	)
	if err != nil {
		log.Fatalf("error processing ResultsVerifier artifacts: %v", err)
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
			log.Fatalf("failed to export ResultsVerifier: %v", err)
		}
		resultsverifierVkeySolFile = vkeySolFile
	} else {
		log.Infow("ResultsVerifier setup skipped; circuit unchanged")
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

type BallotProofArtifactsResult struct {
	CircuitHash      string
	ProvingKeyHash   string
	VerifyingKeyHash string
}

func processBallotProofArtifacts(destination string, force bool) (*BallotProofArtifactsResult, error) {
	startTime := time.Now()
	ballotProofWASMHash, err := circuits.HashBytesSHA256(ballotproof.CircomCircuitWasm)
	if err != nil {
		return nil, fmt.Errorf("hash BallotProof wasm: %w", err)
	}

	ballotProofPKHash, err := circuits.HashBytesSHA256(ballotproof.CircomProvingKey)
	if err != nil {
		return nil, fmt.Errorf("hash BallotProof proving key: %w", err)
	}

	ballotProofVKHash, err := circuits.HashBytesSHA256(ballotproof.CircomVerificationKey)
	if err != nil {
		return nil, fmt.Errorf("hash BallotProof verification key: %w", err)
	}

	fileNotExist := func(path string) bool {
		_, err := os.Stat(path)
		return errors.Is(err, os.ErrNotExist)
	}

	log.Infow("processing BallotProof circom artifacts...")
	if force ||
		fileNotExist(filepath.Join(destination, ballotProofWASMHash)) ||
		fileNotExist(filepath.Join(destination, ballotProofPKHash)) ||
		fileNotExist(filepath.Join(destination, ballotProofVKHash)) {
		if _, err := writeBytes(ballotproof.CircomCircuitWasm, destination); err != nil {
			return nil, fmt.Errorf("copy BallotProof wasm: %w", err)
		}

		if _, err := writeBytes(ballotproof.CircomProvingKey, destination); err != nil {
			return nil, fmt.Errorf("copy BallotProof proving key: %w", err)
		}

		if _, err = writeBytes(ballotproof.CircomVerificationKey, destination); err != nil {
			return nil, fmt.Errorf("copy BallotProof verification key: %w", err)
		}
	} else {
		log.Infow("skipping BallotProof artifact copy; artifacts already up-to-date")
	}

	log.Infow("BallotProof circom artifacts processed", "elapsed", time.Since(startTime).String())
	return &BallotProofArtifactsResult{
		CircuitHash:      ballotProofWASMHash,
		ProvingKeyHash:   ballotProofPKHash,
		VerifyingKeyHash: ballotProofVKHash,
	}, nil
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

func exportSolidityVerifierFile(name string, artifacts *CompileCircuitArtifactsResult, destination string) (string, error) {
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
	newCircuitHash, err := circuits.HashConstraintSystem(ccs)
	if err != nil {
		return nil, fmt.Errorf("hash %s circuit: %w", circuitName, err)
	}
	log.Infow(fmt.Sprintf("%s circuit prepared", circuitName), "elapsed", time.Since(startTime).String(),
		"newCircuitHash", newCircuitHash,
		"oldCircuitHash", expectedCircuitHash)

	if newCircuitHash == expectedCircuitHash && !force {
		if err := artifacts.DownloadVerifyingKey(context.Background()); err != nil {
			return nil, fmt.Errorf("download %s verifying key: %w", circuitName, err)
		}

		vk, err := loadVerifyingKeyFromHash(destination, expectedVerificationKeyHash, artifacts.Curve())
		if err != nil {
			return nil, fmt.Errorf("load existing %s vk %s: %w", circuitName, expectedVerificationKeyHash, err)
		}
		log.Infow(fmt.Sprintf("%s setup skipped; using existing vk from destination", circuitName), "VerifyingKeyHash", expectedVerificationKeyHash)
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
func writeCS(cs constraint.ConstraintSystem, destDir string) (string, error) {
	return writeToFile(destDir, func(w io.Writer) error {
		_, err := cs.WriteTo(w)
		return err
	})
}

// writePK writes the Proving Key to a file and returns its SHA256 hash
func writePK(pk groth16.ProvingKey, destDir string) (string, error) {
	return writeToFile(destDir, func(w io.Writer) error {
		_, err := pk.WriteTo(w)
		return err
	})
}

// writeVK writes the Verifying Key to a file and returns its SHA256 hash
func writeVK(vk groth16.VerifyingKey, destDir string) (string, error) {
	return writeToFile(destDir, func(w io.Writer) error {
		_, err := vk.WriteTo(w)
		return err
	})
}

// writeBytes writes content to a file and returns its SHA256 hash
func writeBytes(content []byte, destDir string) (string, error) {
	return writeToFile(destDir, func(w io.Writer) error {
		_, err := w.Write(content)
		return err
	})
}

// writeToFile handles efficient writing to a file and computing its SHA256 hash
// Returns the hash of the written content
func writeToFile(destDir string, writeFunc func(w io.Writer) error) (string, error) {
	// Create a hash writer
	hashFn := sha256.New()

	// Create a temp file for writing
	tempFile, err := os.CreateTemp(destDir, "temp-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file in %s: %w", destDir, err)
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
	finalFilename := filepath.Join(destDir, hash)

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
