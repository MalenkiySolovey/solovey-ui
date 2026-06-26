package catalog

import (
	"reflect"
	"testing"
)

func TestSubscriptionDefaults(t *testing.T) {
	defaults := SubscriptionDefaults()
	if defaults[SubPortKey] != "2096" {
		t.Fatalf("sub port default = %q", defaults[SubPortKey])
	}
	if defaults[SubPathKey] != "/sub/" {
		t.Fatalf("sub path default = %q", defaults[SubPathKey])
	}
	if defaults[SubJsonPathKey] != "/json/" {
		t.Fatalf("sub json path default = %q", defaults[SubJsonPathKey])
	}
	if defaults[SubClashPathKey] != "/clash/" {
		t.Fatalf("sub clash path default = %q", defaults[SubClashPathKey])
	}
	if defaults[SubXrayPathKey] != "/xray/" {
		t.Fatalf("sub xray path default = %q", defaults[SubXrayPathKey])
	}
	if defaults[SubRemoteGroupAdaptationKey] != "urltest" {
		t.Fatalf("remote group adaptation default = %q", defaults[SubRemoteGroupAdaptationKey])
	}
}

func TestSubscriptionKeyGroups(t *testing.T) {
	if got := SubscriptionPathKeys(); !reflect.DeepEqual(got, []string{SubPathKey, SubJsonPathKey, SubClashPathKey, SubXrayPathKey}) {
		t.Fatalf("path keys = %#v", got)
	}
	if _, ok := SubscriptionBooleanKeys()[SubJsonMuxKey]; !ok {
		t.Fatal("sub json mux should be a boolean key")
	}
	if _, ok := SubscriptionURLKeys()[SubSupportURLKey]; !ok {
		t.Fatal("support URL should be a URL key")
	}
}
