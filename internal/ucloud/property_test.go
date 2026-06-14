package ucloud

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// TestProp_LabelRoundTrip checks that converting a label map to the SDK slice
// and back is lossless, and that the slice is always emitted in deterministic
// (key-sorted) order — the latter matters so recorded requests and cassettes
// are byte-stable across runs.
func TestProp_LabelRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Map keys are unique by construction; draw a map of arbitrary strings.
		in := rapid.MapOf(
			rapid.String(),
			rapid.String(),
		).Draw(t, "labels")

		ls := toLabelSlice(in)

		if len(in) == 0 {
			if ls != nil {
				t.Fatalf("empty map must yield nil slice, got %v", ls)
			}
			return
		}

		// Round-trip is lossless.
		out := labelsToMap(*ls)
		if len(out) != len(in) {
			t.Fatalf("round-trip changed size: in=%d out=%d", len(in), len(out))
		}
		for k, v := range in {
			if out[k] != v {
				t.Fatalf("round-trip lost key %q: want %q got %q", k, v, out[k])
			}
		}

		// Deterministic key-sorted order.
		keys := make([]string, len(*ls))
		for i, l := range *ls {
			keys[i] = l.Key
		}
		if !sort.StringsAreSorted(keys) {
			t.Fatalf("label slice not key-sorted: %v", keys)
		}
	})
}
