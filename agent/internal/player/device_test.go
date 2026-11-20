package player

import "testing"

func TestPlugDeviceFallback(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"hw:3,0", "plughw:3,0", true},
		{"hw:0,1", "plughw:0,1", true},
		{"default", "", false},
		{"null", "", false},
		{"plughw:3,0", "", false},
	}
	for _, c := range cases {
		got, ok := plugDevice(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("plugDevice(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}
