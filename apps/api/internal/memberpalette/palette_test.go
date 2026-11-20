package memberpalette

import "testing"

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
