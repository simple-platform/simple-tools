package scaffold

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSkillBundleLinksResolveAndResourcesAreCopied(t *testing.T) {
	written := make(map[string][]byte)
	if err := copyAgentDocs(&mockWriteTrackingFS{written: written}, TemplatesFS, "/workspace"); err != nil {
		t.Fatal(err)
	}
	if err := checkSkillBundleLinks(TemplatesFS, written); err != nil {
		t.Fatal(err)
	}
}

func TestSkillBundleLinkCheckRejectsMissingResources(t *testing.T) {
	const resource = "templates/agent/skills/record-space-builder/SKILL.md"
	for _, tt := range []struct {
		name          string
		missingSource bool
		missingCopy   bool
		wantErr       string
	}{
		{name: "complete bundle"},
		{name: "missing linked skill", missingSource: true, wantErr: resource},
		{name: "missing copied resource", missingCopy: true, wantErr: "not copied unchanged"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bundle := fstest.MapFS{
				"templates/agent/skills/build-spaces/SKILL.md": {Data: []byte(
					"[Record workflows](../record-space-builder/SKILL.md#purpose)\n" +
						"[Public docs](https://simple.dev/docs/features/spaces) [Here](#purpose)\n" +
						"Repository example: `apps/example/spaces/main/src/App.tsx`\n" +
						"```markdown\n[Example](sibling.md)\n```\n" +
						"````markdown\n```markdown\n[Example](another.md)\n```\n````\n" +
						"[Styles][styles]\n[styles]: assets/layout.css\n")},
				resource: {Data: []byte("# Record workflows\n")},
				"templates/agent/skills/build-spaces/assets/layout.css": {Data: []byte(".page { min-width: 0; }\n")},
			}
			if tt.missingSource {
				delete(bundle, resource)
			}
			written := make(map[string][]byte)
			if err := copyAgentDocs(&mockWriteTrackingFS{written: written}, bundle, "/workspace"); err != nil {
				t.Fatal(err)
			}
			if tt.missingCopy {
				delete(written, "/workspace/.agent/skills/record-space-builder/SKILL.md")
			}
			err := checkSkillBundleLinks(bundle, written)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			} else {
				t.Logf("rejected invalid bundle: %v", err)
			}
		})
	}
}

// Match inline links and reference definitions, not unlinked repository examples.
var skillFileLink = regexp.MustCompile(`\]\(\s*<?([^\s)>]+)|(?m:^\s{0,3}\[[^\]\n]+\]:\s*<?([^\s>]+))`)
var skillCodeFence = regexp.MustCompile("^[ ]{0,3}(`{3,}|~{3,})(.*)$")

func skillMarkdownProse(content []byte) []byte {
	var prose []string
	var fence string
	for _, line := range strings.Split(string(content), "\n") {
		if match := skillCodeFence.FindStringSubmatch(line); match != nil {
			if fence == "" {
				fence = match[1]
			} else if match[1][0] == fence[0] && len(match[1]) >= len(fence) && strings.TrimSpace(match[2]) == "" {
				fence = ""
			}
			continue
		}
		if fence == "" {
			prose = append(prose, line)
		}
	}
	return []byte(strings.Join(prose, "\n"))
}

func checkSkillBundleLinks(bundle fs.FS, written map[string][]byte) error {
	return fs.WalkDir(bundle, "templates/agent/skills", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || path.Ext(name) != ".md" {
			return nil
		}
		content, err := fs.ReadFile(bundle, name)
		if err != nil {
			return err
		}
		for _, match := range skillFileLink.FindAllSubmatch(skillMarkdownProse(content), -1) {
			target := string(match[1]) + string(match[2])
			link, err := url.Parse(target)
			if err != nil {
				return fmt.Errorf("%s: invalid link %q: %w", name, target, err)
			}
			if link.IsAbs() || link.Host != "" || link.Path == "" || strings.HasPrefix(link.Path, "/") {
				continue
			}
			resource := path.Join(path.Dir(name), link.Path)
			data, err := fs.ReadFile(bundle, resource)
			if err != nil {
				return fmt.Errorf("%s: linked resource %s: %w", name, resource, err)
			}
			if strings.HasPrefix(resource, "templates/agent/") {
				destination := filepath.Join("/workspace/.agent", strings.TrimPrefix(resource, "templates/agent/"))
				copied, ok := written[destination]
				if !ok || !bytes.Equal(copied, data) {
					return fmt.Errorf("%s: linked resource %s not copied unchanged", name, resource)
				}
			}
		}
		return nil
	})
}
