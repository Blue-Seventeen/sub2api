package usagestats

import (
	"reflect"
	"strings"
	"testing"
)

func TestUsageIPFilterIsShared(t *testing.T) {
	if _, ok := reflect.TypeOf(UsageLogFilters{}).FieldByName("IPAddresses"); !ok {
		t.Fatal("shared IP filter is missing")
	}
}
func TestParseIPAddresses(t *testing.T) {
	ips, err := ParseIPAddresses(" 2001:0DB8::1 ,192.0.2.1,192.0.2.1 ")
	if err != nil || !reflect.DeepEqual(ips, []string{"192.0.2.1", "2001:0DB8::1"}) {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
	for _, raw := range []string{"host", "1.2.3.4:80", "fe80::1%eth0", "192.0.2.1,", strings.Repeat("192.0.2.1,", 100) + "192.0.2.1"} {
		if _, err := ParseIPAddresses(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	ips, err = ParseIPAddresses(" ")
	if err != nil || len(ips) != 0 {
		t.Fatal("empty must select all")
	}
}
func TestAppendIPWhere(t *testing.T) {
	conditions, args := AppendIPWhere([]string{"user_id = $1"}, []any{int64(42)}, []string{"192.0.2.1", "::1"}, "ul.ip_address")
	if conditions[1] != "ul.ip_address IN ($2, $3)" || len(args) != 3 {
		t.Fatalf("%v %v", conditions, args)
	}
}
