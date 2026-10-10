// Package selfupdate finds the newest release of the CLI on GitHub and replaces
// the running program with it.
//
// A release is a tag on this repository, `v<version>-simple-cli`, with one file
// for each machine the CLI is built for and one file of checksums beside them.
// Nothing else says which version is the newest: no file in the tree carries a
// version number, so the tags are read.
package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a released version of the CLI: three numbers, as in 1.4.0.
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion reads a version written as 1.4.0 or v1.4.0.
//
// Anything else is not a release. A program built from source carries the word
// "dev" where its version goes, and that has to read as "no version" rather
// than as a very old one, or every source build would be told to update.
func ParseVersion(text string) (Version, bool) {
	parts := strings.Split(strings.TrimPrefix(text, "v"), ".")
	if len(parts) != 3 {
		return Version{}, false
	}

	var numbers [3]int
	for i, part := range parts {
		number, ok := parseNumber(part)
		if !ok {
			return Version{}, false
		}
		numbers[i] = number
	}

	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, true
}

// parseNumber reads one part of a version: digits only, so "1", and neither
// "+1" nor "1-rc" nor an empty part.
func parseNumber(part string) (int, bool) {
	if part == "" || strings.Trim(part, "0123456789") != "" {
		return 0, false
	}

	number, err := strconv.Atoi(part)
	if err != nil {
		return 0, false
	}

	return number, true
}

// String writes the version the way a tag and a user write it: 1.4.0.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// NewerThan reports whether v was released after other.
func (v Version) NewerThan(other Version) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}

	return v.Patch > other.Patch
}
