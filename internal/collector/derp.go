package collector

import "strings"

// derpRegionNames maps the public Tailscale DERP region codes to display
// names. Codes not listed here are shown as-is.
var derpRegionNames = map[string]string{
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

// DERPRegionName returns the display name of a public Tailscale DERP region
// code (e.g. "nyc" -> "New York City"). Unknown codes are returned
// unchanged.
func DERPRegionName(code string) string {
	if name, ok := derpRegionNames[strings.ToLower(strings.TrimSpace(code))]; ok {
		return name
	}
	return code
}
