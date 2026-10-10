package build

import (
	"context"
	"fmt"
	"net/http"
	"simple-cli/internal/selfupdate"
	"strings"
	"time"
)

const (
	SCLParserName               = "scl-parser"
	SCLParserReleaseURLTemplate = "https://github.com/simple-platform/simple-tools/releases/download/v%s-scl-parser-cli/scl-parser-%s"

	// sclParserTagSuffix is what follows the version in the tag of a release
	// of scl-parser, as in v1.0.3-scl-parser-cli.
	sclParserTagSuffix = "-scl-parser-cli"

	// sclParserLookupTimeout bounds the question of which version is newest.
	sclParserLookupTimeout = 30 * time.Second
)

// sclParserReleases is who is asked which releases exist. It is a variable so
// a test can answer in GitHub's place.
var sclParserReleases selfupdate.Doer = http.DefaultClient

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

// fetchSCLParserVersion answers with the newest released version of
// scl-parser.
//
// IT IS READ FROM THE REPOSITORY'S TAGS, as the CLI's own newest version is.
// It used to be read from the version written in the tool's mix.exs on main,
// which was the newest release only for as long as each release wrote its
// number there. Releases no longer commit anything back: the tag is the
// version of record, and the number in that file stays where the last release
// of the old kind left it.
func fetchSCLParserVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sclParserLookupTimeout)
	defer cancel()

	version, _, err := selfupdate.NewestTagged(ctx, sclParserReleases, "simple-cli", sclParserTagSuffix)
	if err != nil {
		return "", fmt.Errorf("failed to find the newest scl-parser: %w", err)
	}

	return version.String(), nil
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

func NormalizeActionName(dirName string) string {
	return strings.ReplaceAll(dirName, "-", "_")
}
