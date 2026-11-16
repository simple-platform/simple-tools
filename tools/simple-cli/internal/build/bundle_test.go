package build

import (
	"slices"
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
