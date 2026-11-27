package domains

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyAndReadBlock(t *testing.T) {
	orig := "127.0.0.1 localhost\n# my comment\n"
	withBlock := Apply(orig, []string{"b.test", "a.test"})
	if !strings.HasPrefix(withBlock, orig[:len(orig)-1]) {
		t.Fatalf("user content changed:\n%s", withBlock)
	}
	if got := ReadBlock(withBlock); !reflect.DeepEqual(got, []string{"a.test", "a.test", "b.test", "b.test"}) {
		t.Fatalf("block: %v", got)
	}
	again := Apply(withBlock, []string{"c.test"})
	if strings.Contains(again, "a.test") || !strings.Contains(again, "127.0.0.1 c.test") || strings.Count(again, blockBegin) != 1 {
		t.Fatalf("replace:\n%s", again)
	}
	if removed := Apply(again, nil); strings.Contains(removed, blockBegin) || removed != "127.0.0.1 localhost\n# my comment\n" {
		t.Fatalf("remove:\n%q", removed)
	}
}

func TestApplyKeepsCRLF(t *testing.T) {
	out := Apply("127.0.0.1 localhost\r\n", []string{"x.test"})
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Fatalf("mixed line endings: %q", out)
	}
}

func TestValidHost(t *testing.T) {
	for _, h := range []string{"", "a b", "*.x", "{{ .Name }}", "1.2.3.4", "x#y"} {
		if validHost(h) {
			t.Errorf("validHost(%q) = true", h)
		}
	}
	if !validHost("api.cdek.local") {
		t.Fatal("normal host rejected")
	}
}
