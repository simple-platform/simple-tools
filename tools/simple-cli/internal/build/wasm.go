package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	JavyName               = "javy"
	JavyVersion            = "8.1.0"
	JavyReleaseURLTemplate = "https://github.com/bytecodealliance/javy/releases/download/v%s/javy-%s-%s-v%s.gz"

	WasmOptName               = "wasm-opt"
	WasmOptVersion            = "125"
	WasmOptReleaseURLTemplate = "https://github.com/WebAssembly/binaryen/releases/download/version_%s/binaryen-version_%s-%s-%s.tar.gz"
)

func EnsureJavy(onStatus func(string)) (string, error) {
	def := ToolDef{
		Name: JavyName,
		CheckVersionFn: func() (string, error) {
			return JavyVersion, nil
		},
		DownloadURLFn:  buildJavyDownloadURL,
		PostDownloadFn: ExtractGzip,
		OnStatus:       onStatus,
	}
	return EnsureTool(def)
}

func buildJavyDownloadURL(version string) string {
	arch := mapJavyArch(GetArch())
	os := mapJavyOS(GetPlatform())
	return fmt.Sprintf(JavyReleaseURLTemplate, version, arch, os, version)
}

func mapJavyArch(arch string) string {
	switch arch {
	case "aarch64":
		return "arm"
	case "x86_64":
		return "x86_64"
	default:
		return arch
	}
}

func mapJavyOS(platform string) string {
	switch platform {
	case "macos":
		return "macos"
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return platform
	}
}

func EnsureWasmOpt(onStatus func(string)) (string, error) {
	def := ToolDef{
		Name: WasmOptName,
		CheckVersionFn: func() (string, error) {
			return WasmOptVersion, nil
		},
		DownloadURLFn:  buildWasmOptDownloadURL,
		PostDownloadFn: extractWasmOpt,
		OnStatus:       onStatus,
	}
	return EnsureTool(def)
}

func buildWasmOptDownloadURL(version string) string {
	platform := GetPlatform()
	archStr := GetArch()
	archOS := mapWasmOptArchOS(archStr, platform)
	return fmt.Sprintf(WasmOptReleaseURLTemplate, version, version, archOS.Arch, archOS.OS)
}

type archOSPair struct {
	Arch string
	OS   string
}

func mapWasmOptArchOS(arch, platform string) archOSPair {
	var result archOSPair

	switch platform {
	case "macos":
		result.OS = "macos"
	case "linux":
		result.OS = "linux"
	case "windows":
		result.OS = "windows"
	default:
		result.OS = platform
	}

	switch {
	case arch == "aarch64" && platform == "linux":
		result.Arch = "aarch64"
	case arch == "aarch64":
		result.Arch = "arm64"
	case arch == "x86_64":
		result.Arch = "x86_64"
	default:
		result.Arch = arch
	}

	return result
}

func extractWasmOpt(srcPath, destPath string) error {
	// destPath is .../bin/wasm-opt via EnsureTool -> GetToolsBinDir
	// We want to extract to .../ (the root tools dir) so that bin/wasm-opt and lib/ land in correct places.
	// destPath = ~/.simple/bin/wasm-opt
	// filepath.Dir(destPath) = ~/.simple/bin
	// filepath.Dir(filepath.Dir(destPath)) = ~/.simple

	rootDir := filepath.Dir(filepath.Dir(destPath))
	return ExtractTarGz(srcPath, rootDir, 1)
}

// InstallRustURL is where a developer with no Rust toolchain is sent. It is
// named once so that every refusal below points at the same place.
const InstallRustURL = "https://rustup.rs"

// EnsureCargo locates the cargo that compiles a Rust action.
//
// javy and wasm-opt are fetched into ~/.simple because they are build tooling
// nobody installs on purpose. A Rust toolchain is the opposite: it is the
// developer's own installation, pinned to a channel they chose, and it is what
// `simple test` already runs their tests with. Downloading a second one beside
// it would compile the shipped artifact with a compiler they never tested
// against, so this only looks — and when there is nothing to find it says so in
// the one sentence that ends with the fix.
func EnsureCargo() (string, error) {
	path, err := exec.LookPath("cargo")
	if err != nil {
		return "", fmt.Errorf("cargo was not found on PATH, and this action is written in Rust. Install a Rust toolchain (%s), then build again", InstallRustURL)
	}
	return path, nil
}

// EnsureRustWasmTarget checks that the standard library for RustWasmTarget is
// installed, which is a separate thing from having a Rust toolchain at all.
//
// Without it cargo fails deep in the build with "can't find crate for `std`"
// repeated once per dependency, which reads like a broken action rather than a
// missing component. Refusing here turns that into the one command that fixes
// it.
func EnsureRustWasmTarget() error {
	rustc, err := exec.LookPath("rustc")
	if err != nil {
		return fmt.Errorf("rustc was not found on PATH, and this action is written in Rust. Install a Rust toolchain (%s), then build again", InstallRustURL)
	}

	// rustc prints where the target's library directory *would* be whether or
	// not anyone has installed it, so the exit status and the directory answer
	// two different questions: a non-zero exit means this rustc has never heard
	// of the target, and a path that does not exist means it knows the target
	// but the component was never added.
	out, err := exec.Command(rustc, "--print", "target-libdir", "--target", RustWasmTarget).Output()
	if err != nil {
		return fmt.Errorf("this rustc does not know the %s target. Install a current Rust toolchain (%s), then build again", RustWasmTarget, InstallRustURL)
	}

	libDir := strings.TrimSpace(string(out))
	if libDir == "" || !dirExists(libDir) {
		return fmt.Errorf("the %s target is not installed. Add it with 'rustup target add %s', then build again", RustWasmTarget, RustWasmTarget)
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func CompileToWasm(javyPath, jsPath, pluginPath, outputPath string) error {
	args := []string{
		"build",
		jsPath,
		"-o", outputPath,
	}
	if pluginPath != "" {
		args = append(args, "-C", fmt.Sprintf("plugin=%s", pluginPath))
	}

	cmd := exec.Command(javyPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("javy build failed: %s: %w", string(output), err)
	}
	return nil
}

func OptimizeWasm(wasmOptPath, inputPath, outputPath string, flags []string) error {
	args := append([]string{}, flags...)
	args = append(args, inputPath, "-o", outputPath)

	cmd := exec.Command(wasmOptPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wasm-opt failed: %s: %w", string(output), err)
	}
	return nil
}
