package memberpalette

import "testing"

// clientPalette is the list the web client repeats as MEMBER_COLORS, which is
// what a new calendar's first colour is picked from. The two are pinned to
// each other here and in the client's own test: a palette that drifts hands
// out a colour the server's list does not contain, and the mismatch shows up
// as a swatch nobody can select again.
var clientPalette = []string{
	"#47B2F7", "#F35F8C", "#B38BDC", "#FDC02D", "#E73B3B",
	"#2ECC87", "#F5A623", "#8F8F8F", "#42A5F5", "#FF7043",
}

func TestColorsMatchTheClient(t *testing.T) {
	if len(Colors) != len(clientPalette) {
		t.Fatalf("palette has %d colours, client has %d", len(Colors), len(clientPalette))
	}
	for i, want := range clientPalette {
		if Colors[i] != want {
			t.Errorf("colour %d = %s, client has %s", i+1, Colors[i], want)
		}
	}
}

func TestPick(t *testing.T) {
	cases := []struct {
		name string
		used []string
		want string
	}{
		{"empty calendar takes the first", nil, "#47B2F7"},
		{"skips a taken colour", []string{"#47B2F7"}, "#F35F8C"},
		{"case does not hide a taken colour", []string{"#47b2f7", "#f35f8c"}, "#B38BDC"},
		{"custom colours count against nothing", []string{"#123456"}, "#47B2F7"},
		{"fills a gap before moving on", []string{"#F35F8C"}, "#47B2F7"},
		{"repeats only once every colour is taken", Colors, "#47B2F7"},
		{"repeats the least used", append(append([]string{}, Colors...), "#47B2F7"), "#F35F8C"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Pick(tc.used); got != tc.want {
				t.Errorf("Pick(%v) = %s, want %s", tc.used, got, tc.want)
			}
		})
	}
}
