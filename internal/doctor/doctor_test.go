package doctor

import (
	"reflect"
	"testing"
)

func TestUnsetVars(t *testing.T) {
	out := `time="x" level=warning msg="The \"PUSHER_APP_KEY\" variable is not set. Defaulting to a blank string."
time="x" level=warning msg="The \"PUSHER_APP_KEY\" variable is not set. Defaulting to a blank string."
WARN[0000] The "DB_PASS" variable is not set. Defaulting to a blank string.`
	if got := unsetVars(out); !reflect.DeepEqual(got, []string{"PUSHER_APP_KEY", "DB_PASS"}) {
		t.Fatalf("got %v", got)
	}
}

func TestParseComposeVersion(t *testing.T) {
	if ma, mi, ok := parseComposeVersion("v2.29.1-desktop.1"); !ok || ma != 2 || mi != 29 {
		t.Fatalf("got %d.%d %v", ma, mi, ok)
	}
}
