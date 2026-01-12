package build

import (
	"fmt"
	"os/exec"
)

const (
	JavyName               = "javy"
	JavyVersion            = "8.0.0"
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
	return ExtractTarGzFile(srcPath, destPath, "bin/wasm-opt")
}

func CompileToWasm(javyPath, jsPath, pluginPath, outputPath string) error {
	args := []string{
		"compile",
		jsPath,
		"-o", outputPath,
	}
	if pluginPath != "" {
		args = append(args, "-C", pluginPath)
	}

	cmd := exec.Command(javyPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("javy compile failed: %s: %w", string(output), err)
	}
	return nil
}

func OptimizeWasm(wasmOptPath, inputPath, outputPath string, asyncify bool) error {
	args := []string{
		"-O3",
		inputPath,
		"-o", outputPath,
	}

	if asyncify {
		args = append(args, "--asyncify")
	}

	cmd := exec.Command(wasmOptPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wasm-opt failed: %s: %w", string(output), err)
	}
	return nil
}
