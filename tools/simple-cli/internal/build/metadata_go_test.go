package build

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// MockFileSystem implements fsx.FileSystem for testing
type MockFileSystem struct {
	files map[string]string
}

func (m *MockFileSystem) ReadFile(name string) ([]byte, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(content), nil
}

func (m *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	m.files[name] = string(data)
	return nil
}

func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	_, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return nil, nil // Simplified for testing
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

func (m *MockFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return nil, nil
}

func TestFindPayloadAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantStruct  string
		wantDesc    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid @Payload annotation with description",
			source: `package main

// ProcessData handles data processing
// @Payload DataInput
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantStruct: "DataInput",
			wantDesc:   "ProcessData handles data processing",
			wantErr:    false,
		},
		{
			name: "valid @Payload annotation without description",
			source: `package main

// @Payload SimplePayload
func Handler(ctx context.Context, payload SimplePayload) error {
	return nil
}`,
			wantStruct: "SimplePayload",
			wantDesc:   "",
			wantErr:    false,
		},
		{
			name: "valid @Payload annotation with multi-line description",
			source: `package main

// SendEmail sends an email to a user
// This function integrates with SendGrid API
// @Payload EmailPayload
func SendEmail(ctx context.Context, payload EmailPayload) error {
	return nil
}`,
			wantStruct: "EmailPayload",
			wantDesc:   "SendEmail sends an email to a user This function integrates with SendGrid API",
			wantErr:    false,
		},
		{
			name: "missing @Payload annotation",
			source: `package main

// ProcessData handles data processing
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantErr:     true,
			errContains: "@Payload annotation not found",
		},
		{
			name: "invalid @Payload annotation format (no struct name)",
			source: `package main

// ProcessData handles data processing
// @Payload
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantErr:     true,
			errContains: "invalid @Payload annotation format",
		},
		{
			name: "function without doc comment",
			source: `package main

func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantErr:     true,
			errContains: "@Payload annotation not found",
		},
		{
			name: "multiple functions, @Payload in second function",
			source: `package main

// Helper function
func Helper() error {
	return nil
}

// ProcessData handles data processing
// @Payload DataInput
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantStruct: "DataInput",
			wantDesc:   "ProcessData handles data processing",
			wantErr:    false,
		},
		{
			name: "@Payload annotation with extra whitespace",
			source: `package main

// ProcessData handles data processing
//   @Payload   DataInput  
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantStruct: "DataInput",
			wantDesc:   "ProcessData handles data processing",
			wantErr:    false,
		},
		{
			name: "description after @Payload annotation",
			source: `package main

// @Payload DataInput
// ProcessData handles data processing
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}`,
			wantStruct: "DataInput",
			wantDesc:   "ProcessData handles data processing",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the source code
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.source, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse test source: %v", err)
			}

			// Call findPayloadAnnotation
			got, err := findPayloadAnnotation(fset, file)

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("findPayloadAnnotation() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("findPayloadAnnotation() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("findPayloadAnnotation() unexpected error = %v", err)
				return
			}

			// Validate PayloadInfo
			if got == nil {
				t.Fatal("findPayloadAnnotation() returned nil PayloadInfo")
			}

			if got.StructName != tt.wantStruct {
				t.Errorf("findPayloadAnnotation() StructName = %v, want %v", got.StructName, tt.wantStruct)
			}

			if got.Description != tt.wantDesc {
				t.Errorf("findPayloadAnnotation() Description = %v, want %v", got.Description, tt.wantDesc)
			}

			if got.FuncNode == nil {
				t.Error("findPayloadAnnotation() FuncNode is nil")
			}
		})
	}
}

func TestExtractGoMetadata(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name: "missing main.go file",
			files: map[string]string{
				"/action/other.go": "package main",
			},
			wantErr:     true,
			errContains: "failed to read main.go",
		},
		{
			name: "invalid Go syntax",
			files: map[string]string{
				"/action/main.go": "package main\n\nthis is not valid go code",
			},
			wantErr:     true,
			errContains: "failed to parse main.go",
		},
		{
			name: "missing @Payload annotation",
			files: map[string]string{
				"/action/main.go": `package main

func Handler() error {
	return nil
}`,
			},
			wantErr:     true,
			errContains: "@Payload annotation not found",
		},
		{
			name: "valid @Payload annotation with simple struct",
			files: map[string]string{
				"/action/main.go": `package main

// Handler processes data
// @Payload Input
func Handler(ctx context.Context, payload Input) error {
	return nil
}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}`,
			},
			wantErr: false,
		},
		{
			name: "non-existent struct reference",
			files: map[string]string{
				"/action/main.go": `package main

// Handler processes data
// @Payload NonExistentStruct
func Handler(ctx context.Context, payload Input) error {
	return nil
}

type Input struct {
	Name string
}`,
			},
			wantErr:     true,
			errContains: "struct 'NonExistentStruct' not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock filesystem
			fs := &MockFileSystem{files: tt.files}

			// Call extractGoMetadata
			_, err := extractGoMetadata(fs, "/action")

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("extractGoMetadata() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("extractGoMetadata() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("extractGoMetadata() unexpected error = %v", err)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure PayloadInfo has the expected fields (compile-time check)
var _ = PayloadInfo{
	StructName:  "",
	Description: "",
	FuncNode:    (*ast.FuncDecl)(nil),
}

func TestFindStruct(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		structName  string
		wantFields  int
		wantErr     bool
		errContains string
		checkFields func(*testing.T, *StructInfo)
	}{
		{
			name: "simple struct with primitive fields",
			source: `package main

type Input struct {
	Name string
	Age  int
}`,
			structName: "Input",
			wantFields: 2,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if len(info.Fields) != 2 {
					t.Fatalf("expected 2 fields, got %d", len(info.Fields))
				}
				if info.Fields[0].Name != "Name" {
					t.Errorf("field[0].Name = %v, want 'Name'", info.Fields[0].Name)
				}
				if info.Fields[1].Name != "Age" {
					t.Errorf("field[1].Name = %v, want 'Age'", info.Fields[1].Name)
				}
			},
		},
		{
			name: "struct with json tags",
			source: `package main

type Input struct {
	Name string ` + "`json:\"name\"`" + `
	Age  int    ` + "`json:\"age\"`" + `
}`,
			structName: "Input",
			wantFields: 2,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Tag != `json:"name"` {
					t.Errorf("field[0].Tag = %v, want 'json:\"name\"'", info.Fields[0].Tag)
				}
				if info.Fields[1].Tag != `json:"age"` {
					t.Errorf("field[1].Tag = %v, want 'json:\"age\"'", info.Fields[1].Tag)
				}
			},
		},
		{
			name: "struct with field comments",
			source: `package main

type Input struct {
	// User's full name
	Name string
	// User's age in years
	Age  int
}`,
			structName: "Input",
			wantFields: 2,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Comment != "User's full name" {
					t.Errorf("field[0].Comment = %v, want 'User's full name'", info.Fields[0].Comment)
				}
				if info.Fields[1].Comment != "User's age in years" {
					t.Errorf("field[1].Comment = %v, want 'User's age in years'", info.Fields[1].Comment)
				}
			},
		},
		{
			name: "struct with multi-line field comments",
			source: `package main

type Input struct {
	// User's full name
	// This should be a valid string
	Name string
}`,
			structName: "Input",
			wantFields: 1,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				expected := "User's full name This should be a valid string"
				if info.Fields[0].Comment != expected {
					t.Errorf("field[0].Comment = %v, want %v", info.Fields[0].Comment, expected)
				}
			},
		},
		{
			name: "struct with nested struct field",
			source: `package main

type Address struct {
	Street string
	City   string
}

type Input struct {
	Name    string
	Address Address
}`,
			structName: "Input",
			wantFields: 2,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[1].Name != "Address" {
					t.Errorf("field[1].Name = %v, want 'Address'", info.Fields[1].Name)
				}
			},
		},
		{
			name: "struct with pointer field",
			source: `package main

type Input struct {
	Name *string
}`,
			structName: "Input",
			wantFields: 1,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Name != "Name" {
					t.Errorf("field[0].Name = %v, want 'Name'", info.Fields[0].Name)
				}
			},
		},
		{
			name: "struct with slice field",
			source: `package main

type Input struct {
	Tags []string
}`,
			structName: "Input",
			wantFields: 1,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Name != "Tags" {
					t.Errorf("field[0].Name = %v, want 'Tags'", info.Fields[0].Name)
				}
			},
		},
		{
			name: "struct with map field",
			source: `package main

type Input struct {
	Metadata map[string]string
}`,
			structName: "Input",
			wantFields: 1,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Name != "Metadata" {
					t.Errorf("field[0].Name = %v, want 'Metadata'", info.Fields[0].Name)
				}
			},
		},
		{
			name: "struct with complex tags",
			source: `package main

type Input struct {
	Email string ` + "`json:\"email\" jsonschema:\"required,pattern=^[^@]+@[^@]+$\"`" + `
}`,
			structName: "Input",
			wantFields: 1,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				expected := `json:"email" jsonschema:"required,pattern=^[^@]+@[^@]+$"`
				if info.Fields[0].Tag != expected {
					t.Errorf("field[0].Tag = %v, want %v", info.Fields[0].Tag, expected)
				}
			},
		},
		{
			name: "empty struct",
			source: `package main

type Input struct {
}`,
			structName: "Input",
			wantFields: 0,
			wantErr:    false,
		},
		{
			name: "struct not found",
			source: `package main

type Input struct {
	Name string
}`,
			structName:  "NonExistent",
			wantErr:     true,
			errContains: "struct 'NonExistent' not found",
		},
		{
			name: "type is not a struct",
			source: `package main

type Input string`,
			structName:  "Input",
			wantErr:     true,
			errContains: "type 'Input' is not a struct",
		},
		{
			name: "multiple structs, find specific one",
			source: `package main

type First struct {
	A string
}

type Second struct {
	B int
}

type Third struct {
	C bool
}`,
			structName: "Second",
			wantFields: 1,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Name != "B" {
					t.Errorf("field[0].Name = %v, want 'B'", info.Fields[0].Name)
				}
			},
		},
		{
			name: "struct with inline struct field",
			source: `package main

type Input struct {
	Name string
	Meta struct {
		Version int
	}
}`,
			structName: "Input",
			wantFields: 2,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[1].Name != "Meta" {
					t.Errorf("field[1].Name = %v, want 'Meta'", info.Fields[1].Name)
				}
			},
		},
		{
			name: "struct with multiple fields on same line",
			source: `package main

type Input struct {
	X, Y int
}`,
			structName: "Input",
			wantFields: 2,
			wantErr:    false,
			checkFields: func(t *testing.T, info *StructInfo) {
				if info.Fields[0].Name != "X" {
					t.Errorf("field[0].Name = %v, want 'X'", info.Fields[0].Name)
				}
				if info.Fields[1].Name != "Y" {
					t.Errorf("field[1].Name = %v, want 'Y'", info.Fields[1].Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the source code
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.source, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse test source: %v", err)
			}

			// Call findStruct
			got, err := findStruct(fset, []*ast.File{file}, tt.structName)

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("findStruct() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("findStruct() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("findStruct() unexpected error = %v", err)
				return
			}

			// Validate StructInfo
			if got == nil {
				t.Fatal("findStruct() returned nil StructInfo")
			}

			if got.StructType == nil {
				t.Error("findStruct() StructType is nil")
			}

			if len(got.Fields) != tt.wantFields {
				t.Errorf("findStruct() field count = %d, want %d", len(got.Fields), tt.wantFields)
			}

			// Run custom field checks if provided
			if tt.checkFields != nil {
				tt.checkFields(t, got)
			}
		})
	}
}

func TestExtractFieldInfo(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		structName string
		wantFields []FieldInfo
	}{
		{
			name: "extract all field information",
			source: `package main

type Input struct {
	// User's name
	Name string ` + "`json:\"name\"`" + `
	// User's age
	Age  int    ` + "`json:\"age\"`" + `
}`,
			structName: "Input",
			wantFields: []FieldInfo{
				{
					Name:    "Name",
					Tag:     `json:"name"`,
					Comment: "User's name",
				},
				{
					Name:    "Age",
					Tag:     `json:"age"`,
					Comment: "User's age",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the source code
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.source, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse test source: %v", err)
			}

			// Find the struct
			structInfo, err := findStruct(fset, []*ast.File{file}, tt.structName)
			if err != nil {
				t.Fatalf("findStruct() error = %v", err)
			}

			// Validate extracted fields
			if len(structInfo.Fields) != len(tt.wantFields) {
				t.Fatalf("field count = %d, want %d", len(structInfo.Fields), len(tt.wantFields))
			}

			for i, want := range tt.wantFields {
				got := structInfo.Fields[i]
				if got.Name != want.Name {
					t.Errorf("field[%d].Name = %v, want %v", i, got.Name, want.Name)
				}
				if got.Tag != want.Tag {
					t.Errorf("field[%d].Tag = %v, want %v", i, got.Tag, want.Tag)
				}
				if got.Comment != want.Comment {
					t.Errorf("field[%d].Comment = %v, want %v", i, got.Comment, want.Comment)
				}
			}
		})
	}
}

// Ensure StructInfo has the expected fields (compile-time check)
var _ = StructInfo{
	StructType: (*ast.StructType)(nil),
	Fields:     []FieldInfo{},
}

// Ensure FieldInfo has the expected fields (compile-time check)
var _ = FieldInfo{
	Name:    "",
	Type:    nil,
	Tag:     "",
	Comment: "",
}

func TestParseStructTag(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want StructTagInfo
	}{
		{
			name: "json tag with name only",
			tag:  `json:"email"`,
			want: StructTagInfo{
				JSONName: "email",
				Omit:     false,
				Optional: false,
				Required: false,
			},
		},
		{
			name: "json tag with omit marker",
			tag:  `json:"-"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     true,
				Optional: false,
				Required: false,
			},
		},
		{
			name: "json tag with omitempty",
			tag:  `json:",omitempty"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: true,
				Required: false,
			},
		},
		{
			name: "json tag with name and omitempty",
			tag:  `json:"name,omitempty"`,
			want: StructTagInfo{
				JSONName: "name",
				Omit:     false,
				Optional: true,
				Required: false,
			},
		},
		{
			name: "jsonschema tag with required",
			tag:  `jsonschema:"required"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: true,
			},
		},
		{
			name: "jsonschema tag with default value",
			tag:  `jsonschema:"default=test"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Default:  "test",
			},
		},
		{
			name: "jsonschema tag with minimum",
			tag:  `jsonschema:"minimum=0"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Min:      floatPtr(0),
			},
		},
		{
			name: "jsonschema tag with maximum",
			tag:  `jsonschema:"maximum=100"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Max:      floatPtr(100),
			},
		},
		{
			name: "jsonschema tag with pattern",
			tag:  `jsonschema:"pattern=^[a-z]+$"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Pattern:  "^[a-z]+$",
			},
		},
		{
			name: "jsonschema tag with multiple constraints",
			tag:  `jsonschema:"required,minimum=0,maximum=120"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: true,
				Min:      floatPtr(0),
				Max:      floatPtr(120),
			},
		},
		{
			name: "combined json and jsonschema tags",
			tag:  `json:"email" jsonschema:"required,pattern=^[^@]+@[^@]+$"`,
			want: StructTagInfo{
				JSONName: "email",
				Omit:     false,
				Optional: false,
				Required: true,
				Pattern:  "^[^@]+@[^@]+$",
			},
		},
		{
			name: "combined json with omitempty and jsonschema required",
			tag:  `json:"name,omitempty" jsonschema:"required"`,
			want: StructTagInfo{
				JSONName: "name",
				Omit:     false,
				Optional: true,
				Required: true,
			},
		},
		{
			name: "jsonschema with all constraints",
			tag:  `jsonschema:"required,default=18,minimum=0,maximum=120,pattern=^[0-9]+$"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: true,
				Default:  "18",
				Min:      floatPtr(0),
				Max:      floatPtr(120),
				Pattern:  "^[0-9]+$",
			},
		},
		{
			name: "empty tag",
			tag:  ``,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
			},
		},
		{
			name: "tag with no json or jsonschema",
			tag:  `xml:"data"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
			},
		},
		{
			name: "jsonschema with decimal minimum and maximum",
			tag:  `jsonschema:"minimum=0.5,maximum=99.9"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Min:      floatPtr(0.5),
				Max:      floatPtr(99.9),
			},
		},
		{
			name: "jsonschema with negative minimum",
			tag:  `jsonschema:"minimum=-10,maximum=10"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Min:      floatPtr(-10),
				Max:      floatPtr(10),
			},
		},
		{
			name: "complex pattern with special characters",
			tag:  `jsonschema:"pattern=^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Pattern:  `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$`,
			},
		},
		{
			name: "default value with spaces",
			tag:  `jsonschema:"default=hello world"`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: false,
				Default:  "hello world",
			},
		},
		{
			name: "json tag with extra whitespace",
			tag:  `json:"name , omitempty"`,
			want: StructTagInfo{
				JSONName: "name",
				Omit:     false,
				Optional: true,
				Required: false,
			},
		},
		{
			name: "jsonschema with whitespace around constraints",
			tag:  `jsonschema:" required , minimum = 0 , maximum = 100 "`,
			want: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
				Required: true,
				Min:      floatPtr(0),
				Max:      floatPtr(100),
			},
		},
		{
			name: "full example from design doc",
			tag:  `json:"age" jsonschema:"default=18,minimum=0,maximum=120"`,
			want: StructTagInfo{
				JSONName: "age",
				Omit:     false,
				Optional: false,
				Required: false,
				Default:  "18",
				Min:      floatPtr(0),
				Max:      floatPtr(120),
			},
		},
		{
			name: "email example from design doc",
			tag:  `json:"email,omitempty" jsonschema:"pattern=^[^@]+@[^@]+$"`,
			want: StructTagInfo{
				JSONName: "email",
				Omit:     false,
				Optional: true,
				Required: false,
				Pattern:  "^[^@]+@[^@]+$",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStructTag(tt.tag)

			if got.JSONName != tt.want.JSONName {
				t.Errorf("JSONName = %v, want %v", got.JSONName, tt.want.JSONName)
			}
			if got.Omit != tt.want.Omit {
				t.Errorf("Omit = %v, want %v", got.Omit, tt.want.Omit)
			}
			if got.Optional != tt.want.Optional {
				t.Errorf("Optional = %v, want %v", got.Optional, tt.want.Optional)
			}
			if got.Required != tt.want.Required {
				t.Errorf("Required = %v, want %v", got.Required, tt.want.Required)
			}
			if got.Default != tt.want.Default {
				t.Errorf("Default = %v, want %v", got.Default, tt.want.Default)
			}
			if !floatPtrEqual(got.Min, tt.want.Min) {
				t.Errorf("Min = %v, want %v", formatFloatPtr(got.Min), formatFloatPtr(tt.want.Min))
			}
			if !floatPtrEqual(got.Max, tt.want.Max) {
				t.Errorf("Max = %v, want %v", formatFloatPtr(got.Max), formatFloatPtr(tt.want.Max))
			}
			if got.Pattern != tt.want.Pattern {
				t.Errorf("Pattern = %v, want %v", got.Pattern, tt.want.Pattern)
			}
		})
	}
}

func TestExtractTag(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		key  string
		want string
	}{
		{
			name: "extract json tag",
			tag:  `json:"name"`,
			key:  "json",
			want: "name",
		},
		{
			name: "extract jsonschema tag",
			tag:  `jsonschema:"required"`,
			key:  "jsonschema",
			want: "required",
		},
		{
			name: "extract from multiple tags",
			tag:  `json:"name" jsonschema:"required"`,
			key:  "json",
			want: "name",
		},
		{
			name: "extract second tag from multiple",
			tag:  `json:"name" jsonschema:"required"`,
			key:  "jsonschema",
			want: "required",
		},
		{
			name: "tag not present",
			tag:  `json:"name"`,
			key:  "xml",
			want: "",
		},
		{
			name: "empty tag string",
			tag:  ``,
			key:  "json",
			want: "",
		},
		{
			name: "tag with complex value",
			tag:  `json:"name,omitempty"`,
			key:  "json",
			want: "name,omitempty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTag(tt.tag, tt.key)
			if got != tt.want {
				t.Errorf("extractTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJSONTag(t *testing.T) {
	tests := []struct {
		name     string
		jsonTag  string
		wantInfo StructTagInfo
	}{
		{
			name:    "name only",
			jsonTag: "email",
			wantInfo: StructTagInfo{
				JSONName: "email",
				Omit:     false,
				Optional: false,
			},
		},
		{
			name:    "omit marker",
			jsonTag: "-",
			wantInfo: StructTagInfo{
				JSONName: "",
				Omit:     true,
				Optional: false,
			},
		},
		{
			name:    "omitempty only",
			jsonTag: ",omitempty",
			wantInfo: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: true,
			},
		},
		{
			name:    "name with omitempty",
			jsonTag: "name,omitempty",
			wantInfo: StructTagInfo{
				JSONName: "name",
				Omit:     false,
				Optional: true,
			},
		},
		{
			name:    "empty string",
			jsonTag: "",
			wantInfo: StructTagInfo{
				JSONName: "",
				Omit:     false,
				Optional: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := StructTagInfo{}
			parseJSONTag(&info, tt.jsonTag)

			if info.JSONName != tt.wantInfo.JSONName {
				t.Errorf("JSONName = %v, want %v", info.JSONName, tt.wantInfo.JSONName)
			}
			if info.Omit != tt.wantInfo.Omit {
				t.Errorf("Omit = %v, want %v", info.Omit, tt.wantInfo.Omit)
			}
			if info.Optional != tt.wantInfo.Optional {
				t.Errorf("Optional = %v, want %v", info.Optional, tt.wantInfo.Optional)
			}
		})
	}
}

func TestParseJSONSchemaTag(t *testing.T) {
	tests := []struct {
		name      string
		schemaTag string
		wantInfo  StructTagInfo
	}{
		{
			name:      "required flag",
			schemaTag: "required",
			wantInfo: StructTagInfo{
				Required: true,
			},
		},
		{
			name:      "default value",
			schemaTag: "default=test",
			wantInfo: StructTagInfo{
				Default: "test",
			},
		},
		{
			name:      "minimum value",
			schemaTag: "minimum=0",
			wantInfo: StructTagInfo{
				Min: floatPtr(0),
			},
		},
		{
			name:      "maximum value",
			schemaTag: "maximum=100",
			wantInfo: StructTagInfo{
				Max: floatPtr(100),
			},
		},
		{
			name:      "pattern",
			schemaTag: "pattern=^[a-z]+$",
			wantInfo: StructTagInfo{
				Pattern: "^[a-z]+$",
			},
		},
		{
			name:      "multiple constraints",
			schemaTag: "required,minimum=0,maximum=100",
			wantInfo: StructTagInfo{
				Required: true,
				Min:      floatPtr(0),
				Max:      floatPtr(100),
			},
		},
		{
			name:      "all constraints",
			schemaTag: "required,default=50,minimum=0,maximum=100,pattern=^[0-9]+$",
			wantInfo: StructTagInfo{
				Required: true,
				Default:  "50",
				Min:      floatPtr(0),
				Max:      floatPtr(100),
				Pattern:  "^[0-9]+$",
			},
		},
		{
			name:      "empty string",
			schemaTag: "",
			wantInfo:  StructTagInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := StructTagInfo{}
			parseJSONSchemaTag(&info, tt.schemaTag)

			if info.Required != tt.wantInfo.Required {
				t.Errorf("Required = %v, want %v", info.Required, tt.wantInfo.Required)
			}
			if info.Default != tt.wantInfo.Default {
				t.Errorf("Default = %v, want %v", info.Default, tt.wantInfo.Default)
			}
			if !floatPtrEqual(info.Min, tt.wantInfo.Min) {
				t.Errorf("Min = %v, want %v", formatFloatPtr(info.Min), formatFloatPtr(tt.wantInfo.Min))
			}
			if !floatPtrEqual(info.Max, tt.wantInfo.Max) {
				t.Errorf("Max = %v, want %v", formatFloatPtr(info.Max), formatFloatPtr(tt.wantInfo.Max))
			}
			if info.Pattern != tt.wantInfo.Pattern {
				t.Errorf("Pattern = %v, want %v", info.Pattern, tt.wantInfo.Pattern)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{
			name:    "integer",
			input:   "42",
			want:    42.0,
			wantErr: false,
		},
		{
			name:    "decimal",
			input:   "3.14",
			want:    3.14,
			wantErr: false,
		},
		{
			name:    "negative",
			input:   "-10",
			want:    -10.0,
			wantErr: false,
		},
		{
			name:    "negative decimal",
			input:   "-3.14",
			want:    -3.14,
			wantErr: false,
		},
		{
			name:    "zero",
			input:   "0",
			want:    0.0,
			wantErr: false,
		},
		{
			name:    "with whitespace",
			input:   "  42  ",
			want:    42.0,
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "abc",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFloat(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseFloat() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseFloat() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("parseFloat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper functions for testing

func floatPtr(f float64) *float64 {
	return &f
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func formatFloatPtr(f *float64) string {
	if f == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *f)
}

// Ensure StructTagInfo has the expected fields (compile-time check)
var _ = StructTagInfo{
	JSONName: "",
	Omit:     false,
	Optional: false,
	Required: false,
	Default:  "",
	Min:      (*float64)(nil),
	Max:      (*float64)(nil),
	Pattern:  "",
}

func TestGenerateSchemaFromStruct(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		structName string
		wantSchema JSONSchema
		wantErr    bool
	}{
		{
			name: "simple struct with primitive types",
			source: `package main

type Input struct {
	Name string ` + "`json:\"name\"`" + `
	Age  int    ` + "`json:\"age\"`" + `
	Active bool ` + "`json:\"active\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":   {Type: "string"},
					"age":    {Type: "number"},
					"active": {Type: "boolean"},
				},
				Required: []string{"name", "age", "active"},
			},
			wantErr: false,
		},
		{
			name: "struct with optional fields (omitempty)",
			source: `package main

type Input struct {
	Name  string ` + "`json:\"name\"`" + `
	Email string ` + "`json:\"email,omitempty\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":  {Type: "string"},
					"email": {Type: "string"},
				},
				Required: []string{"name"},
			},
			wantErr: false,
		},
		{
			name: "struct with jsonschema required tag",
			source: `package main

type Input struct {
	Name  string ` + "`json:\"name,omitempty\" jsonschema:\"required\"`" + `
	Email string ` + "`json:\"email,omitempty\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":  {Type: "string"},
					"email": {Type: "string"},
				},
				Required: []string{"name"},
			},
			wantErr: false,
		},
		{
			name: "struct with omitted field (json:\"-\")",
			source: `package main

type Input struct {
	Name     string ` + "`json:\"name\"`" + `
	Internal string ` + "`json:\"-\"`" + `
	Age      int    ` + "`json:\"age\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string"},
					"age":  {Type: "number"},
				},
				Required: []string{"name", "age"},
			},
			wantErr: false,
		},
		{
			name: "struct with slice field",
			source: `package main

type Input struct {
	Tags []string ` + "`json:\"tags\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"tags": {
						Type:  "array",
						Items: &Property{Type: "string"},
					},
				},
				Required: []string{"tags"},
			},
			wantErr: false,
		},
		{
			name: "struct with map field",
			source: `package main

type Input struct {
	Metadata map[string]string ` + "`json:\"metadata\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"metadata": {
						Type:                 "object",
						AdditionalProperties: &Property{Type: "string"},
					},
				},
				Required: []string{"metadata"},
			},
			wantErr: false,
		},
		{
			name: "struct with pointer field",
			source: `package main

type Input struct {
	Name *string ` + "`json:\"name\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string"},
				},
				Required: []string{"name"},
			},
			wantErr: false,
		},
		{
			name: "struct with nested struct",
			source: `package main

type Address struct {
	Street string ` + "`json:\"street\"`" + `
	City   string ` + "`json:\"city\"`" + `
}

type Input struct {
	Name    string  ` + "`json:\"name\"`" + `
	Address Address ` + "`json:\"address\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":    {Type: "string"},
					"address": {Type: "object"}, // Named types are treated as objects
				},
				Required: []string{"name", "address"},
			},
			wantErr: false,
		},
		{
			name: "struct with inline struct",
			source: `package main

type Input struct {
	Name string ` + "`json:\"name\"`" + `
	Meta struct {
		Version int ` + "`json:\"version\"`" + `
	} ` + "`json:\"meta\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string"},
					"meta": {
						Type: "object",
						Properties: map[string]Property{
							"version": {Type: "number"},
						},
					},
				},
				Required: []string{"name", "meta"},
			},
			wantErr: false,
		},
		{
			name: "struct with constraints (default, min, max, pattern)",
			source: `package main

type Input struct {
	Age   int    ` + "`json:\"age\" jsonschema:\"default=18,minimum=0,maximum=120\"`" + `
	Email string ` + "`json:\"email\" jsonschema:\"pattern=^[^@]+@[^@]+$\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"age": {
						Type:    "number",
						Default: 18.0,
						Minimum: floatPtr(0),
						Maximum: floatPtr(120),
					},
					"email": {
						Type:    "string",
						Pattern: "^[^@]+@[^@]+$",
					},
				},
				Required: []string{"age", "email"},
			},
			wantErr: false,
		},
		{
			name: "struct with field comments",
			source: `package main

type Input struct {
	// User's full name
	Name string ` + "`json:\"name\"`" + `
	// User's age in years
	Age  int    ` + "`json:\"age\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "User's full name"},
					"age":  {Type: "number", Description: "User's age in years"},
				},
				Required: []string{"name", "age"},
			},
			wantErr: false,
		},
		{
			name: "struct with various numeric types",
			source: `package main

type Input struct {
	Int8Val    int8    ` + "`json:\"int8\"`" + `
	Int16Val   int16   ` + "`json:\"int16\"`" + `
	Int32Val   int32   ` + "`json:\"int32\"`" + `
	Int64Val   int64   ` + "`json:\"int64\"`" + `
	UintVal    uint    ` + "`json:\"uint\"`" + `
	Float32Val float32 ` + "`json:\"float32\"`" + `
	Float64Val float64 ` + "`json:\"float64\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"int8":    {Type: "number"},
					"int16":   {Type: "number"},
					"int32":   {Type: "number"},
					"int64":   {Type: "number"},
					"uint":    {Type: "number"},
					"float32": {Type: "number"},
					"float64": {Type: "number"},
				},
				Required: []string{"int8", "int16", "int32", "int64", "uint", "float32", "float64"},
			},
			wantErr: false,
		},
		{
			name: "empty struct",
			source: `package main

type Input struct {
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type:       "object",
				Properties: map[string]Property{},
				Required:   []string{},
			},
			wantErr: false,
		},
		{
			name: "nested struct handling (3+ levels deep)",
			source: `package main

type Level3 struct {
	Value string ` + "`json:\"value\"`" + `
}

type Level2 struct {
	Name   string ` + "`json:\"name\"`" + `
	Level3 Level3 ` + "`json:\"level3\"`" + `
}

type Level1 struct {
	ID     int    ` + "`json:\"id\"`" + `
	Level2 Level2 ` + "`json:\"level2\"`" + `
}

type Input struct {
	Root   string ` + "`json:\"root\"`" + `
	Level1 Level1 ` + "`json:\"level1\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"root":   {Type: "string"},
					"level1": {Type: "object"}, // Named types are treated as objects
				},
				Required: []string{"root", "level1"},
			},
			wantErr: false,
		},
		{
			name: "nested inline struct handling (3+ levels deep)",
			source: `package main

type Input struct {
	Root string ` + "`json:\"root\"`" + `
	Level1 struct {
		Name string ` + "`json:\"name\"`" + `
		Level2 struct {
			ID string ` + "`json:\"id\"`" + `
			Level3 struct {
				Value string ` + "`json:\"value\"`" + `
			} ` + "`json:\"level3\"`" + `
		} ` + "`json:\"level2\"`" + `
	} ` + "`json:\"level1\"`" + `
}`,
			structName: "Input",
			wantSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"root": {Type: "string"},
					"level1": {
						Type: "object",
						Properties: map[string]Property{
							"name": {Type: "string"},
							"level2": {
								Type: "object",
								Properties: map[string]Property{
									"id": {Type: "string"},
									"level3": {
										Type: "object",
										Properties: map[string]Property{
											"value": {Type: "string"},
										},
									},
								},
							},
						},
					},
				},
				Required: []string{"root", "level1"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the source code
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.source, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse test source: %v", err)
			}

			// Find the struct
			structInfo, err := findStruct(fset, []*ast.File{file}, tt.structName)
			if err != nil {
				t.Fatalf("findStruct() error = %v", err)
			}

			// Generate schema
			got, err := generateSchemaFromStruct(structInfo)

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("generateSchemaFromStruct() expected error, got nil")
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("generateSchemaFromStruct() unexpected error = %v", err)
				return
			}

			// Validate schema
			if got.Type != tt.wantSchema.Type {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantSchema.Type)
			}

			// Check properties count
			if len(got.Properties) != len(tt.wantSchema.Properties) {
				t.Errorf("Properties count = %d, want %d", len(got.Properties), len(tt.wantSchema.Properties))
			}

			// Check each property
			for propName, wantProp := range tt.wantSchema.Properties {
				gotProp, ok := got.Properties[propName]
				if !ok {
					t.Errorf("Property %q not found in generated schema", propName)
					continue
				}

				if gotProp.Type != wantProp.Type {
					t.Errorf("Property %q: Type = %v, want %v", propName, gotProp.Type, wantProp.Type)
				}

				if gotProp.Description != wantProp.Description {
					t.Errorf("Property %q: Description = %v, want %v", propName, gotProp.Description, wantProp.Description)
				}

				// Check Items for arrays
				if wantProp.Items != nil {
					if gotProp.Items == nil {
						t.Errorf("Property %q: Items is nil, want %v", propName, wantProp.Items)
					} else if gotProp.Items.Type != wantProp.Items.Type {
						t.Errorf("Property %q: Items.Type = %v, want %v", propName, gotProp.Items.Type, wantProp.Items.Type)
					}
				}

				// Check AdditionalProperties for maps
				if wantProp.AdditionalProperties != nil {
					if gotProp.AdditionalProperties == nil {
						t.Errorf("Property %q: AdditionalProperties is nil, want %v", propName, wantProp.AdditionalProperties)
					} else {
						// Type assert to *Property for comparison
						gotAddlProp, gotOk := gotProp.AdditionalProperties.(*Property)
						wantAddlProp, wantOk := wantProp.AdditionalProperties.(*Property)
						if gotOk && wantOk && gotAddlProp.Type != wantAddlProp.Type {
							t.Errorf("Property %q: AdditionalProperties.Type = %v, want %v", propName, gotAddlProp.Type, wantAddlProp.Type)
						}
					}
				}

				// Check nested properties for inline structs
				if len(wantProp.Properties) > 0 {
					if len(gotProp.Properties) != len(wantProp.Properties) {
						t.Errorf("Property %q: nested properties count = %d, want %d", propName, len(gotProp.Properties), len(wantProp.Properties))
					}
					for nestedName, wantNested := range wantProp.Properties {
						gotNested, ok := gotProp.Properties[nestedName]
						if !ok {
							t.Errorf("Property %q: nested property %q not found", propName, nestedName)
							continue
						}
						if gotNested.Type != wantNested.Type {
							t.Errorf("Property %q.%q: Type = %v, want %v", propName, nestedName, gotNested.Type, wantNested.Type)
						}
					}
				}

				// Check constraints
				if !floatPtrEqual(gotProp.Minimum, wantProp.Minimum) {
					t.Errorf("Property %q: Minimum = %v, want %v", propName, formatFloatPtr(gotProp.Minimum), formatFloatPtr(wantProp.Minimum))
				}
				if !floatPtrEqual(gotProp.Maximum, wantProp.Maximum) {
					t.Errorf("Property %q: Maximum = %v, want %v", propName, formatFloatPtr(gotProp.Maximum), formatFloatPtr(wantProp.Maximum))
				}
				if gotProp.Pattern != wantProp.Pattern {
					t.Errorf("Property %q: Pattern = %v, want %v", propName, gotProp.Pattern, wantProp.Pattern)
				}
				// Check default value (compare as interface{})
				if wantProp.Default != nil {
					if gotProp.Default == nil {
						t.Errorf("Property %q: Default is nil, want %v", propName, wantProp.Default)
					} else if fmt.Sprintf("%v", gotProp.Default) != fmt.Sprintf("%v", wantProp.Default) {
						t.Errorf("Property %q: Default = %v, want %v", propName, gotProp.Default, wantProp.Default)
					}
				}
			}

			// Check required array
			if len(got.Required) != len(tt.wantSchema.Required) {
				t.Errorf("Required count = %d, want %d", len(got.Required), len(tt.wantSchema.Required))
			}

			// Convert to map for easier comparison
			requiredMap := make(map[string]bool)
			for _, req := range got.Required {
				requiredMap[req] = true
			}

			for _, wantReq := range tt.wantSchema.Required {
				if !requiredMap[wantReq] {
					t.Errorf("Required field %q not found in generated schema", wantReq)
				}
			}
		})
	}
}

func TestExtractGoMetadata_EndToEnd(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string
		wantDesc       string
		wantProperties map[string]Property
		wantRequired   []string
		wantErr        bool
	}{
		{
			name: "complete Go action with all features",
			files: map[string]string{
				"/action/main.go": `package main

import "context"

// SendEmail sends a welcome email to a new user
// Integrates with SendGrid API
// @Payload EmailPayload
func SendEmail(ctx context.Context, payload EmailPayload) (*Response, error) {
	return nil, nil
}

// EmailPayload defines the email input
type EmailPayload struct {
	// User's email address (must be valid)
	Email string ` + "`json:\"email\" jsonschema:\"required,pattern=^[^@]+@[^@]+$\"`" + `
	
	// User's display name
	Name string ` + "`json:\"name\" jsonschema:\"required\"`" + `
	
	// Optional custom message
	Message string ` + "`json:\"message,omitempty\"`" + `
	
	// User's age (18-120)
	Age int ` + "`json:\"age\" jsonschema:\"default=18,minimum=0,maximum=120\"`" + `
	
	// Email preferences
	Preferences *EmailPreferences ` + "`json:\"preferences,omitempty\"`" + `
}

// EmailPreferences defines email settings
type EmailPreferences struct {
	// Send newsletter
	Newsletter bool ` + "`json:\"newsletter\"`" + `
	
	// Email frequency (daily, weekly, monthly)
	Frequency string ` + "`json:\"frequency\" jsonschema:\"pattern=^(daily|weekly|monthly)$\"`" + `
}

type Response struct {
	Success bool
}`,
			},
			wantDesc: "SendEmail sends a welcome email to a new user Integrates with SendGrid API",
			wantProperties: map[string]Property{
				"email": {
					Type:        "string",
					Description: "User's email address (must be valid)",
					Pattern:     "^[^@]+@[^@]+$",
				},
				"name": {
					Type:        "string",
					Description: "User's display name",
				},
				"message": {
					Type:        "string",
					Description: "Optional custom message",
				},
				"age": {
					Type:        "number",
					Description: "User's age (18-120)",
					Default:     18.0,
					Minimum:     floatPtr(0),
					Maximum:     floatPtr(120),
				},
				"preferences": {
					Type:        "object",
					Description: "Email preferences",
				},
			},
			wantRequired: []string{"email", "name", "age"},
			wantErr:      false,
		},
		{
			name: "Go action with arrays and maps",
			files: map[string]string{
				"/action/main.go": `package main

import "context"

// ProcessData processes user data
// @Payload DataInput
func ProcessData(ctx context.Context, payload DataInput) error {
	return nil
}

type DataInput struct {
	// List of tags
	Tags []string ` + "`json:\"tags\"`" + `
	
	// Metadata key-value pairs
	Metadata map[string]string ` + "`json:\"metadata\"`" + `
	
	// List of numbers
	Numbers []int ` + "`json:\"numbers\"`" + `
}`,
			},
			wantDesc: "ProcessData processes user data",
			wantProperties: map[string]Property{
				"tags": {
					Type:        "array",
					Description: "List of tags",
					Items:       &Property{Type: "string"},
				},
				"metadata": {
					Type:                 "object",
					Description:          "Metadata key-value pairs",
					AdditionalProperties: &Property{Type: "string"},
				},
				"numbers": {
					Type:        "array",
					Description: "List of numbers",
					Items:       &Property{Type: "number"},
				},
			},
			wantRequired: []string{"tags", "metadata", "numbers"},
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock filesystem
			fs := &MockFileSystem{files: tt.files}

			// Call extractGoMetadata
			metadata, err := extractGoMetadata(fs, "/action")

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("extractGoMetadata() expected error, got nil")
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("extractGoMetadata() unexpected error = %v", err)
				return
			}

			// Validate metadata
			if metadata == nil {
				t.Fatal("extractGoMetadata() returned nil metadata")
			}

			// Check description
			if metadata.Description != tt.wantDesc {
				t.Errorf("Description = %v, want %v", metadata.Description, tt.wantDesc)
			}

			// Check schema type
			if metadata.Schema.Type != "object" {
				t.Errorf("Schema.Type = %v, want 'object'", metadata.Schema.Type)
			}

			// Check properties
			for propName, wantProp := range tt.wantProperties {
				gotProp, ok := metadata.Schema.Properties[propName]
				if !ok {
					t.Errorf("Property %q not found in schema", propName)
					continue
				}

				if gotProp.Type != wantProp.Type {
					t.Errorf("Property %q: Type = %v, want %v", propName, gotProp.Type, wantProp.Type)
				}

				if gotProp.Description != wantProp.Description {
					t.Errorf("Property %q: Description = %v, want %v", propName, gotProp.Description, wantProp.Description)
				}

				if gotProp.Pattern != wantProp.Pattern {
					t.Errorf("Property %q: Pattern = %v, want %v", propName, gotProp.Pattern, wantProp.Pattern)
				}

				if !floatPtrEqual(gotProp.Minimum, wantProp.Minimum) {
					t.Errorf("Property %q: Minimum = %v, want %v", propName, formatFloatPtr(gotProp.Minimum), formatFloatPtr(wantProp.Minimum))
				}

				if !floatPtrEqual(gotProp.Maximum, wantProp.Maximum) {
					t.Errorf("Property %q: Maximum = %v, want %v", propName, formatFloatPtr(gotProp.Maximum), formatFloatPtr(wantProp.Maximum))
				}

				if wantProp.Default != nil {
					if gotProp.Default == nil {
						t.Errorf("Property %q: Default is nil, want %v", propName, wantProp.Default)
					} else if fmt.Sprintf("%v", gotProp.Default) != fmt.Sprintf("%v", wantProp.Default) {
						t.Errorf("Property %q: Default = %v, want %v", propName, gotProp.Default, wantProp.Default)
					}
				}

				// Check Items for arrays
				if wantProp.Items != nil {
					if gotProp.Items == nil {
						t.Errorf("Property %q: Items is nil", propName)
					} else if gotProp.Items.Type != wantProp.Items.Type {
						t.Errorf("Property %q: Items.Type = %v, want %v", propName, gotProp.Items.Type, wantProp.Items.Type)
					}
				}

				// Check AdditionalProperties for maps
				if wantProp.AdditionalProperties != nil {
					if gotProp.AdditionalProperties == nil {
						t.Errorf("Property %q: AdditionalProperties is nil", propName)
					} else {
						// Type assert to *Property for comparison
						gotAddlProp, gotOk := gotProp.AdditionalProperties.(*Property)
						wantAddlProp, wantOk := wantProp.AdditionalProperties.(*Property)
						if gotOk && wantOk && gotAddlProp.Type != wantAddlProp.Type {
							t.Errorf("Property %q: AdditionalProperties.Type = %v, want %v", propName, gotAddlProp.Type, wantAddlProp.Type)
						}
					}
				}
			}

			// Check required fields
			requiredMap := make(map[string]bool)
			for _, req := range metadata.Schema.Required {
				requiredMap[req] = true
			}

			for _, wantReq := range tt.wantRequired {
				if !requiredMap[wantReq] {
					t.Errorf("Required field %q not found in schema", wantReq)
				}
			}
		})
	}
}
func TestGeneratePropertyFromType_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		structName  string
		fieldName   string
		wantErr     bool
		errContains string
	}{
		{
			name: "unsupported type (channel)",
			source: `package main

type Input struct {
	Ch chan string ` + "`json:\"ch\"`" + `
}`,
			structName:  "Input",
			fieldName:   "Ch",
			wantErr:     true,
			errContains: "unsupported type",
		},
		{
			name: "unsupported map key type (int)",
			source: `package main

type Input struct {
	Data map[int]string ` + "`json:\"data\"`" + `
}`,
			structName:  "Input",
			fieldName:   "Data",
			wantErr:     true,
			errContains: "unsupported map key type",
		},
		{
			name: "nested error in array type",
			source: `package main

type Input struct {
	Channels []chan string ` + "`json:\"channels\"`" + `
}`,
			structName:  "Input",
			fieldName:   "Channels",
			wantErr:     true,
			errContains: "failed to generate array item schema",
		},
		{
			name: "nested error in map value type",
			source: `package main

type Input struct {
	ChannelMap map[string]chan int ` + "`json:\"channel_map\"`" + `
}`,
			structName:  "Input",
			fieldName:   "ChannelMap",
			wantErr:     true,
			errContains: "failed to generate map value schema",
		},
		{
			name: "nested error in inline struct field",
			source: `package main

type Input struct {
	Meta struct {
		Ch chan string ` + "`json:\"ch\"`" + `
	} ` + "`json:\"meta\"`" + `
}`,
			structName:  "Input",
			fieldName:   "Meta",
			wantErr:     true,
			errContains: "failed to generate schema for inline struct field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the source code
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.source, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse test source: %v", err)
			}

			// Find the struct
			structInfo, err := findStruct(fset, []*ast.File{file}, tt.structName)
			if err != nil {
				t.Fatalf("findStruct() error = %v", err)
			}

			// Generate schema (this should fail)
			_, err = generateSchemaFromStruct(structInfo)

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("generateSchemaFromStruct() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("generateSchemaFromStruct() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("generateSchemaFromStruct() unexpected error = %v", err)
			}
		})
	}
}

func TestSplitConstraints_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "pattern with commas",
			input: "required,pattern=^(a,b,c)$,minimum=0",
			want:  []string{"required", "pattern=^(a,b,c)$", "minimum=0"},
		},
		{
			name:  "pattern at end with commas",
			input: "required,minimum=0,pattern=^(a,b,c)$",
			want:  []string{"required", "minimum=0", "pattern=^(a,b,c)$"},
		},
		{
			name:  "multiple patterns (edge case)",
			input: "pattern=^a,b$,required,pattern=^c,d$",
			want:  []string{"pattern=^a,b$", "required", "pattern=^c,d$"},
		},
		{
			name:  "empty constraint",
			input: "",
			want:  []string{},
		},
		{
			name:  "single constraint",
			input: "required",
			want:  []string{"required"},
		},
		{
			name:  "pattern only",
			input: "pattern=^[a,b,c]+$",
			want:  []string{"pattern=^[a,b,c]+$"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitConstraints(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitConstraints() length = %d, want %d", len(got), len(tt.want))
				t.Errorf("got: %v", got)
				t.Errorf("want: %v", tt.want)
				return
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("splitConstraints()[%d] = %v, want %v", i, got[i], want)
				}
			}
		})
	}
}

func TestApplyConstraints_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		propType string
		tagInfo  StructTagInfo
		wantProp Property
	}{
		{
			name:     "boolean default true",
			propType: "boolean",
			tagInfo: StructTagInfo{
				Default: "true",
			},
			wantProp: Property{
				Type:    "boolean",
				Default: true,
			},
		},
		{
			name:     "boolean default false",
			propType: "boolean",
			tagInfo: StructTagInfo{
				Default: "false",
			},
			wantProp: Property{
				Type:    "boolean",
				Default: false,
			},
		},
		{
			name:     "boolean default invalid (kept as string)",
			propType: "boolean",
			tagInfo: StructTagInfo{
				Default: "maybe",
			},
			wantProp: Property{
				Type:    "boolean",
				Default: "maybe",
			},
		},
		{
			name:     "number default valid",
			propType: "number",
			tagInfo: StructTagInfo{
				Default: "42.5",
			},
			wantProp: Property{
				Type:    "number",
				Default: 42.5,
			},
		},
		{
			name:     "number default invalid (kept as string)",
			propType: "number",
			tagInfo: StructTagInfo{
				Default: "not-a-number",
			},
			wantProp: Property{
				Type:    "number",
				Default: "not-a-number",
			},
		},
		{
			name:     "string default",
			propType: "string",
			tagInfo: StructTagInfo{
				Default: "hello world",
			},
			wantProp: Property{
				Type:    "string",
				Default: "hello world",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prop := Property{Type: tt.propType}
			applyConstraints(&prop, tt.tagInfo)

			if prop.Type != tt.wantProp.Type {
				t.Errorf("Type = %v, want %v", prop.Type, tt.wantProp.Type)
			}

			if tt.wantProp.Default != nil {
				if prop.Default == nil {
					t.Errorf("Default is nil, want %v", tt.wantProp.Default)
				} else if fmt.Sprintf("%v", prop.Default) != fmt.Sprintf("%v", tt.wantProp.Default) {
					t.Errorf("Default = %v (%T), want %v (%T)", prop.Default, prop.Default, tt.wantProp.Default, tt.wantProp.Default)
				}
			}
		})
	}
}

func TestExtractTag_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		key  string
		want string
	}{
		{
			name: "malformed tag (no closing quote)",
			tag:  `json:"name`,
			key:  "json",
			want: "",
		},
		{
			name: "key at end of string",
			tag:  `xml:"data" json:"name"`,
			key:  "json",
			want: "name",
		},
		{
			name: "key with similar prefix",
			tag:  `jsonschema:"required" json:"name"`,
			key:  "json",
			want: "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTag(tt.tag, tt.key)
			if got != tt.want {
				t.Errorf("extractTag() = %v, want %v", got, tt.want)
			}
		})
	}
}
