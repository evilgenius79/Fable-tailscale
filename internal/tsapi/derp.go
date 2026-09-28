package tsapi

import "strings"

// DERPRegionNames maps the public Tailscale DERP region codes to the display
// names the control API uses in clientConnectivity.latency. Treat it as
// read-only.
var DERPRegionNames = map[string]string{
	"nyc": "New York City",
	"sfo": "San Francisco",
	"ord": "Chicago",
	"dfw": "Dallas",
	"sea": "Seattle",
	"lax": "Los Angeles",
	"mia": "Miami",
	"den": "Denver",
	"hnl": "Honolulu",
	"tor": "Toronto",
	"lhr": "London",
	"fra": "Frankfurt",
	"ams": "Amsterdam",
	"par": "Paris",
	"mad": "Madrid",
	"waw": "Warsaw",
	"hkg": "Hong Kong",
	"sin": "Singapore",
	"tok": "Tokyo",
	"syd": "Sydney",
	"sao": "São Paulo",
	"jnb": "Johannesburg",
	"blr": "Bangalore",
	"nue": "Nuremberg",
	"dxb": "Dubai",
	"nai": "Nairobi",
	"iad": "Ashburn",
}

// derpAliases are alternative spellings (lowercase) of region names.
var derpAliases = map[string]string{
	"sao paulo":     "sao",
	"new york":      "nyc",
	"hongkong":      "hkg",
	"washington dc": "iad",
}

// derpCodesByName is the lowercase name -> code inverse of DERPRegionNames.
var derpCodesByName = func() map[string]string {
	m := make(map[string]string, len(DERPRegionNames)+len(derpAliases))
	for code, name := range DERPRegionNames {
		m[strings.ToLower(name)] = code
	}
	for alias, code := range derpAliases {
		m[alias] = code
	}
	return m
}()

// DERPCode maps a DERP region display name as reported by the control API
// (e.g. "New York City") to its region code ("nyc"). The lookup is
// case-insensitive and a value that already is a known code is returned as
// is. Unknown names are lowercased with whitespace runs replaced by "-", so
// custom DERP regions still get a stable key. The empty string maps to "".
func DERPCode(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return ""
	}
	if code, ok := derpCodesByName[n]; ok {
		return code
	}
	if _, ok := DERPRegionNames[n]; ok {
		return n
	}
	return strings.Join(strings.Fields(n), "-")
}
