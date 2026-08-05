package calendars

import "testing"

// palette is the list the web client repeats as MEMBER_COLORS, which is what
// a new calendar's first colour is picked from. The two are pinned to each
// other here and in the client's own test: a palette that drifts hands out a
// colour the calendar's list does not contain, and the mismatch shows up as a
// swatch nobody can select again.
var palette = []string{
	"#47B2F7", "#F35F8C", "#B38BDC", "#FDC02D", "#E73B3B",
	"#2ECC87", "#F5A623", "#8F8F8F", "#42A5F5", "#FF7043",
}

func TestLabelPaletteMatchesTheClient(t *testing.T) {
	labels := labelPalette()
	if len(labels) != len(palette) {
		t.Fatalf("palette has %d colours, client has %d", len(labels), len(palette))
	}
	for i, want := range palette {
		if labels[i].Color != want {
			t.Errorf("colour %d = %s, client has %s", i+1, labels[i].Color, want)
		}
		if labels[i].ID == "" || labels[i].NameKey == "" {
			t.Errorf("colour %d has no id or name key", i+1)
		}
	}
}
