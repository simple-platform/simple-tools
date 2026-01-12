package build

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
)

const (
	SCLParserName               = "scl-parser"
	SCLParserReleaseURLTemplate = "https://github.com/simple-platform/simple-tools/releases/download/v%s-scl-parser-cli/scl-parser-%s"
)

var (
	SCLParserMixExsURL = "https://raw.githubusercontent.com/simple-platform/simple-tools/main/tools/scl_parser_cli/mix.exs"
)

type ActionConfig struct {
	Name                 string `json:"name"`
	ExecutionEnvironment string `json:"execution_environment"`
	Language             string `json:"language"`
	Timeout              int    `json:"timeout,omitempty"`
}

func EnsureSCLParser(onStatus func(string)) (string, error) {
	def := ToolDef{
		Name:           SCLParserName,
		CheckVersionFn: fetchSCLParserVersion,
		DownloadURLFn:  buildSCLParserDownloadURL,
		PostDownloadFn: nil,
		OnStatus:       onStatus,
	}
	return EnsureTool(def)
}

func fetchSCLParserVersion() (string, error) {
	resp, err := http.Get(SCLParserMixExsURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch mix.exs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status fetching mix.exs: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read mix.exs: %w", err)
	}

	return extractVersionFromMixExs(string(body))
}

func extractVersionFromMixExs(content string) (string, error) {
	re := regexp.MustCompile(`version:\s*"([^"]+)"`)
	matches := re.FindStringSubmatch(content)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not find version in mix.exs")
	}
	return matches[1], nil
}

func buildSCLParserDownloadURL(version string) string {
	platform := getSCLParserPlatform()
	return fmt.Sprintf(SCLParserReleaseURLTemplate, version, platform)
}

func getSCLParserPlatform() string {
	return mapSCLPlatform(GetPlatform(), GetArch())
}

func mapSCLPlatform(platform, arch string) string {
	switch {
	case platform == "macos" && arch == "aarch64":
		return "macos-silicon"
	case platform == "macos":
		return "macos"
	case platform == "linux" && arch == "aarch64":
		return "linux-arm64"
	case platform == "linux":
		return "linux"
	case platform == "windows":
		return "windows.exe"
	default:
		return platform
	}
}

func ParseActionSCL(parserPath, sclFilePath string) (*ActionConfig, error) {
	cmd := exec.Command(parserPath, "parse", "--file", sclFilePath, "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("parser failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run parser: %w", err)
	}

	var config ActionConfig
	if err := json.Unmarshal(output, &config); err != nil {
		return nil, fmt.Errorf("failed to parse parser output: %w", err)
	}

	return &config, nil
}

func FindActionSCLFile(appDir, actionName string) (string, error) {
	// Placeholder until strict structure is enforced or search implemented
	// For now assume standard location if needed, but this was a TODO
	return "", fmt.Errorf("SCL file discovery not yet implemented")
}

func NormalizeActionName(dirName string) string {
	return strings.ReplaceAll(dirName, "-", "_")
}
