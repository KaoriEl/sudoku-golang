package admin

import "testing"

func TestExtractVars(t *testing.T) {
	raw := `{"image":"pg:${PG_TAG:-16}","environment":{"A":"${DB_PASS:?need it}","B":"$HOME","C":"$${LITERAL}","D":"${DB_PASS}"}}`
	vars := extractVars(raw, map[string]bool{"DB_PASS": true})
	got := map[string]ConfigVar{}
	for _, v := range vars {
		got[v.Name] = v
	}
	if _, ok := got["LITERAL"]; ok {
		t.Fatal("$$ escape treated as a variable")
	}
	if v := got["DB_PASS"]; v.Set || !v.Required || v.Uses != 2 {
		t.Fatalf("DB_PASS: %+v", v)
	}
	if v := got["PG_TAG"]; !v.Set || v.Default != "16" {
		t.Fatalf("PG_TAG: %+v", v)
	}
	if vars[0].Name != "DB_PASS" {
		t.Fatalf("unset vars must come first: %+v", vars)
	}
}
