package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"simple-cli/internal/agents"
	"simple-cli/internal/build"
	"simple-cli/internal/config"
	"simple-cli/internal/fsx"
)

func defaultLintSCLParser() (config.SCLParser, error) {
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return nil, err
	}
	return &config.DefaultSCLParser{ParserPath: parserPath}, nil
}

type parsedSCLFile struct {
	path   string
	data   []byte
	blocks []config.SCLBlock
}

type sclBlockLocation struct {
	path string
	line int
}

// checkSCLWorkspace reads and parses each source file before applying record checks.
func checkSCLWorkspace(fsys fsx.FileSystem, root string, scanRoots, appDirs []string, parser config.SCLParser) ([]string, error) {
	if parser == nil {
		return nil, fmt.Errorf("SCL parser is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace root: %w", err)
	}
	files := make(map[string]struct{})
	for _, scanRoot := range scanRoots {
		if !filepath.IsAbs(scanRoot) {
			scanRoot = filepath.Join(root, scanRoot)
		}
		if err := collectSCLFiles(fsys, scanRoot, files); err != nil {
			return nil, fmt.Errorf("failed to find SCL files under %s: %w", relativeLintPath(root, scanRoot), err)
		}
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var findings []string
	parsedFiles := make([]parsedSCLFile, 0, len(paths))
	for _, path := range paths {
		data, err := fsys.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", relativeLintPath(root, path), err)
		}
		blocks, err := parser.Parse(path)
		if err != nil {
			findings = append(findings, formatSCLParseFinding(root, path, err))
			continue
		}
		parsedFiles = append(parsedFiles, parsedSCLFile{path: path, data: data, blocks: blocks})
	}
	findings = append(findings, repeatedSCLBlockFindings(root, appDirs, parsedFiles)...)

	for _, appDir := range appDirs {
		appID := appIDFromSCLAppDir(root, appDir)
		if appID == "" {
			return nil, fmt.Errorf("SCL record checks require an app directory under %s/apps: %s", root, appDir)
		}
		problems, err := agents.CheckWithParser(fsys, root, appID, func(path string) ([]byte, error) {
			blocks, err := parser.Parse(path)
			if err != nil {
				return nil, err
			}
			data, err := json.Marshal(blocks)
			if err != nil {
				return nil, fmt.Errorf("failed to encode parser output for %s: %w", path, err)
			}
			return data, nil
		})
		if err != nil {
			findings = append(findings, fmt.Sprintf("apps/%s/records:0: failed to run build record checks: %v", appID, err))
			continue
		}
		for _, problem := range problems {
			findings = append(findings, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(problem.File), problem.Line, problem.Rule))
		}
	}
	sort.Strings(findings)
	return findings, nil
}

func collectSCLFiles(fsys fsx.FileSystem, root string, files map[string]struct{}) error {
	entries, err := fsys.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				continue
			}
			if err := collectSCLFiles(fsys, path, files); err != nil {
				return err
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".scl") {
			files[path] = struct{}{}
		}
	}
	return nil
}

func formatSCLParseFinding(root, path string, err error) string {
	line, message := sclParserError(err)
	if line == 0 {
		return fmt.Sprintf("%s:0: parser did not report a line: %s", relativeLintPath(root, path), message)
	}
	return fmt.Sprintf("%s:%d: %s", relativeLintPath(root, path), line, message)
}

func sclParserError(err error) (int, string) {
	message := strings.TrimSpace(err.Error())
	const prefix = "scl-parser failed:"
	if strings.HasPrefix(message, prefix) {
		output := strings.TrimSpace(strings.TrimPrefix(message, prefix))
		var detail struct {
			Error string `json:"error"`
			Line  int    `json:"line"`
		}
		if json.Unmarshal([]byte(output), &detail) == nil && detail.Error != "" {
			return detail.Line, detail.Error
		}
		return 0, output
	}
	return 0, message
}

func repeatedSCLBlockFindings(root string, appDirs []string, files []parsedSCLFile) []string {
	byDefinition := make(map[string][]sclBlockLocation)
	definitionNames := make(map[string]string)
	for _, file := range files {
		appDir := containingSCLApp(root, appDirs, file.path)
		if appDir == "" {
			continue
		}
		occurrences := make(map[string]int)
		for _, block := range file.blocks {
			if block.Type != "block" || !hasSCLBlockName(block.Name) {
				continue
			}
			encodedName, err := json.Marshal(block.Name)
			if err != nil {
				continue
			}
			definition := appDir + "\x00" + block.Key + "\x00" + string(encodedName)
			occurrence := occurrences[definition]
			occurrences[definition] = occurrence + 1
			byDefinition[definition] = append(byDefinition[definition], sclBlockLocation{
				path: relativeLintPath(root, file.path), line: sclBlockLine(file, block, occurrence),
			})
			definitionNames[definition] = fmt.Sprint(block.Name)
		}
	}

	var findings []string
	for definition, locations := range byDefinition {
		if len(locations) < 2 {
			continue
		}
		parts := strings.Split(definition, "\x00")
		kind := parts[1]
		for _, location := range locations {
			findings = append(findings, fmt.Sprintf("%s:%d: duplicate block name: kind %q name %q is defined more than once", location.path, location.line, kind, definitionNames[definition]))
		}
	}
	return findings
}

func hasSCLBlockName(name any) bool {
	switch value := name.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	default:
		return true
	}
}

func sclBlockLine(file parsedSCLFile, block config.SCLBlock, occurrence int) int {
	var pattern *regexp.Regexp
	switch name := block.Name.(type) {
	case string:
		pattern = regexp.MustCompile(`^\s*` + regexp.QuoteMeta(block.Key) + `\s+["']?` + regexp.QuoteMeta(name) + `["']?\s*\{`)
	case []any:
		if block.Key == "set" && len(name) >= 2 {
			pattern = regexp.MustCompile(`^\s*set\s+["']?` + regexp.QuoteMeta(fmt.Sprint(name[0])) + `["']?\s*,\s*["']?` + regexp.QuoteMeta(fmt.Sprint(name[1])) + `["']?\s*\{`)
		}
	}
	if pattern == nil {
		return block.NameLoc.Line
	}
	match := 0
	for index, line := range strings.Split(string(file.data), "\n") {
		if pattern.MatchString(line) {
			if match == occurrence {
				return index + 1
			}
			match++
		}
	}
	return block.NameLoc.Line
}

func containingSCLApp(root string, appDirs []string, path string) string {
	for _, appDir := range appDirs {
		if !filepath.IsAbs(appDir) {
			appDir = filepath.Join(root, appDir)
		}
		appDir = filepath.Clean(appDir)
		rel, err := filepath.Rel(appDir, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return appDir
		}
	}
	return ""
}

func appIDFromSCLAppDir(root, appDir string) string {
	if !filepath.IsAbs(appDir) {
		appDir = filepath.Join(root, appDir)
	}
	appsRoot := filepath.Join(root, "apps")
	rel, err := filepath.Rel(appsRoot, filepath.Clean(appDir))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func relativeLintPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
