package cli

import "testing"

func TestResolveExtractScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		scopeFlag   string
		originScope string
		includeAll  bool
		want        string
	}{
		{
			name:      "explicit scope flag wins over everything",
			scopeFlag: "codex",
			want:      "codex",
		},
		{
			name:        "explicit scope flag beats configured origin",
			scopeFlag:   "codex",
			originScope: "cursor",
			want:        "codex",
		},
		{
			name:        "explicit scope flag beats include-all",
			scopeFlag:   "codex",
			originScope: "cursor",
			includeAll:  true,
			want:        "codex",
		},
		{
			// Regression: before the fix, a configured origin_scope
			// collapsed the --include-all override back to the narrow
			// scope, silently producing incomplete exports.
			name:        "include-all overrides configured origin scope",
			originScope: "codex",
			includeAll:  true,
			want:        "all",
		},
		{
			name:       "include-all with no configured scope",
			includeAll: true,
			want:       "all",
		},
		{
			name:        "configured origin scope used when no flags",
			originScope: "codex",
			want:        "codex",
		},
		{
			name:        "whitespace-only scope flag is treated as unset",
			scopeFlag:   "   ",
			originScope: "codex",
			want:        "codex",
		},
		{
			name: "no inputs returns empty (caller should error)",
			want: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveExtractScope(tc.scopeFlag, tc.originScope, tc.includeAll)
			if got != tc.want {
				t.Fatalf("resolveExtractScope(%q, %q, %v) = %q, want %q",
					tc.scopeFlag, tc.originScope, tc.includeAll, got, tc.want)
			}
		})
	}
}
