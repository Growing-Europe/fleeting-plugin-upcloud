package group

import (
	"context"
	"testing"

	"pgregory.net/rapid"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/ucloud"
)

// TestProp_IncreaseNeverExceedsCapacity is the core safety invariant: for any
// current group size, requested delta, and configured cap, Increase must never
// create beyond the remaining room, so the live group never exceeds
// max_instances (fail-closed on quota).
func TestProp_IncreaseNeverExceedsCapacity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		capacity := rapid.IntRange(1, 20).Draw(t, "capacity")
		current := rapid.IntRange(0, 25).Draw(t, "current")
		ask := rapid.IntRange(0, 30).Draw(t, "ask")

		f := &fakeCloud{listResult: make([]ucloud.Server, current)}
		c := cfg()
		c.MaxInstances = capacity
		g := New(c, f)

		n, _ := g.Increase(context.Background(), ask)

		room := capacity - current
		if room < 0 {
			room = 0 // already at/over cap (e.g. cap lowered externally) -> add nothing
		}
		// Core invariant: never create beyond the remaining room. When the group
		// already exceeds the cap this forces n == 0 (fail-closed, never worsened).
		if n < 0 || n > room {
			t.Fatalf("created %d outside [0,%d] (cap=%d current=%d ask=%d)", n, room, capacity, current, ask)
		}
		if n != len(f.created) {
			t.Fatalf("returned %d but made %d Create calls", n, len(f.created))
		}
	})
}
