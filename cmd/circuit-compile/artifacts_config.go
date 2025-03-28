package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
)

// UpdateCircuitArtifactsConfig updates the hash constants in the circuit_artifacts.go file
// with the values from the provided hash list
func UpdateCircuitArtifactsConfig(hashList map[string]string, configPath string) error {
	// Ensure the config path is an absolute path
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for config: %w", err)
	}

	// Check if the config file exists
	if _, err := os.Stat(absConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("config file does not exist at path: %s", absConfigPath)
	}

	// Read the file content
	content, err := os.ReadFile(absConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Create a modified copy of the content
	modifiedContent := string(content)

	// Update hash constants in the file
	for constName, newHash := range hashList {
		// Create a regex pattern to match the constant declaration
		pattern := fmt.Sprintf(`(%s\s*=\s*")([a-f0-9]+)(")`, constName)
		re := regexp.MustCompile(pattern)

		// Check if the pattern exists in the content
		if !re.MatchString(modifiedContent) {
			log.Warnw("pattern not found in config file", "constant", constName)
			continue
		}

		// Replace the hash value
		modifiedContent = re.ReplaceAllString(modifiedContent, "${1}"+newHash+"${3}")
		log.Infow("updated hash constant", "constant", constName, "old_hash", re.FindStringSubmatch(string(content))[2], "new_hash", newHash)
	}

	// Don't write the file if no changes were made
	if modifiedContent == string(content) {
		log.Infow("no changes needed for config file", "path", absConfigPath)
		return nil
	}

	// Write the modified content back to the file
	if err := os.WriteFile(absConfigPath, []byte(modifiedContent), 0644); err != nil {
		return fmt.Errorf("failed to write updated config file: %w", err)
	}

	log.Infow("circuit artifacts config updated successfully", "path", absConfigPath)
	return nil
}

// CheckHashChanges compares new hashes with existing ones in the config file
// and returns a list of constants that would be updated
func CheckHashChanges(hashList map[string]string, configPath string) ([]string, error) {
	// Read the file content
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Track changes
	changes := []string{}

	// Check each hash constant
	for constName, newHash := range hashList {
		// Create a regex pattern to match the constant declaration
		pattern := fmt.Sprintf(`%s\s*=\s*"([a-f0-9]+)"`, constName)
		re := regexp.MustCompile(pattern)

		// Find the current hash value
		matches := re.FindStringSubmatch(string(content))
		if len(matches) < 2 {
			log.Warnw("constant not found in config file", "constant", constName)
			continue
		}

		currentHash := matches[1]
		if currentHash != newHash {
			changes = append(changes, fmt.Sprintf("%s: %s -> %s", constName, currentHash, newHash))
		}
	}

	return changes, nil
}

// FindCircuitArtifactsFile attempts to find the circuit_artifacts.go file
// Returns the path if found, an error otherwise
func FindCircuitArtifactsFile() (string, error) {
	// Try the default location
	defaultPath := "config/circuit_artifacts.go"
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	}

	// Try to find it by walking through project directories
	var configPath string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "circuit_artifacts.go" {
			// Check if the file contains the expected content
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil // Continue searching
			}
			if bytes.Contains(content, []byte("VoteVerifierCircuitHash")) {
				configPath = path
				return filepath.SkipAll // Stop the walk
			}
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("error searching for circuit_artifacts.go: %w", err)
	}

	if configPath == "" {
		return "", fmt.Errorf("circuit_artifacts.go file not found")
	}

	return configPath, nil
}
