package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"simple-cli/internal/fsx"
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

// EnsureDependencies installs an action's or a space's packages with npm, then
// notes what they were installed from (RecordInstall).
func EnsureDependencies(dir string) error {
	fsys := fsx.OSFileSystem{}
	before, readErr := fingerprint(fsys, dir, packageFiles)

	cmd := ExecCommandFunc("npm", npmInstallArgs...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm install failed: %s: %w", string(output), err)
	}

	// The note vouches for the files npm read, so it is written only when they
	// are still exactly as npm found them. One of them changes while npm runs
	// when a branch is switched or a pull lands meanwhile, or when npm rewrites
	// the lockfile to match package.json. Without a note, the next
	// `simple test` installs again from whatever is there by then. A note that
	// cannot be written has the same effect, so it does not fail this install.
	after, err := fingerprint(fsys, dir, packageFiles)
	if readErr == nil && err == nil && bytes.Equal(before, after) {
		_ = RecordInstall(fsys, dir)
	}

	return nil
}

// installNote is the file, inside an install's node_modules, that holds the
// fingerprint of noteFiles as they stood when the install finished.
const installNote = ".simple-install"

// packageFiles are the files in a package's folder that decide what npm
// installs there: what package.json asks for, what package-lock.json pins, and
// the npm settings in .npmrc.
var packageFiles = []string{"package.json", "package-lock.json", ".npmrc"}

// noteFiles are packageFiles and npm's own record of the packages it
// installed, node_modules/.package-lock.json. npm rewrites that record
// whenever an install changes anything, so a note also stops matching when
// someone installs by hand. An install that changes nothing leaves all four
// files exactly as they were.
var noteFiles = slices.Concat(packageFiles, []string{filepath.Join("node_modules", ".package-lock.json")})

// RecordInstall writes the fingerprint of dir's noteFiles into its install
// note. EnsureDependencies calls it once npm has finished.
func RecordInstall(fsys fsx.FileSystem, dir string) error {
	note, err := fingerprint(fsys, dir, noteFiles)
	if err != nil {
		return err
	}

	// npm creates no node_modules for a package that has nothing to install.
	modules := filepath.Join(dir, "node_modules")
	if err := fsys.MkdirAll(modules, fsx.DirPerm); err != nil {
		return err
	}

	return fsys.WriteFile(filepath.Join(modules, installNote), note, fsx.FilePerm)
}

// InstalledFromCurrentFiles answers whether dir's packages were installed by
// EnsureDependencies, which `simple build` and `simple test` both run, from
// the noteFiles dir holds now. When it answers no, an install may change what
// is in node_modules: a pull or a dependency update moved the lockfile,
// package.json or .npmrc changed, or npm installed something by hand.
func InstalledFromCurrentFiles(fsys fsx.FileSystem, dir string) bool {
	recorded, err := fsys.ReadFile(filepath.Join(dir, "node_modules", installNote))
	if err != nil {
		return false
	}

	current, err := fingerprint(fsys, dir, noteFiles)

	return err == nil && bytes.Equal(recorded, current)
}

// fingerprint is a SHA-256 digest of the named files in dir, each taken with
// its name and length, so a file that is absent and a file that is empty
// differ.
func fingerprint(fsys fsx.FileSystem, dir string, names []string) ([]byte, error) {
	digest := sha256.New()

	for _, name := range names {
		data, err := fsys.ReadFile(filepath.Join(dir, name))

		switch {
		case errors.Is(err, fs.ErrNotExist):
			digest.Write(fmt.Appendf(nil, "%s absent\n", name))
		case err != nil:
			return nil, err
		default:
			digest.Write(fmt.Appendf(nil, "%s %d\n", name, len(data)))
			digest.Write(data)
		}
	}

	return []byte(hex.EncodeToString(digest.Sum(nil))), nil
}
