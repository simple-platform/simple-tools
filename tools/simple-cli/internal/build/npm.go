package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	return installDependencies(dir, false)
}

// UpdateDependencies deliberately asks npm to select versions from package.json
// and write the resulting lock file.
func UpdateDependencies(dir string) error {
	return installDependencies(dir, true)
}

func installDependencies(dir string, update bool) error {
	fsys := fsx.OSFileSystem{}
	before, readErr := fingerprint(fsys, dir, packageFiles)
	beforeManifest, manifestReadErr := fingerprint(fsys, dir, []string{"package.json", ".npmrc"})

	args, err := dependenciesInstallArgs(fsys, dir, update)
	if err != nil {
		return err
	}

	cmd := ExecCommandFunc("npm", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		if args[0] == "ci" {
			return fmt.Errorf("npm ci failed in %s:\n%s\n%w\nIf package.json and package-lock.json disagree, choose versions with simple deps update %s and commit the lock file", dir, string(output), err, dir)
		}
		return fmt.Errorf("npm %s failed in %s:\n%s\n%w", args[0], dir, string(output), err)
	}

	// A deliberate update and a first install create or rewrite the lockfile.
	// In those cases package.json and npm settings still prove which inputs npm
	// read; for npm ci, all package files must remain unchanged.
	after, err := fingerprint(fsys, dir, packageFiles)
	afterManifest, afterManifestErr := fingerprint(fsys, dir, []string{"package.json", ".npmrc"})
	unchangedFiles := readErr == nil && err == nil && bytes.Equal(before, after)
	unchangedInstallInputs := args[0] == "install" && manifestReadErr == nil && afterManifestErr == nil && bytes.Equal(beforeManifest, afterManifest)
	if unchangedFiles || unchangedInstallInputs {
		_ = RecordInstall(fsys, dir)
	}

	return nil
}

func dependenciesInstallArgs(fsys fsx.FileSystem, dir string, update bool) ([]string, error) {
	if update {
		return []string{"install", "--ignore-scripts"}, nil
	}
	if _, err := fsys.Stat(filepath.Join(dir, "package-lock.json")); err == nil {
		return []string{"ci", "--ignore-scripts"}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to check for package-lock.json in %s: %w", dir, err)
	}
	return npmInstallArgs, nil
}

// DependenciesNeedInstall answers whether the install note no longer describes
// the package files, or a required package is absent from Node's lookup path.
func DependenciesNeedInstall(fsys fsx.FileSystem, dir string) bool {
	if PackagesMissing(fsys, dir) {
		return true
	}
	if _, err := fsys.Stat(filepath.Join(dir, "package-lock.json")); errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return !InstalledFromCurrentFiles(fsys, dir)
}

// BuildDependenciesNeedInstall requires a folder's own lock file to be noted
// before a build can skip its install. Without one, npm must create the lock
// file even when the packages happen to be reachable from a workspace root.
func BuildDependenciesNeedInstall(fsys fsx.FileSystem, dir string) bool {
	if PackagesMissing(fsys, dir) {
		return true
	}
	if _, err := fsys.Stat(filepath.Join(dir, "package-lock.json")); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return !InstalledFromCurrentFiles(fsys, dir)
}

// PackagesMissing checks the non-optional packages named in package.json
// using the same upward lookup Node uses when resolving imports.
func PackagesMissing(fsys fsx.FileSystem, dir string) bool {
	raw, err := fsys.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return true
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var manifest struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return true
	}
	for _, listed := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
		for name := range listed {
			if _, optional := manifest.OptionalDependencies[name]; optional {
				continue
			}
			if _, found := fsx.ResolveUpward(fsys, dir, "node_modules", name); !found {
				return true
			}
		}
	}
	return false
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
