package build

import (
	"os"
	"path/filepath"
	"testing"
)

// THE GENERATOR'S PACKAGES ARE INSTALLED ONCE, NOT ONCE PER RUN.
//
// The generator imports two npm packages. Before running it, this package checks
// whether they are there and installs them if they are not — and for as long as
// the check looked in the wrong directory, they never appeared to be there, so
// every single run paid for an install that changed nothing. It cost this suite
// about half a minute per run, and it cost every developer build the same on
// every action described.
//
// The wrong directory was not a typo. It was a walk that stopped at the first
// package.json above it, which inside a workspace is always a member's manifest
// and never the root. So the two things checked here are the two halves of
// getting that right: what counts as a workspace root, and where a package
// counts as installed.

// requireWorkspaceRoot asks where the workspace root is and fails unless the
// answer is the directory named.
//
// The two paths are compared with their symlinks taken out, because a temporary
// directory on macOS is handed out under a symlinked path — /var, which is a
// link to /private/var — and the same directory reached by the two spellings
// would otherwise not compare equal. The failure quotes the spellings the test
// and the code actually used, since that is what a reader has to look at.
func requireWorkspaceRoot(t *testing.T, want, why string) {
	t.Helper()

	found, err := findWorkspaceRoot()
	if err != nil {
		t.Fatalf("failed to find the workspace root: %v", err)
	}

	if resolvedPath(t, found) != resolvedPath(t, want) {
		t.Errorf("workspace root = %s, want %s (%s)", found, want, why)
	}
}

// resolvedPath is a path with its symlinks taken out.
func resolvedPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("failed to resolve %s: %v", path, err)
	}

	return resolved
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// This suite is the case that exposed the defect, so this suite is where it is
// held down: run from a directory several levels inside a member package, the
// answer must still be the top of the repository.
//
// It is asserted as a property of the answer rather than against a path spelled
// out here, because a path spelled out here is a second opinion about where the
// root is, and a second opinion can be wrong in the same direction as the first.
// The root of this repository is the directory holding pnpm-workspace.yaml, and
// no other directory on the way up to it holds one.
func TestWorkspaceRootFoundFromInsideAMemberPackage(t *testing.T) {
	root, err := findWorkspaceRoot()
	if err != nil {
		t.Fatalf("failed to find the workspace root: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "pnpm-workspace.yaml")); err != nil {
		t.Fatalf(
			"the workspace root came back as %s, which holds no pnpm-workspace.yaml: the walk stopped inside a member package",
			root,
		)
	}

	// And with the root right, nothing that runs the generator has anything to
	// install — which is the whole point of getting the root right.
	if !generatorPackagesResolvable(root) {
		t.Fatalf("the generator's packages are not resolvable from %s, so every run would install them again", root)
	}
}

// A member's own manifest must not be mistaken for the root, and removing the
// file that marks the root must change the answer — otherwise the marker is not
// what is being read and this test proves nothing.
func TestWorkspaceRootPrefersTheFileThatMarksIt(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "tools", "cli")
	deep := filepath.Join(member, "internal", "build")

	writeFile(t, filepath.Join(root, "pnpm-workspace.yaml"), "packages:\n  - tools/*\n")
	writeFile(t, filepath.Join(root, "package.json"), `{"name":"root"}`)
	writeFile(t, filepath.Join(member, "package.json"), `{"name":"member"}`)

	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", deep, err)
	}

	t.Chdir(deep)

	requireWorkspaceRoot(t, root, "the directory holding pnpm-workspace.yaml")

	// Take the marker away. Nothing else about the tree changes, so an answer
	// that does not change with it was never reading the marker.
	if err := os.Remove(filepath.Join(root, "pnpm-workspace.yaml")); err != nil {
		t.Fatalf("failed to remove the workspace marker: %v", err)
	}

	requireWorkspaceRoot(t, member, "with no workspace above it, the nearest manifest")
}

// npm and yarn mark their root inside the manifest rather than beside it, and a
// repository laid out that way has the same member manifests to walk past.
func TestWorkspaceRootReadsTheWorkspacesFieldOfAManifest(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "packages", "one")

	writeFile(t, filepath.Join(root, "package.json"), `{"name":"root","workspaces":["packages/*"]}`)
	writeFile(t, filepath.Join(member, "package.json"), `{"name":"one"}`)

	t.Chdir(member)

	requireWorkspaceRoot(t, root, "the manifest declaring workspaces")
}

// A `workspaces` field that names nothing is not a declaration, and a manifest
// this package cannot read is not one either. Neither should capture the walk.
func TestWorkspaceRootIgnoresAManifestThatDeclaresNothing(t *testing.T) {
	for name, manifest := range map[string]string{
		"a null field":        `{"name":"root","workspaces":null}`,
		"no field at all":     `{"name":"root"}`,
		"json it cannot read": `{"name":"root",`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			member := filepath.Join(root, "packages", "one")

			writeFile(t, filepath.Join(root, "package.json"), manifest)
			writeFile(t, filepath.Join(member, "package.json"), `{"name":"one"}`)

			t.Chdir(member)

			requireWorkspaceRoot(t, member, "the nearest manifest, the root above declaring no members")
		})
	}
}

// A project belonging to no workspace at all still has to get an answer, and it
// is the project itself.
func TestWorkspaceRootOfAProjectInNoWorkspace(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "myapp")

	writeFile(t, filepath.Join(project, "package.json"), `{"name":"myapp"}`)

	t.Chdir(project)

	requireWorkspaceRoot(t, project, "the project itself")
}

// A package installed above a directory is installed as far as Node is
// concerned, because Node keeps walking up until it finds one. Deciding it is
// missing because it is not in one particular directory is what bought the
// install that never had anything to do.
func TestNodeCanResolveTakesTheWalkNodeTakes(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "tools", "cli", "internal", "build")

	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", deep, err)
	}

	installed := filepath.Join(root, "node_modules", "ts-morph")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatalf("failed to install the package: %v", err)
	}

	if !nodeCanResolve(deep, "ts-morph") {
		t.Error("a package installed at the top of the tree was not found from a directory inside it")
	}

	if nodeCanResolve(deep, "ts-json-schema-generator") {
		t.Error("a package installed nowhere was found anyway")
	}

	// Uninstall it. An answer that survives the package going away is not
	// reading the tree.
	if err := os.RemoveAll(installed); err != nil {
		t.Fatalf("failed to remove the package: %v", err)
	}

	if nodeCanResolve(deep, "ts-morph") {
		t.Error("the package was still found after being removed")
	}
}

// Node never asks a directory named node_modules for a node_modules of its own,
// so neither does this. The tree below is the one that tells the two behaviours
// apart: a walk that skipped the rule would answer from
// node_modules/node_modules, which is a path nothing installs to.
func TestNodeCanResolveDoesNotLookInsideNodeModules(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "node_modules", "node_modules", "ts-morph"), 0o755); err != nil {
		t.Fatalf("failed to build the nested tree: %v", err)
	}

	if nodeCanResolve(filepath.Join(root, "node_modules"), "ts-morph") {
		t.Error("the walk asked node_modules for a node_modules of its own")
	}

	// The same starting directory, with the package where a package really is
	// installed, is answered — so the refusal above is the rule biting rather
	// than the walk being blind to anything under a node_modules at all.
	beside := t.TempDir()

	if err := os.MkdirAll(filepath.Join(beside, "node_modules", "ts-morph"), 0o755); err != nil {
		t.Fatalf("failed to install the package: %v", err)
	}

	if !nodeCanResolve(filepath.Join(beside, "node_modules"), "ts-morph") {
		t.Error("a package installed in the node_modules being walked out of was not found")
	}
}
