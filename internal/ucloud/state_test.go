package ucloud

import "testing"

func TestMapState(t *testing.T) {
	cases := map[string]State{
		"started":     StateRunning,
		"maintenance": StateCreating,
		"stopped":     StateDeleting,
		"error":       StateUnhealthy,
		"":            StateUnhealthy,
		"some-future": StateUnhealthy,
	}
	for in, want := range cases {
		if got := MapState(in); got != want {
			t.Errorf("MapState(%q) = %q, want %q", in, got, want)
		}
	}
}
