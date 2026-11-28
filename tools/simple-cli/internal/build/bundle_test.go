package build

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The alias is what keeps an action's bundle to one copy of the SDK when code
// it imports from outside its folder maps the SDK into a neighbour's packages.
// Asserted on the arguments, because running esbuild here would need npm.
func TestBundleArgsSendEveryImportOfTheSDKToTheActionsOwnCopy(t *testing.T) {
	args := bundleArgs("src/index.ts", "build/bundle.sync.js", true, nil)

	if !slices.Contains(args, "--alias:@simpleplatform/sdk=@simpleplatform/sdk") {
		t.Errorf("esbuild arguments %v lack the SDK alias", args)
	}
}

func TestBundlersRequireActionLocalPackagesAndDisableNpxDownloads(t *testing.T) {
	for _, test := range []struct {
		name     string
		packageN string
		args     []string
	}{
		{name: "esbuild", packageN: "esbuild", args: esbuildCommandArgs("src/index.ts", "build/out.js", false, nil)},
		{name: "sdk builder", packageN: sdkPackage, args: bundleAsyncCommandArgs("src/index.ts", "build/out.js")},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := os.Stat(filepath.Join(dir, "node_modules", filepath.FromSlash(test.packageN))); !os.IsNotExist(err) {
				t.Fatalf("test package unexpectedly exists: %v", err)
			}
			var err error
			if test.name == "esbuild" {
				err = BundleJS(dir, "src/index.ts", "build/out.js", false, nil)
			} else {
				err = BundleAsync(dir, "src/index.ts", "build/out.js")
			}
			if err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), test.packageN) {
				t.Fatalf("bundler error = %v, want missing package and folder", err)
			}
			if len(test.args) < 3 || test.args[0] != "--no" || test.args[1] != "--" {
				t.Errorf("npx arguments = %v, want downloads disabled with --no --", test.args)
			}
		})
	}
}
