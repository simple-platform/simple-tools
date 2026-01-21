package scaffold

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"simple-cli/internal/build"
)

// checkSCLEntityMatchType uses scl-parser CLI to check if a specific entity with a specific name/type exists in an SCL file.
// It is defined as a package-level variable (rather than a regular function) so tests can
// replace it with a stub or mock implementation when needed.
var checkSCLEntityMatchType = func(filePath string, entityName string, entityType string, blockKey string) (bool, error) {
	// check if scl-parser is installed and get path
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return false, fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	cmd := exec.Command(parserPath, filePath)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return false, fmt.Errorf("scl-parser failed for %s: %s", filePath, string(exitErr.Stderr))
		}
		return false, fmt.Errorf("failed to run scl-parser: %w", err)
	}

	var blocks []map[string]interface{}
	if err := json.Unmarshal(output, &blocks); err != nil {
		return false, fmt.Errorf("failed to parse scl-parser output: %w", err)
	}

	for _, block := range blocks {
		if matchesEntity(block, blockKey, entityType, entityName) {
			return true, nil
		}
	}

	return false, nil
}

// matchesEntity checks if a parsed SCL block matches the specified key, type, and name.
//
// It supports two shapes of the "name" field produced by scl-parser:
//
//  1. "set type, name" blocks where block["name"] is a list, e.g. ["type", "name"].
//     This pattern is produced by SCL statements such as:
//     set dev_simple_system.logic, action_name
//     which scl-parser reports as:
//     "key":  "set"
//     "name": ["dev_simple_system.logic", "action_name"]
//     In this case, entityType must match the first element ("dev_simple_system.logic")
//     and entityName must match the second element ("action_name").
//
//  2. Simple declaration blocks where block["name"] is a string, e.g. "user".
//     This pattern is produced by SCL statements such as:
//     table user
//     which scl-parser reports as:
//     "key":  "table"
//     "name": "user"
//     For these blocks, only entityName is matched, and only when entityType is empty.
//     If a non-empty entityType is provided, string "name" blocks are not considered
//     matches by this helper.
func matchesEntity(block map[string]interface{}, blockKey, entityType, entityName string) bool {
	if block["type"] != "block" {
		return false
	}

	key, ok := block["key"].(string)
	if !ok || key != blockKey {
		return false
	}

	nameVal := block["name"]

	// Case 1: 'set type, name' -> nameVal is ["type", "name"]
	if nameList, ok := nameVal.([]interface{}); ok {
		if len(nameList) >= 2 {
			typeStr, okType := nameList[0].(string)
			nameStr, okName := nameList[1].(string)
			if okType && okName && typeStr == entityType && nameStr == entityName {
				return true
			}
		}
		return false
	}

	// Case 2: Simple block 'table user' -> nameVal is "user"
	if nameStr, ok := nameVal.(string); ok {
		// If we are looking for a specific type (e.g. "set"), a string name doesn't match
		// unless entityType is empty (generic block match)
		if entityType == "" && nameStr == entityName {
			return true
		}
	}

	return false
}
