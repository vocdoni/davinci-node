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
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/circuits/statetransition"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types/params"
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
	s3Config := NewDefaultS3Config()

	// Define flags
	flag.StringVar(&destination, "destination", "artifacts", "destination folder for the artifacts")
	flag.BoolVar(&updateConfig, "update-config", false, "update circuit_artifacts.go file with new hashes")
	flag.StringVar(&configPath, "config-path", "", "path to circuit_artifacts.go file (auto-detected if not specified)")
	flag.BoolVar(&updateWasm, "update-wasm", false, "compile and update WASM files only")

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
	// Vote Verifier Circuit Compilation
	////////////////////////////////////////
	startTime := time.Now()
	log.Infow("compiling vote verifier circuit...")
	// generate the placeholders for the recursion
	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(
		ballottest.TestCircomVerificationKey, circuits.BallotProofNPubInputs)
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

	// Setup Vote Verifier circuit
	voteVerifierPk, voteVerifierVk, err := groth16.Setup(voteVerifierCCS)
	if err != nil {
		log.Fatalf("error setting up vote verifier circuit: %v", err)
	}
	log.Infow("vote verifier circuit compiled", "elapsed", time.Since(startTime).String())

	// Write the vote verifier artifacts to disk
	startTime = time.Now()
	log.Infow("writing vote verifier artifacts to disk...")
	hash, err := writeCS(voteVerifierCCS, destination)
	if err != nil {
		log.Fatalf("error writing vote verifier constraint system: %v", err)
	}
	hashList["VoteVerifierCircuitHash"] = hash

	hash, err = writePK(voteVerifierPk, destination)
	if err != nil {
		log.Fatalf("error writing vote verifier proving key: %v", err)
	}
	hashList["VoteVerifierProvingKeyHash"] = hash

	hash, err = writeVK(voteVerifierVk, destination)
	if err != nil {
		log.Fatalf("error writing vote verifier verifying key: %v", err)
	}
	hashList["VoteVerifierVerificationKeyHash"] = hash

	log.Infow("vote verifier artifacts written to disk", "elapsed", time.Since(startTime).String())

	////////////////////////////////////////
	// Aggregate Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling aggregator circuit...")
	startTime = time.Now()
	voteVerifierFixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bls12377.G1Affine, sw_bls12377.G2Affine, sw_bls12377.GT](voteVerifierVk)
	if err != nil {
		log.Fatalf("failed to fix vote verifier verification key: %v", err)
	}
	// create final placeholder
	aggregatePlaceholder := &aggregator.AggregatorCircuit{
		Proofs:          [params.VotesPerBatch]stdgroth16.Proof[sw_bls12377.G1Affine, sw_bls12377.G2Affine]{},
		VerificationKey: voteVerifierFixedVk,
	}
	for i := range params.VotesPerBatch {
		aggregatePlaceholder.Proofs[i] = stdgroth16.PlaceholderProof[sw_bls12377.G1Affine, sw_bls12377.G2Affine](voteVerifierCCS)
	}

	aggregateCCS, err := frontend.Compile(params.AggregatorCurve.ScalarField(), r1cs.NewBuilder, aggregatePlaceholder)
	if err != nil {
		log.Fatalf("failed to compile aggregator circuit: %v", err)
	}
	// Setup Aggregator circuit
	aggregatePk, aggregateVk, err := groth16.Setup(aggregateCCS)
	if err != nil {
		log.Fatalf("error setting up aggregator circuit: %v", err)
	}
	log.Infow("aggregator circuit compiled", "elapsed", time.Since(startTime).String())

	// Write the aggregator artifacts to disk
	startTime = time.Now()
	log.Infow("writing aggregator artifacts to disk...")
	hash, err = writeCS(aggregateCCS, destination)
	if err != nil {
		log.Fatalf("error writing aggregator constraint system: %v", err)
	}
	hashList["AggregatorCircuitHash"] = hash

	hash, err = writePK(aggregatePk, destination)
	if err != nil {
		log.Fatalf("error writing aggregator proving key: %v", err)
	}
	hashList["AggregatorProvingKeyHash"] = hash

	hash, err = writeVK(aggregateVk, destination)
	if err != nil {
		log.Fatalf("error writing aggregator verifying key: %v", err)
	}
	hashList["AggregatorVerificationKeyHash"] = hash

	log.Infow("aggregator artifacts written to disk", "elapsed", time.Since(startTime).String())

	////////////////////////////////////////
	// Statetransition Circuit Compilation
	////////////////////////////////////////
	log.Infow("compiling statetransition circuit...")
	startTime = time.Now()
	aggregatorFixedVk, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](aggregateVk)
	if err != nil {
		log.Fatalf("failed to fix vote verifier verification key: %v", err)
	}
	// create final placeholder
	statetransitionPlaceholder := &statetransition.StateTransitionCircuit{
		AggregatorProof: stdgroth16.PlaceholderProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](aggregateCCS),
		AggregatorVK:    aggregatorFixedVk,
	}
	statetransitionCCS, err := frontend.Compile(params.StateTransitionCurve.ScalarField(), r1cs.NewBuilder, statetransitionPlaceholder)
	if err != nil {
		log.Fatalf("failed to compile statetransition circuit: %v", err)
	}
	// Setup statetransition circuit
	statetransitionPk, statetransitionVk, err := groth16.Setup(statetransitionCCS)
	if err != nil {
		log.Fatalf("error setting up statetransition circuit: %v", err)
	}
	log.Infow("statetransition circuit compiled", "elapsed", time.Since(startTime).String())

	// Write the statetransition artifacts to disk
	startTime = time.Now()
	log.Infow("writing statetransition artifacts to disk...")
	hash, err = writeCS(statetransitionCCS, destination)
	if err != nil {
		log.Fatalf("error writing statetransition constraint system: %v", err)
	}
	hashList["StateTransitionCircuitHash"] = hash

	hash, err = writePK(statetransitionPk, destination)
	if err != nil {
		log.Fatalf("error writing statetransition proving key: %v", err)
	}
	hashList["StateTransitionProvingKeyHash"] = hash

	hash, err = writeVK(statetransitionVk, destination)
	if err != nil {
		log.Fatalf("error writing statetransition verifying key: %v", err)
	}
	hashList["StateTransitionVerificationKeyHash"] = hash

	log.Infow("statetransition artifacts written to disk", "elapsed", time.Since(startTime).String())

	/*
		Export the state transition solidity verifier
	*/
	log.Infow("exporting state transition solidity verifier...")
	// Cast vk to bn254 VerifyingKey and force precomputation (not sure if necessary).
	statetransitionSolidityVk := statetransitionVk.(*groth16_bn254.VerifyingKey)
	if err := statetransitionSolidityVk.Precompute(); err != nil {
		log.Fatalf("failed to precompute vk: %v", err)
	}
	statetransitionVkeySolFile := path.Join(destination, "statetransition_vkey.sol")
	fd, err := os.Create(statetransitionVkeySolFile)
	if err != nil {
		log.Fatalf("failed to create statetransition_vkey.sol: %v", err)
	}
	buf := bytes.NewBuffer(nil)
	if err := statetransitionSolidityVk.ExportSolidity(buf, solidity.WithPragmaVersion("^0.8.28")); err != nil {
		log.Fatalf("failed to export vk to Solidity: %v", err)
	}
	if _, err := fd.Write(buf.Bytes()); err != nil {
		log.Fatalf("failed to write statetransition_vkey.sol: %v", err)
	}
	if err := fd.Close(); err != nil {
		log.Warnw("failed to close statetransition_vkey.sol file", "error", err)
	}

	// Insert the proving key hash into the vkey.sol file
	if err := insertProvingKeyHashToVkeySolidity(statetransitionVkeySolFile, hashList["StateTransitionProvingKeyHash"]); err != nil {
		log.Warnw("failed to insert proving key hash into vkey.sol", "error", err)
	}
	log.Infow("statetransition_vkey.sol file created", "path", fd.Name())

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
	// Setup results verifier circuit
	resultsverifierPk, resultsverifierVk, err := groth16.Setup(resultsverifierCCS)
	if err != nil {
		log.Fatalf("error setting up results verifier circuit: %v", err)
	}
	log.Infow("results verifier circuit compiled", "elapsed", time.Since(startTime).String())

	// Write the results verifier artifacts to disk
	startTime = time.Now()
	log.Infow("writing results verifier artifacts to disk...")
	hash, err = writeCS(resultsverifierCCS, destination)
	if err != nil {
		log.Fatalf("error writing results verifier constraint system: %v", err)
	}
	hashList["ResultsVerifierCircuitHash"] = hash

	hash, err = writePK(resultsverifierPk, destination)
	if err != nil {
		log.Fatalf("error writing results verifier proving key: %v", err)
	}
	hashList["ResultsVerifierProvingKeyHash"] = hash

	hash, err = writeVK(resultsverifierVk, destination)
	if err != nil {
		log.Fatalf("error writing results verifier verifying key: %v", err)
	}
	hashList["ResultsVerifierVerificationKeyHash"] = hash

	log.Infow("results verifier artifacts written to disk", "elapsed", time.Since(startTime).String())

	/*
		Export the results verifier solidity verifier
	*/
	log.Infow("exporting results verifier solidity verifier...")
	// Cast vk to bn254 VerifyingKey and force precomputation (not sure if necessary).
	resultsverifierSolidityVk := resultsverifierVk.(*groth16_bn254.VerifyingKey)
	if err := resultsverifierSolidityVk.Precompute(); err != nil {
		log.Fatalf("failed to precompute vk: %v", err)
	}
	resultsverifierVkeySolFile := path.Join(destination, "resultsverifier_vkey.sol")
	fd, err = os.Create(resultsverifierVkeySolFile)
	if err != nil {
		log.Fatalf("failed to create resultsverifier_vkey.sol: %v", err)
	}
	buf = bytes.NewBuffer(nil)
	if err := resultsverifierSolidityVk.ExportSolidity(buf, solidity.WithPragmaVersion("^0.8.28")); err != nil {
		log.Fatalf("failed to export vk to Solidity: %v", err)
	}
	if _, err := fd.Write(buf.Bytes()); err != nil {
		log.Fatalf("failed to write resultsverifier_vkey.sol: %v", err)
	}
	if err := fd.Close(); err != nil {
		log.Warnw("failed to close resultsverifier_vkey.sol file", "error", err)
	}

	// Insert the proving key hash into the vkey.sol file
	if err := insertProvingKeyHashToVkeySolidity(resultsverifierVkeySolFile, hashList["ResultsVerifierProvingKeyHash"]); err != nil {
		log.Warnw("failed to insert proving key hash into resultsverifier_vkey.sol", "error", err)
	}
	log.Infow("resultsverifier_vkey.sol file created", "path", fd.Name())

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

		// copy the state transition solidity file to the config directory
		configDir := filepath.Dir(configPath)
		statetransitionSolidityFile := path.Join(configDir, "statetransition_vkey.sol")
		statetransitionSourceFile, err := os.Open(statetransitionVkeySolFile)
		if err != nil {
			log.Warnw("failed to open vkey.sol file", "error", err)
			return
		}
		defer func() {
			if err := statetransitionSourceFile.Close(); err != nil {
				log.Warnw("failed to close source vkey.sol file", "error", err)
			}
		}()
		statetransitionDestFile, err := os.Create(statetransitionSolidityFile)
		if err != nil {
			log.Warnw("failed to create destination vkey.sol file", "error", err)
			return
		}
		defer func() {
			if err := statetransitionDestFile.Close(); err != nil {
				log.Warnw("failed to close destination vkey.sol file", "error", err)
			}
		}()

		if _, err := io.Copy(statetransitionDestFile, statetransitionSourceFile); err != nil {
			log.Warnw("failed to copy vkey.sol file", "error", err)
			return
		}

		log.Infow("copied statetransition_vkey.sol file to config directory", "path", statetransitionSolidityFile)

		// copy the solidity file to the config directory
		resultsverifierSolidityFile := path.Join(configDir, "resultsverifier_vkey.sol")
		resultsverifierSourceFile, err := os.Open(resultsverifierVkeySolFile)
		if err != nil {
			log.Warnw("failed to open vkey.sol file", "error", err)
			return
		}
		defer func() {
			if err := resultsverifierSourceFile.Close(); err != nil {
				log.Warnw("failed to close source vkey.sol file", "error", err)
			}
		}()
		resultsverifierDestFile, err := os.Create(resultsverifierSolidityFile)
		if err != nil {
			log.Warnw("failed to create destination vkey.sol file", "error", err)
			return
		}
		defer func() {
			if err := resultsverifierDestFile.Close(); err != nil {
				log.Warnw("failed to close destination vkey.sol file", "error", err)
			}
		}()

		if _, err := io.Copy(resultsverifierDestFile, resultsverifierSourceFile); err != nil {
			log.Warnw("failed to copy vkey.sol file", "error", err)
			return
		}

		log.Infow("copied resultsverifier_vkey.sol file to config directory", "path", resultsverifierSolidityFile)
	}
}

// writeCS writes the Constraint System to a file and returns its SHA256 hash
func writeCS(cs constraint.ConstraintSystem, to string) (string, error) {
	return writeToFile(to, "ccs", func(w io.Writer) error {
		_, err := cs.WriteTo(w)
		return err
	})
}

// writePK writes the Proving Key to a file and returns its SHA256 hash
func writePK(pk groth16.ProvingKey, to string) (string, error) {
	return writeToFile(to, "pk", func(w io.Writer) error {
		_, err := pk.WriteTo(w)
		return err
	})
}

// writeVK writes the Verifying Key to a file and returns its SHA256 hash
func writeVK(vk groth16.VerifyingKey, to string) (string, error) {
	return writeToFile(to, "vk", func(w io.Writer) error {
		_, err := vk.WriteTo(w)
		return err
	})
}

// writeToFile handles efficient writing to a file and computing its SHA256 hash
// Returns the hash of the written content
func writeToFile(to, ext string, writeFunc func(w io.Writer) error) (string, error) {
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
		if err := tempFile.Close(); err != nil {
			log.Warnw("failed to close temp file", "error", err)
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
	finalFilename := filepath.Join(to, fmt.Sprintf("%s.%s", hash, ext))

	// Close the temp file before renaming
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

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

	// Create versioned filename based on baseFileName
	var versionedName string
	if filepath.Ext(baseFileName) != "" {
		// Has extension (e.g., "davinci_crypto.wasm" -> "davinci_crypto_ba1f.wasm")
		ext := filepath.Ext(baseFileName)
		nameWithoutExt := baseFileName[:len(baseFileName)-len(ext)]
		versionedName = fmt.Sprintf("%s_%s%s", nameWithoutExt, hashSuffix, ext)
	} else {
		// No extension
		versionedName = fmt.Sprintf("%s_%s", baseFileName, hashSuffix)
	}

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
