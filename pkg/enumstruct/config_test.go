package enumstruct

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeConfig writes doc to a .enumstruct.yml in a fresh temp dir and returns
// the path.
func writeConfig(t *testing.T, doc string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".enumstruct.yml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestLoadConfigYAMLForms covers YAML spellings that are all valid and must
// therefore all be accepted. These previously round-tripped through a
// hand-rolled parser that silently returned empty or literal values for
// several of them, which made the linter fail open.
func TestLoadConfigYAMLForms(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want Config
	}{
		{
			name: "exclude_fields with two-space indent",
			doc:  "exclude_fields:\n  \"pkg/model.Foo\":\n    - DeprecatedField\n",
			want: Config{ExcludeFields: map[string][]string{"pkg/model.Foo": {"DeprecatedField"}}},
		},
		{
			name: "exclude_fields with four-space indent",
			doc:  "exclude_fields:\n    \"pkg/model.Foo\":\n      - DeprecatedField\n",
			want: Config{ExcludeFields: map[string][]string{"pkg/model.Foo": {"DeprecatedField"}}},
		},
		{
			name: "exclude_fields as a flow mapping",
			doc:  "exclude_fields: {\"pkg/model.Foo\": [\"DeprecatedField\"]}\n",
			want: Config{ExcludeFields: map[string][]string{"pkg/model.Foo": {"DeprecatedField"}}},
		},
		{
			name: "exclude_fields with multiple keys and values",
			doc:  "exclude_fields:\n  \"p.T\":\n    - A\n    - B\n  \"p.U\":\n    - C\n",
			want: Config{ExcludeFields: map[string][]string{"p.T": {"A", "B"}, "p.U": {"C"}}},
		},
		{
			name: "types as a block sequence",
			doc:  "types:\n  - a/b.C\n  - d/e.F\n",
			want: Config{Types: []string{"a/b.C", "d/e.F"}},
		},
		{
			name: "types as a flow sequence",
			doc:  "types: [\"a/b.C\", \"d/e.F\"]\n",
			want: Config{Types: []string{"a/b.C", "d/e.F"}},
		},
		{
			name: "anchor and alias are resolved",
			doc:  "defaults: &d\n  - A\nexclude_fields:\n  \"p.T\": *d\n",
			want: Config{ExcludeFields: map[string][]string{"p.T": {"A"}}},
		},
		{
			name: "folded scalar",
			doc:  "default_mode: >\n  lenient\n",
			want: Config{DefaultMode: "lenient\n"},
		},
		{
			name: "document start marker",
			doc:  "---\ndefault_mode: lenient\n",
			want: Config{DefaultMode: "lenient"},
		},
		{
			name: "comments and blank lines are ignored",
			doc:  "# leading comment\n\ndefault_mode: lenient # trailing\n\n# trailing comment\n",
			want: Config{DefaultMode: "lenient"},
		},
		{
			name: "hash inside a quoted scalar is not a comment",
			doc:  "default_mode: \"strict#notacomment\"\n",
			want: Config{DefaultMode: "strict#notacomment"},
		},
		{
			name: "check_generated false",
			doc:  "check_generated: false\n",
			want: Config{CheckGenerated: new(false)},
		},
		{
			name: "all keys together",
			doc: "types:\n  - a/b.C\ndefault_mode: lenient\ncheck_generated: false\n" +
				"exclude_fields:\n  \"a/b.C\":\n    - Legacy\n",
			want: Config{
				Types:          []string{"a/b.C"},
				DefaultMode:    "lenient",
				CheckGenerated: new(false),
				ExcludeFields:  map[string][]string{"a/b.C": {"Legacy"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loadConfigFromPath(writeConfig(t, tc.doc))
			if err != nil {
				t.Fatalf("loadConfigFromPath: %v", err)
			}

			// loadConfigFromPath applies defaults; fill them into want so each
			// case only has to state the keys it exercises.
			want := tc.want
			applyDefaults(&want)

			if !reflect.DeepEqual(got.Types, want.Types) {
				t.Errorf("Types = %#v, want %#v", got.Types, want.Types)
			}
			if got.DefaultMode != want.DefaultMode {
				t.Errorf("DefaultMode = %q, want %q", got.DefaultMode, want.DefaultMode)
			}
			if *got.CheckGenerated != *want.CheckGenerated {
				t.Errorf("CheckGenerated = %v, want %v", *got.CheckGenerated, *want.CheckGenerated)
			}
			if !reflect.DeepEqual(got.ExcludeFields, want.ExcludeFields) {
				t.Errorf("ExcludeFields = %#v, want %#v", got.ExcludeFields, want.ExcludeFields)
			}
		})
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	got, err := loadConfigFromPath(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("loadConfigFromPath: %v", err)
	}
	if got.DefaultMode != "strict" {
		t.Errorf("DefaultMode = %q, want %q", got.DefaultMode, "strict")
	}
	if got.CheckGenerated == nil || !*got.CheckGenerated {
		t.Errorf("CheckGenerated = %v, want true", got.CheckGenerated)
	}
}

// TestLoadConfigMalformed checks that a broken config surfaces an error
// rather than being silently discarded.
func TestLoadConfigMalformed(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{name: "check_generated is not a bool", doc: "check_generated: notabool\n"},
		{name: "types is a scalar", doc: "types: 42\n"},
		{name: "unclosed flow sequence", doc: "types: [\"a/b.C\"\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadConfigFromPath(writeConfig(t, tc.doc)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

// TestResolveConfigPathWalksUp verifies the config is found in an ancestor
// directory, not just the starting one.
func TestResolveConfigPathWalksUp(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, ".enumstruct.yml")
	if err := os.WriteFile(want, []byte("default_mode: lenient\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, found, err := resolveConfigPath(nested)
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if !found {
		t.Fatal("config not found walking up from nested dir")
	}
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}
