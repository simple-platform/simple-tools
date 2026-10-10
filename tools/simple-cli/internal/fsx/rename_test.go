package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOSFileSystem_Rename(t *testing.T) {
	fsys := OSFileSystem{}
	dir := t.TempDir()
	fresh := filepath.Join(dir, "program.new")
	program := filepath.Join(dir, "program")

	if err := fsys.WriteFile(program, []byte("old"), ExecPerm); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := fsys.WriteFile(fresh, []byte("new"), ExecPerm); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := fsys.Rename(fresh, program); err != nil {
		t.Fatalf("Rename() over an existing file error = %v", err)
	}

	content, err := fsys.ReadFile(program)
	if err != nil || string(content) != "new" {
		t.Errorf("after Rename the file holds %q (%v), want %q", content, err, "new")
	}
	if _, err := fsys.Stat(fresh); !os.IsNotExist(err) {
		t.Errorf("after Rename the old name still exists: %v", err)
	}

	if err := fsys.Rename(filepath.Join(dir, "missing"), program); err == nil {
		t.Error("Rename() of a missing file error = nil, want one")
	}
}

func TestMockFileSystem_Rename(t *testing.T) {
	broken := errors.New("broken on purpose")

	tests := []struct {
		name      string
		files     map[string][]byte
		renameErr error
		wantErr   error
		wantFiles map[string]string
	}{
		{
			name:      "a file takes a free name",
			files:     map[string][]byte{"a": []byte("one")},
			wantFiles: map[string]string{"b": "one"},
		},
		{
			name:      "a file takes a name that was held",
			files:     map[string][]byte{"a": []byte("one"), "b": []byte("two")},
			wantFiles: map[string]string{"b": "one"},
		},
		{
			name:      "a file that is not there cannot be moved",
			files:     map[string][]byte{"b": []byte("two")},
			wantErr:   os.ErrNotExist,
			wantFiles: map[string]string{"b": "two"},
		},
		{
			name:      "nothing at all cannot be moved either",
			files:     nil,
			wantErr:   os.ErrNotExist,
			wantFiles: map[string]string{},
		},
		{
			name:      "a failure the test asked for",
			files:     map[string][]byte{"a": []byte("one")},
			renameErr: broken,
			wantErr:   broken,
			wantFiles: map[string]string{"a": "one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockFileSystem{Files: tt.files, RenameErr: tt.renameErr}

			if err := mock.Rename("a", "b"); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Rename() error = %v, want %v", err, tt.wantErr)
			}

			if len(mock.Files) != len(tt.wantFiles) {
				t.Errorf("the mock holds %d files, want %d", len(mock.Files), len(tt.wantFiles))
			}
			for name, want := range tt.wantFiles {
				if got := string(mock.Files[name]); got != want {
					t.Errorf("%s holds %q, want %q", name, got, want)
				}
			}
		})
	}
}
