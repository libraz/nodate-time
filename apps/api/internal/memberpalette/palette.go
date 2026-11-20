// Package memberpalette holds the colours a member's layer on a calendar is
// drawn from, and the rule that hands one to a member who did not choose.
//
// The web client repeats the list as MEMBER_COLORS; both sides pin it in a
// test, because a palette that drifts hands out a colour the calendar's own
// list does not contain.
package memberpalette

import "strings"

// Colors is the palette in the order it is offered and handed out.
var Colors = []string{
	"#47B2F7", "#F35F8C", "#B38BDC", "#FDC02D", "#E73B3B",
	"#2ECC87", "#F5A623", "#8F8F8F", "#42A5F5", "#FF7043",
}

// Pick returns the palette colour the fewest of used already carry, earliest
// in the palette on a tie, so a calendar fills every colour before any repeats.
// Colours outside the palette count against nothing.
func Pick(used []string) string {
	counts := make(map[string]int, len(Colors))
	for _, c := range used {
		counts[strings.ToUpper(c)]++
	}
	best := Colors[0]
	for _, c := range Colors[1:] {
		if counts[c] < counts[best] {
			best = c
		}
	}
	return best
}
