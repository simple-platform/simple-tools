package main

import (
	"testing"
)

func TestContainsLine(t *testing.T) {
	content := ".DS_Store\ntmp/\n"
	if !containsLine(content, ".DS_Store") {
		t.Error("Should match existing line")
	}
	if !containsLine(content, "tmp/") {
		t.Error("Should match existing line with newline")
	}
	if containsLine(content, "node_modules/") {
		t.Error("Should not match missing line")
	}
	if containsLine(content, "tmp") {
		// Strict matching: "tmp" shouldn't match "tmp/"
		t.Error("Should verify strict matching")
	}
}

// Note: Testing main() integration is harder without refactoring main to be testable or using exec.Command
// For now we trust unit test of helper and manual verification if needed.
