package build

import (
	"fmt"
	"os/exec"
)

// sdkPackage is the platform SDK that actions bundle.
const sdkPackage = "@simpleplatform/sdk"

func BundleJS(dir, entryPoint, outFile string, minify bool, defines map[string]string) error {
	// Using esbuild CLI
	cmd := exec.Command("npx", append([]string{"esbuild"}, bundleArgs(entryPoint, outFile, minify, defines)...)...)
	cmd.Dir = dir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("esbuild failed: %s: %w", string(output), err)
	}

	return nil
}

// bundleArgs are esbuild's arguments for bundling one action.
//
// ONE COPY OF THE SDK PER BUNDLE, AND IT IS THE ACTION'S OWN. Code an action
// imports from outside its folder, such as an app's shared `_contracts`, can
// reach the SDK through a tsconfig path into a neighbouring action's packages.
// While every action's packages came from one shared install, both routes led
// to the same files and esbuild bundled them once. Installed per action, as
// `simple build` installs them, they are two copies, and the bundle carried
// both: the SDK's code and its module-level state twice.
//
// The alias sends every import of the SDK, from any file, to the copy found
// from the action's own folder: esbuild resolves an alias from its working
// directory, which is the action's, and applies it before tsconfig paths. An
// action that imports no SDK is unaffected. Two consequences hold the same
// promise: an action that reached the SDK only through such a path, with none
// installed of its own, now fails to build until it lists the SDK itself; and
// a dependency that pins a different SDK major gets the action's copy too, and
// the build fails on the mismatch rather than shipping two copies.
func bundleArgs(entryPoint, outFile string, minify bool, defines map[string]string) []string {
	args := []string{
		entryPoint,
		"--bundle",
		"--outfile=" + outFile,
		"--alias:" + sdkPackage + "=" + sdkPackage,
	}

	if minify {
		args = append(args, "--minify")
	}

	for k, v := range defines {
		args = append(args, fmt.Sprintf("--define:%s=%s", k, v))
	}

	return args
}
