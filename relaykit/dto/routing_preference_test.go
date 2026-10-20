package dto

import "testing"

func TestRoutingPreferenceNormalizeAndValidate(t *testing.T) {
	valid := RoutingPreference{Mode: "  Price "}
	valid.Normalize()
	if valid.Mode != "price" {
		t.Fatalf("mode=%q", valid.Mode)
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid preference rejected: %v", err)
	}
	if err := (&RoutingPreference{}).Validate(); err != nil {
		t.Fatalf("empty mode must inherit default: %v", err)
	}
	if err := (&RoutingPreference{Mode: "fastest"}).Validate(); err == nil {
		t.Fatal("unknown mode should be rejected")
	}
}
