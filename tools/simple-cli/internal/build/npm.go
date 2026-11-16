package build

import (
	"fmt"
	"os/exec"
)

// npmInstallArgs are npm's arguments for installing an action's or a space's
// packages.
//
// NO INSTALL SCRIPTS. npm runs every dependency's preinstall, install and
// postinstall script by default, with the developer's own permissions, and a
// compromised release ships its payload exactly there, free to rewrite the
// files of the repository it is installed into. pnpm does not run them unless a
// package is allowed by name, which is what protected actions and spaces while
// they were installed through pnpm workspaces. Building and testing an action
// or a space needs none of them: the packages that ship native binaries, such
// as esbuild, carry them as optional platform packages, which npm installs
// without running anything. The flag also skips the action's or space's own
// install-time scripts (preinstall, install, postinstall, prepare); its build
// and test scripts still run, because they are run by name.
var npmInstallArgs = []string{"install", "--ignore-scripts"}

func EnsureDependencies(dir string) error {
	cmd := exec.Command("npm", npmInstallArgs...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm install failed: %s: %w", string(output), err)
	}
	return nil
}
