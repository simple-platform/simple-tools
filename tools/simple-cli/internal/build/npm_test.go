package build

import (
	"slices"
	"testing"
)

// Installs run no dependency's install scripts: that is where a compromised
// release runs its code. Asserted on the arguments, because running npm here
// would need the network.
func TestNpmInstallRunsNoInstallScripts(t *testing.T) {
	if !slices.Equal(npmInstallArgs, []string{"install", "--ignore-scripts"}) {
		t.Errorf("npm install arguments are %v, want install --ignore-scripts", npmInstallArgs)
	}
}
