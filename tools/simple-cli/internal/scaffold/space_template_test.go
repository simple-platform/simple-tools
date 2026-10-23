package scaffold

import (
	"regexp"
	"strings"
	"testing"
)

// Every new space starts from the space template, so the template carries
// what the rules for spaces depend on: the UI Kit, which needs version 3 of
// the Space SDK, and the UI Kit's theme, which gives Tailwind's colour names
// the instance's brand colours. The theme only takes effect when Tailwind
// processes it, so it is imported right after Tailwind's own stylesheet.
func TestSpaceTemplateUsesTheUIKitAndItsTheme(t *testing.T) {
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	readTemplateJSON(t, "templates/space/package.json", &manifest)
	if sdk := manifest.Dependencies["@simpleplatform/sdk"]; !strings.HasPrefix(sdk, "^3.") {
		t.Errorf("@simpleplatform/sdk = %q, want a version 3 range, which the UI Kit needs", sdk)
	}
	if kit := manifest.Dependencies["@simpleplatform/ui-kit"]; kit == "" {
		t.Error("the space template does not depend on @simpleplatform/ui-kit")
	}

	theme, err := TemplatesFS.ReadFile("templates/space/src/styles/theme.css")
	if err != nil {
		t.Fatalf("read theme.css: %v", err)
	}
	var imports []string
	for _, match := range regexp.MustCompile(`(?m)^@import\s+"([^"]+)";`).FindAllStringSubmatch(string(theme), -1) {
		imports = append(imports, match[1])
	}
	if strings.Join(imports, ",") != "tailwindcss,@simpleplatform/ui-kit/theme.css" {
		t.Errorf("theme.css imports %v, want Tailwind and then the UI Kit's theme", imports)
	}
	if colour := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(?:rgb|hsl)a?\(`).FindString(string(theme)); colour != "" {
		t.Errorf("theme.css writes a colour of its own (%s); colours come from the UI Kit's brand theme", colour)
	}
}
