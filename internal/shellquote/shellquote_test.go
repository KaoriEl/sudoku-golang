package shellquote

import "testing"

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"":                  "''",
		"plain/path.yaml":   "plain/path.yaml",
		"with space":        "'with space'",
		"it's":              `'it'"'"'s'`,
		"$(rm -rf /)":       "'$(rm -rf /)'",
		"a;b":               "'a;b'",
		"/Users/x/My Proj/": "'/Users/x/My Proj/'",
	}
	for in, want := range cases {
		if got := POSIX(in); got != want {
			t.Errorf("POSIX(%q) = %q, want %q", in, got, want)
		}
	}
	if got := PowerShell(`C:\My Proj\it's.yaml`); got != `'C:\My Proj\it''s.yaml'` {
		t.Errorf("PowerShell = %q", got)
	}
	if got := PowerShell(`C:\proj\compose.yaml`); got != `C:\proj\compose.yaml` {
		t.Errorf("PowerShell safe path = %q", got)
	}
}
