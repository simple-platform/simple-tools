#!/usr/bin/env bash
# Refuses source files that hide executable code behind a long run of whitespace.
#
# That shape is not a style problem. It is how an injected loader stays invisible:
# the payload is appended to a file's last line after ~2000 spaces, so a diff shows
# one changed line ending in the file's real closing brace and the malicious content
# sits thousands of columns off-screen, past the edge of every review tool.
#
# Deliberately independent of lint-staged's globs. Those exclude vendored and generated-code
# paths, and the last injection landed in exactly such a path.
#
# A second check, further down, refuses a package anywhere in the repository
# that runs a script at install time, and any other file pnpm runs at install.
#
# Modes:
#   --staged        what is about to be committed   (pre-commit)
#   --tree          the whole working tree          (CI, and the push/merge/
#                                                    checkout/rewrite hooks)

set -uo pipefail

MODE="${1:---staged}"

# Validated here, not inside files(). files() is piped, so it runs in a
# subshell and an exit there ends the subshell while the script carries on --
# which is how four hooks called this with flags it does not implement and the
# guard printed usage, returned success, and scanned nothing.
case "$MODE" in
  --staged|--tree) ;;
  *) echo "usage: $0 [--staged|--tree]" >&2; exit 2 ;;
esac
export SUPPLY_CHAIN_GUARD_MODE="$MODE"

# Every path below is relative to the repository's top.
top="$(git rev-parse --show-toplevel)" && cd "$top" || exit 2

# files: every file this run checks, NUL-separated: otherwise git prints a
# name holding a non-ASCII letter in quotes, as "caf\303\251.js", no file has
# that name, and the file went unchecked. git is given no pattern to match
# names against, so a caller's `git --literal-pathspecs`, which git hands on
# to the hooks it runs and some editors pass on every call, cannot make it
# list nothing. Each check picks its own files from the list, in node. Before
# a commit the list holds each file added, copied, modified, renamed or
# changed in type, such as a link replaced by a real file. Staged mode reads
# the index's blob: the working copy can be cleaned or deleted after a payload
# is staged, but Git would still commit that staged content.
files() {
  case "$MODE" in
    --staged) git diff --cached --name-only -z --diff-filter=ACMRT ;;
    --tree)   git ls-files -z ;;
  esac
}

# The start of both checks' node programs. eachFile(check) calls
# check(file, raw) for each name files() printed: file is the name as text,
# and raw the name byte for byte, to open the file by. Read as text, a name
# that is not valid UTF-8 names no file, and that file would go unchecked.
# statOf(raw) is what is at raw, or null when nothing is. Any other error,
# such as a folder that cannot be read or a link to itself, stops the check,
# so a file it cannot open is refused, never skipped. readText(raw) is the
# file's text without the byte-order mark some editors write first.
#
# fold(name) folds a name the way a Mac's disk compares names, so each check
# reads every file that pnpm or node there would load under one of its names.
# The disk folds letter case beyond A to Z: it loads `.pnpmfile.cjſ`, with a
# long s, as `.pnpmfile.cjs`, and takes a Kelvin sign for K, ﬁ for fi and ß
# for ss. git, even ignoring case, folds only A to Z. Upper- then lower-casing
# folds each of those. It also folds a dotless ı to i, which the disk does
# not, so such a name is read as well. The disk also takes an accented letter
# written as one character for the same letter followed by its accent
# (Unicode's NFD); checked over every Unicode character, that changes none of
# the names the checks look for.
FILES='
  const { execFileSync } = require("child_process");
  const fs = require("fs");
  const path = require("path");
  const staged = process.env.SUPPLY_CHAIN_GUARD_MODE === "--staged";
  let stagedEntries;
  let stagedBlobs;
  const fold = (name) => name.toUpperCase().toLowerCase();
  const eachFile = (check) => {
    const names = fs.readFileSync(0);
    const paths = [];
    for (let start = 0, end; (end = names.indexOf(0, start)) !== -1; start = end + 1) {
      paths.push(names.subarray(start, end));
    }
    if (staged) {
      stagedEntries = new Map();
      const index = execFileSync("git", ["ls-files", "--stage", "-z"]);
      for (let start = 0, end; (end = index.indexOf(0, start)) !== -1; start = end + 1) {
        const record = index.subarray(start, end);
        const tab = record.indexOf(9);
        const header = record.subarray(0, tab).toString("ascii");
        const match = /^(\d{6}) ([0-9a-f]+) 0$/.exec(header);
        if (tab === -1 || match === null) continue;
        stagedEntries.set(record.subarray(tab + 1).toString("hex"), { mode: match[1], oid: match[2] });
      }
      stagedBlobs = new Map();
    }
    for (const raw of paths) {
      check(raw.toString(), raw);
    }
  };
  const entryOf = (raw) => stagedEntries?.get(raw.toString("hex"));
  const isReadableFile = (raw) => staged
    ? ["100644", "100755", "120000"].includes(entryOf(raw)?.mode)
    : statOf(raw)?.isFile();
  const readContent = (raw) => {
    if (!staged) return fs.readFileSync(raw);
    const entry = entryOf(raw);
    if (!entry || !["100644", "100755", "120000"].includes(entry.mode)) {
      throw new Error("a staged path is not a regular file or symbolic link");
    }
    if (!stagedBlobs.has(entry.oid)) {
      const size = Number(execFileSync("git", ["cat-file", "-s", entry.oid], { encoding: "ascii" }).trim());
      if (!Number.isSafeInteger(size) || size < 0) throw new Error("git cat-file returned an invalid staged blob size");
      const content = execFileSync("git", ["cat-file", "blob", entry.oid], { maxBuffer: size + 1 });
      if (content.length !== size) throw new Error("git cat-file returned a truncated staged blob");
      stagedBlobs.set(entry.oid, content);
    }
    const content = stagedBlobs.get(entry.oid);
    if (content === undefined) throw new Error("a staged blob could not be read");
    return content;
  };
  const statOf = (raw) => {
    try {
      return fs.statSync(raw);
    } catch (error) {
      if (error.code === "ENOENT" || error.code === "ENOTDIR") return null;
      throw error;
    }
  };
  const readText = (raw) => readContent(raw).toString("utf8").replace(/^\ufeff/, "");
'

# A run of blanks this long is never formatting. Real indentation is orders of
# magnitude smaller; the observed payloads used 500-2000.
#
# The run is counted in the columns it takes on screen, since that is what
# pushes code out of view. A space is one column. A tab is eight, as wide as
# terminals, less and browsers (so GitHub) draw it unless told otherwise:
# counted as one, 99 tabs, nearly 800 columns, would pass. Any other blank is
# two, the widest any of them is drawn (an ideographic or an em space). That
# over-counts the narrow ones, but refuses only runs of fifty of them, which
# no formatting has. A character that may be drawn as nothing, such as a
# zero-width space or a control character, counts as no column and does not
# end a run: on screen the run goes on.
RUN=100

status=0

# What either check says when git or node fails, or is missing. The tool that
# failed has printed why just above it.
COULD_NOT_RUN="the check could not run: git or node failed, and its error is above"

# The scan runs in node, which the second check needs too. node reads each
# blank as one character on every machine; awk reads it as bytes or as a
# character depending on which awk runs and in which locale.
if ! hidden="$(files | node -e "$FILES"'
  const run = " ".repeat(Number(process.argv[1]));
  // JS and TS in each of their forms, and the patch files pnpm applies to
  // dependencies at install, whose added lines become code in them.
  const scanned = [".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".mts", ".cts", ".astro", ".patch", ".diff"];
  // Every other blank: the characters Unicode classes as spaces (no-break, en,
  // em, thin, ideographic and the like), and the letters and symbols fonts
  // draw as blank space, the Hangul fillers and the empty Braille cell.
  const otherBlanks = /[\u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\u115f\u1160\u3164\uffa0\u2800]/g;
  // Characters that may be drawn as nothing: those Unicode calls default
  // ignorable, such as the zero-width space, the joiners, the byte-order mark
  // and the soft hyphen; its other format characters; and the control
  // characters other than tab and the line breaks, which some tools draw as a
  // symbol but a browser may not; and the line and paragraph separators, which
  // some tools draw as a line break and others as nothing. A file in UTF-16,
  // read here as UTF-8, holds a zero byte between its letters, which this
  // drops too.
  const invisible = /(?![\t\n\r])[\p{Default_Ignorable_Code_Point}\p{Cf}\p{Cc}\p{Zl}\p{Zp}]/gu;
  eachFile((file, raw) => {
    // node_modules is matched as spelled, not folded: folding may add files
    // to read, never take any away.
    if (!scanned.some((ending) => fold(file).endsWith(ending)) || ("/" + file).includes("/node_modules/")) return;
    if (!isReadableFile(raw)) return;
    // Every file is read as UTF-8, as node, pnpm and Astro read it. One that
    // starts with a UTF-16 byte-order mark is also read as UTF-16, as browsers
    // and the TypeScript compiler read it. A run in either reading is refused.
    const bytes = readContent(raw);
    const readings = [bytes.toString("utf8")];
    if (bytes[0] === 0xff && bytes[1] === 0xfe) readings.push(bytes.toString("utf16le"));
    if (bytes[0] === 0xfe && bytes[1] === 0xff) {
      readings.push(Buffer.from(bytes.subarray(0, bytes.length & -2)).swap16().toString("utf16le"));
    }
    for (const text of readings) {
      // Each blank becomes as many spaces as the columns it counts for, and
      // each invisible character goes. The other blanks go first: the Hangul
      // fillers are default ignorable too, but are drawn as blank space.
      const drawn = text.replace(otherBlanks, "  ").replace(invisible, "").replaceAll("\t", " ".repeat(8));
      const at = drawn.indexOf(run);
      if (at !== -1) {
        console.log(`    ${file} (line ${drawn.slice(0, at).split("\n").length})`);
        return;
      }
    }
  });
' -- "$RUN")"; then
  echo ""
  echo "  BLOCKED: the repository was not checked for hidden code:"
  echo "  $COULD_NOT_RUN."
  echo ""
  status=1
elif [ -n "$hidden" ]; then
  echo ""
  echo "  BLOCKED: hidden code found past a long whitespace run"
  echo ""
  printf '%s\n' "$hidden"
  cat <<'MSG'

  Do NOT edit the payload out by hand — the file may also carry a prepended
  shim, and a partial removal leaves a working loader.

  Restore the file from a known-clean commit instead:

      git checkout <clean-ref> -- <path>

  If you believe this is a false positive (a generated .d.ts or a minified
  bundle can legitimately have very long lines), confirm the file has none of:

      grep -c 'fromCharCode(127)' <path>
      grep -c 'createRequire(import.meta.url)' <path>

  then make this script skip that one path, next to its node_modules rule,
  with a comment saying who checked it and when.

MSG
  status=1
fi

# ---------------------------------------------------------------------------
# Refuses a package that runs a script at install time, wherever it sits.
#
# pnpm runs those scripts on every `pnpm install`, on every laptop and CI
# runner, before anyone has read what they run. An injected loader can arrive
# that way, and regenerating a package.json can bring such a script back. So can
# any other tool that rewrites a package.json.
#
# Which scripts, read from the installers' own code: pnpm 10 runs preinstall,
# install, postinstall, preprepare, prepare and postprepare for every workspace
# package, and pnpm:devPreinstall for the root; npm adds prepublish, and runs
# predependencies, dependencies and postdependencies when an install changes
# node_modules (seen with npm 11.5). pnpm also turns a binding.gyp into an
# install script that runs node-gyp, and reads a package.yaml or package.json5
# as the manifest. This check reads only package.json, so those last two are
# refused outright. So is a .pnpmfile.cjs: pnpm loads the one in the folder it
# installs, here the repository's top, and runs its code.
#
# Some of pnpm's settings make install run a file too, as pnpm 10.34.5's code
# shows: pnpmfile and globalPnpmfile load one; scriptShell runs every script
# through one; nodeOptions can preload one into each script's node;
# configDependencies loads the pnpmfile.cjs of a plugin it downloads; and
# dangerouslyAllowAllBuilds lets every dependency run its install scripts, as
# neverBuiltDependencies does when no onlyBuiltDependencies list is set. pnpm
# takes its settings from a .npmrc in the folder it runs in, at the
# workspace's top and in each package; from every top-level key of
# pnpm-workspace.yaml; and from the "pnpm" field of the package.json at the
# top of what it installs. So a tracked .npmrc is refused in any folder, and a
# setting in a pnpm-workspace.yaml or in a "pnpm" field is refused unless it is
# on the settings allowlist below.
#
# Every such file the repository tracks is read, not only those under
# pnpm-workspace.yaml's packages: pnpm also installs folders that list reaches
# only through a symlink, or names with a brace pattern. Each package.json is
# parsed as JSON, the way pnpm reads it, so a key spelled with an escaped
# letter is still caught.

INSTALL_SCRIPTS='preinstall install postinstall preprepare prepare postprepare prepublish pnpm:devPreinstall predependencies dependencies postdependencies'

# Install-time scripts need an explicit exact-command exception and a reason.
# This app-workspace template has no exception: new install scripts stay refused.
INSTALL_SCRIPT_ALLOWLIST=''

# The pnpm settings the repo may set, one per line, in a pnpm-workspace.yaml
# or a package.json's "pnpm" field. Any other is refused until its name is
# added here, with its reason:
#
#   packages               the folders that are workspace packages.
#   onlyBuiltDependencies  the dependencies allowed to run install scripts.
#                          Adding one is a dependency change, reviewed as such.
#   ignoredBuiltDependencies  the dependencies whose install scripts pnpm
#                          skips without a warning. It only takes permission
#                          away; `pnpm approve-builds` writes it.
#   allowBuilds            the same two lists in the form `pnpm approve-builds`
#                          writes to pnpm-workspace.yaml: each dependency set
#                          to true joins onlyBuiltDependencies, each set to
#                          false joins ignoredBuiltDependencies.
#   minimumReleaseAgeExcludePrune  controls pruning for versions withheld by
#                          the workspace's minimum release age.
#   shellEmulator          selects pnpm's built-in shell handling for scripts.
#   trustPolicy            prevents downgrades in package trust requirements.
#   overrides              the versions pnpm must use for some dependencies.
#   patchedDependencies    the patches pnpm applies to some dependencies. Only
#                          in package.json, where this check reads that each
#                          patch is a .patch or .diff file in the repository,
#                          outside node_modules, which the hidden-code check
#                          scans. `pnpm patch-commit` writes it there while
#                          package.json has a "pnpm" field.
PNPM_SETTINGS_ALLOWLIST='packages
onlyBuiltDependencies
ignoredBuiltDependencies
allowBuilds
minimumReleaseAgeExcludePrune
shellEmulator
trustPolicy
overrides
patchedDependencies'

# Reads files() on stdin and prints one line for each thing it refuses.
refusals() {
  node -e "$FILES"'
    const names = process.argv[1].split(" ");
    const allowed = new Set(process.argv[2].split("\n"));
    const settings = new Set(process.argv[3].split("\n"));
    const plain = (s) => String(s).replace(/[^\x20-\x7e]/g, "?");
    const unlisted = (where, setting) => `${plain(where)}: pnpm setting ${setting} is not on the settings allowlist`;
    // What pnpm reads to learn what a package runs at install, in any folder:
    // its manifest under each name pnpm accepts, a binding.gyp, a .pnpmfile.cjs,
    // and its settings files.
    const manifests = ["package.json", "package.json5", "package.yaml", "binding.gyp", ".pnpmfile.cjs", ".npmrc", "pnpm-workspace.yaml"];
    eachFile((file, raw) => {
      const base = fold(path.basename(file));
      if (!manifests.includes(base) || (staged ? !isReadableFile(raw) : !statOf(raw))) return;
      if (base === ".npmrc") {
        console.log(`${plain(file)}: pnpm takes settings from it, and some make install run a file; set what the repo needs in pnpm-workspace.yaml`);
        return;
      }
      if (base === "pnpm-workspace.yaml") {
        // Each setting must be a plain "key:" line at the left edge, the only
        // form of YAML this check reads. Any other line that is not blank, a
        // comment, a --- that starts the text, or part of the value above it,
        // is refused rather than guessed at.
        let seenSetting = false;
        readText(raw).split(/\r\n|\r|\n/).forEach((line, index) => {
          if (/^[ \t]*(#.*)?$/.test(line) || (!seenSetting && /^---[ \t]*$/.test(line))) return;
          if (seenSetting && /^( |-( |$))/.test(line)) return;
          seenSetting = true;
          const setting = /^([A-Za-z0-9_][A-Za-z0-9_.-]*)[ \t]*:([ \t]|$)/.exec(line)?.[1];
          if (setting == null) {
            console.log(`${plain(file)}: line ${index + 1} is not a plain top-level setting, so this check cannot read it`);
          } else if (setting === "patchedDependencies") {
            console.log(`${plain(file)}: keep patchedDependencies in package.json, where this check reads which file each patch is`);
          } else if (!settings.has(setting)) {
            console.log(unlisted(file, setting));
          }
        });
        return;
      }
      if (base === "binding.gyp") {
        console.log(`${plain(file)}: pnpm builds it with node-gyp at install`);
        return;
      }
      if (base === ".pnpmfile.cjs") {
        console.log(`${plain(file)}: pnpm loads it at install and runs its code`);
        return;
      }
      if (base !== "package.json") {
        console.log(`${plain(file)}: pnpm reads it as the manifest; use package.json, which this check reads`);
        return;
      }
      let manifest;
      try {
        manifest = JSON.parse(readText(raw));
      } catch {
        console.log(`${plain(file)}: not valid JSON, so its scripts cannot be checked`);
        return;
      }
      const scripts = manifest?.scripts ?? {};
      const label = typeof manifest?.name === "string" ? `${file} (${manifest.name})` : file;
      for (const name of names) {
        if (Object.hasOwn(scripts, name) && !allowed.has(`${file} ${name} ${scripts[name]}`)) {
          console.log(`${plain(label)}: ${name} runs ${JSON.stringify(String(scripts[name]))}`);
        }
      }
      if (manifest?.pnpm !== null && typeof manifest?.pnpm === "object") {
        for (const setting of Object.keys(manifest.pnpm)) {
          if (!settings.has(setting)) console.log(unlisted(label, `pnpm.${setting}`));
        }
        // pnpm applies a patch whatever its file is called and wherever it is,
        // but the hidden-code check reads only .patch and .diff files in the
        // repository, outside node_modules folders. A path is relative to the
        // folder of the package.json that names it.
        for (const patch of Object.values(manifest.pnpm.patchedDependencies ?? {})) {
          const where = path.posix.normalize(path.posix.join(path.posix.dirname(file), String(patch)));
          const spot = "/" + fold(where);
          if (path.isAbsolute(String(patch)) || where === ".." || where.startsWith("../")
              || spot.includes("/node_modules/") || !/\.(patch|diff)$/.test(spot)) {
            console.log(`${plain(label)}: patch ${plain(patch)} is not a .patch or .diff file in the repository outside node_modules, which the hidden-code check reads`);
          }
        }
      }
    });
  ' -- "$INSTALL_SCRIPTS" "$INSTALL_SCRIPT_ALLOWLIST" "$PNPM_SETTINGS_ALLOWLIST"
}

problem=""
refused=""
if ! refused="$(files | refusals)"; then
  problem="$COULD_NOT_RUN"
fi

if [ -n "$problem" ]; then
  echo ""
  echo "  BLOCKED: the repository was not checked for install-time scripts:"
  echo "  $problem."
  echo ""
  status=1
elif [ -n "$refused" ]; then
  echo ""
  echo "  BLOCKED: a package or a setting can run code at install time"
  echo ""
  printf '%s\n' "$refused" | sed 's/^/    /'
  cat <<'MSG'

  pnpm runs these on every `pnpm install`, on every machine and CI runner,
  before anyone has read what they run.

  If a tool wrote it, take it out again before anyone runs pnpm install. If nobody can say where it came
  from, treat it as an injection: do not run pnpm install, and restore the file
  from a known-clean commit:

      git checkout <clean-ref> -- <path>

  If the repo genuinely needs one of these scripts, add it to the
  install-script allowlist in this script, with its exact command and the
  reason. If it needs one of these pnpm settings, add the setting's name to
  the pnpm settings allowlist in this script, with the reason, and set it in
  pnpm-workspace.yaml: a .npmrc stays refused. Any other file here stays
  refused until this check is changed to read it.

MSG
  status=1
fi

exit "$status"
