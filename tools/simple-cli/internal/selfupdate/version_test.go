package selfupdate

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   Version
		wantOK bool
	}{
		{"three numbers", "1.4.0", Version{1, 4, 0}, true},
		{"with the tag's v", "v2.10.3", Version{2, 10, 3}, true},
		{"zeroes", "0.0.0", Version{0, 0, 0}, true},
		{"a source build", "dev", Version{}, false},
		{"empty", "", Version{}, false},
		{"two numbers", "1.4", Version{}, false},
		{"four numbers", "1.4.0.1", Version{}, false},
		{"a pre-release", "1.4.0-rc1", Version{}, false},
		{"an empty part", "1..0", Version{}, false},
		{"a signed part", "+1.4.0", Version{}, false},
		{"a letter", "1.x.0", Version{}, false},
		{"a number too large to hold", "99999999999999999999.0.0", Version{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseVersion(tt.text)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("ParseVersion(%q) = %v, %v; want %v, %v", tt.text, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	if got := (Version{1, 4, 0}).String(); got != "1.4.0" {
		t.Errorf("String() = %q, want 1.4.0", got)
	}
}

func TestVersionNewerThan(t *testing.T) {
	tests := []struct {
		name  string
		v     Version
		other Version
		want  bool
	}{
		{"a later major", Version{2, 0, 0}, Version{1, 9, 9}, true},
		{"an earlier major", Version{1, 9, 9}, Version{2, 0, 0}, false},
		{"a later minor", Version{1, 5, 0}, Version{1, 4, 9}, true},
		{"an earlier minor", Version{1, 4, 9}, Version{1, 5, 0}, false},
		{"a later patch", Version{1, 4, 1}, Version{1, 4, 0}, true},
		{"an earlier patch", Version{1, 4, 0}, Version{1, 4, 1}, false},
		{"the same", Version{1, 4, 0}, Version{1, 4, 0}, false},
		{"numbers, not text", Version{1, 10, 0}, Version{1, 9, 0}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.NewerThan(tt.other); got != tt.want {
				t.Errorf("%v.NewerThan(%v) = %v, want %v", tt.v, tt.other, got, tt.want)
			}
		})
	}
}
