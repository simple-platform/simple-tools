package build

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"simple-cli/internal/fsx"
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

// extractWasmOpt unpacks the archive wasm-opt is released in, and leaves the
// tool at destPath.
//
// The archive is a tree: the tool, the other programs released with it, and
// under lib/ what the tool loads when it starts. It belongs under the tools
// directory, two levels above the tool, so that bin/ and lib/ sit where the
// tool looks for them: for a destPath in ~/.simple/bin that is ~/.simple.
//
// NOTHING UNDER THE TOOLS DIRECTORY IS WRITTEN INTO. The tree is unpacked into
// a directory of its own and each file is renamed into place, for the reason
// downloadTool gives: a file another process is running, or has loaded, has to
// stay whole.
//
// The tool itself is renamed to destPath and nowhere else. downloadTool asked
// for it there and gives it its name afterwards, so the tool appears only once
// everything it loads is in place.
func extractWasmOpt(srcPath, destPath string) error {
	binDir := filepath.Dir(destPath)
	rootDir := filepath.Dir(binDir)

	unpacked, err := os.MkdirTemp(rootDir, ".unpack-*")
	if err != nil {
		return fmt.Errorf("failed to create a directory to unpack into: %w", err)
	}
	defer func() { _ = os.RemoveAll(unpacked) }()

	if err := ExtractTarGz(srcPath, unpacked, 1); err != nil {
		return err
	}

	tool := filepath.Join(unpacked, filepath.Base(binDir), WasmOptName)
	found := false

	err = filepath.WalkDir(unpacked, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		target := destPath
		if path == tool {
			found = true
		} else {
			rel, err := filepath.Rel(unpacked, path)
			if err != nil {
				return err
			}
			target = filepath.Join(rootDir, rel)
			if err := os.MkdirAll(filepath.Dir(target), fsx.DirPerm); err != nil {
				return fmt.Errorf("failed to create the directory for %s: %w", target, err)
			}
		}

		if err := os.Rename(path, target); err != nil {
			return fmt.Errorf("failed to install %s: %w", target, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// An archive without the tool would otherwise install the empty file
	// downloadTool staged for it, and report success.
	if !found {
		return fmt.Errorf("the archive carries no %s", filepath.Join(filepath.Base(binDir), WasmOptName))
	}

	return nil
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
