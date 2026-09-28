package tsapi

import "testing"

func TestDERPCode(t *testing.T) {
	cases := []struct{ name, want string }{
		{"New York City", "nyc"},
		{"new york city", "nyc"},
		{"NEW YORK CITY", "nyc"},
		{"  Frankfurt  ", "fra"},
		{"São Paulo", "sao"},
		{"Sao Paulo", "sao"},
		{"Hong Kong", "hkg"},
		{"Ashburn", "iad"},
		{"nyc", "nyc"},
		{"FRA", "fra"},
		{"Atlantis City", "atlantis-city"},
		{"My   Custom\tRegion", "my-custom-region"},
		{"custom", "custom"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DERPCode(tc.name); got != tc.want {
				t.Errorf("DERPCode(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestDERPRegionNamesRoundTrip(t *testing.T) {
	want := []string{"nyc", "sfo", "ord", "dfw", "sea", "lax", "mia", "den", "hnl", "tor", "lhr", "fra", "ams", "par", "mad", "waw", "hkg", "sin", "tok", "syd", "sao", "jnb", "blr", "nue", "dxb", "nai", "iad"}
	if len(DERPRegionNames) != len(want) {
		t.Errorf("DERPRegionNames has %d entries, want %d", len(DERPRegionNames), len(want))
	}
	for _, code := range want {
		name, ok := DERPRegionNames[code]
		if !ok || name == "" {
			t.Errorf("missing region %q", code)
			continue
		}
		if got := DERPCode(name); got != code {
			t.Errorf("DERPCode(%q) = %q, want %q", name, got, code)
		}
	}
}
