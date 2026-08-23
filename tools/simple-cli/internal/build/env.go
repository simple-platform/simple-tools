package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"simple-cli/internal/fsx"
	"simple-cli/internal/home"
	"slices"
	"strings"
)

// SCL-parser outputs JSON AST in this format:
// [{"key": "set", "name": [...], "children": [...], "type": "block"}]
type sclBlock struct {
	Type     string     `json:"type"`
	Key      string     `json:"key"`
	Children []sclChild `json:"children"`
}

type sclChild struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// ParseExecutionEnvironment uses scl-parser CLI to extract execution_environment
func ParseExecutionEnvironment(sclParserPath, actionDir string) (string, error) {
	// SCL file is at apps/<app>/records/10_actions.scl
	// actionDir is apps/<app>/actions/<action>/
	appDir := filepath.Dir(filepath.Dir(actionDir))
	actionName := filepath.Base(actionDir)
	sclPath := filepath.Join(appDir, "records", "10_actions.scl")
	if _, err := os.Stat(sclPath); os.IsNotExist(err) {
		return "server", nil // default
	}

	cmd := home.ToolCommand(sclParserPath, sclPath)
	output, err := cmd.Output()
	if err != nil {
		return "server", nil // fallback on parse error
	}

	var blocks []sclBlock
	if err := json.Unmarshal(output, &blocks); err != nil {
		return "server", nil
	}

	// Find execution_environment in the correct set block
	for _, block := range blocks {
		if block.Key == "set" {
			// Check if this block corresponds to the current action
			// We must check the "name" property inside the block children
			isTargetAction := false
			for _, child := range block.Children {
				if child.Key == "name" {
					if val, ok := child.Value.(string); ok && val == actionName {
						isTargetAction = true
						break
					}
				}
			}

			if isTargetAction {
				for _, child := range block.Children {
					if child.Key == "execution_environment" {
						if str, ok := child.Value.(string); ok {
							return str, nil
						}
					}
				}
				// Default to server if name matches but no env specified
				return "server", nil
			}
		}
	}
	return "server", nil
}

// ActionLanguage is the language an action is written in. It is what the build
// path switches on to pick a compiler, so there is one of these per toolchain
// rather than per file extension.
type ActionLanguage string

const (
	LanguageTypeScript ActionLanguage = "typescript"
	LanguageGo         ActionLanguage = "go"
	LanguageRust       ActionLanguage = "rust"
)

// actionSources is every file that is an action's source, paired with the
// language it proves and the name that language is called by in a message.
//
// It is one list because two questions are asked of it and they must not drift
// apart: DetectActionLanguage asks which language a directory holds, and
// hasActionSource asks the shorter question of whether it holds one at all. A
// language that only the first of those knew about would be buildable and
// invisible at the same time — recognised by the compiler, and never handed to
// it, because the directory was not counted as an action in the first place.
//
// The order is the order the files are looked for, and so the order they are
// named back in an ambiguity.
var actionSources = []struct {
	rel     string
	lang    ActionLanguage
	display string
}{
	{"src/index.ts", LanguageTypeScript, "TypeScript"},
	{"index.ts", LanguageTypeScript, "TypeScript"},
	{"src/main.rs", LanguageRust, "Rust"},
	{"main.go", LanguageGo, "Go"},
}

// hasActionSource reports whether a directory holds any action's source.
//
// This is deliberately weaker than DetectActionLanguage: an ambiguous directory
// holding two languages still has a source, and has to be recognised so the
// build can refuse it by name rather than pass over it in silence.
func hasActionSource(actionDir string) bool {
	for _, src := range actionSources {
		if fileExists(filepath.Join(actionDir, filepath.FromSlash(src.rel))) {
			return true
		}
	}
	return false
}

// DetectActionLanguage answers which language an action is written in.
//
// The answer is read off the source file that is actually in the directory,
// never off a manifest. A Cargo.toml or a package.json says which dependencies
// a directory pulls in and can be dropped there by a tool that wrote none of
// the action; the file the compiler is pointed at is the action.
//
// THIS IS THE ONLY ANSWER. The metadata path asked the same question of a
// second reader that knew two of the three languages, so a Rust action was a
// Rust action to the compiler and a directory with no source to the thing that
// describes it — built, and then left with no action.json, on a build that
// reported success. One reading means the two halves cannot disagree about what
// they are looking at.
//
// Two of them present is refused rather than settled by precedence. Whichever
// one won, the other would be compiled by nothing at all and the developer
// would hear about it only when the deployed action behaved like the source
// they were not editing. None present is refused for the reason it would fail
// anyway, only sooner, and naming what was looked for.
func DetectActionLanguage(actionDir string) (ActionLanguage, error) {
	return detectActionLanguage(fsx.OSFileSystem{}, actionDir)
}

// detectActionLanguage is the reading itself, over whichever filesystem the
// caller is working through.
//
// The exported form above is what the build calls, and it supplies the real
// one. The parameter exists because the metadata path is written against an
// injectable filesystem end to end, and a detector that reached past it would
// answer from a directory its caller was not describing.
func detectActionLanguage(fsys fsx.FileSystem, actionDir string) (ActionLanguage, error) {
	var found []ActionLanguage
	var names []string

	for _, src := range actionSources {
		if _, err := fsys.Stat(filepath.Join(actionDir, filepath.FromSlash(src.rel))); err != nil {
			continue
		}
		// index.ts and src/index.ts are two spellings of one answer, not two
		// languages, so a language already named is not named twice.
		if slices.Contains(found, src.lang) {
			continue
		}
		found = append(found, src.lang)
		names = append(names, fmt.Sprintf("%s (%s)", src.rel, src.display))
	}

	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("no action source found in %s: expected src/index.ts or index.ts (TypeScript), src/main.rs (Rust), or main.go (Go)",
			filepath.Base(actionDir))
	default:
		return "", fmt.Errorf("cannot tell which language %s is written in: found %s. An action is written in one language, so remove the source that does not belong to it",
			filepath.Base(actionDir), strings.Join(names, " and "))
	}
}
