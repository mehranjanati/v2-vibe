package engine

import "testing"

// C3: the legacy transcript pseudo-file check was previously duplicated
// inline (two string literals) in hub.go and room.go. It now lives in a
// single helper; this test pins both accepted spellings and rejects real
// project files.
func TestIsLegacyOutputFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"generated-output.txt", true},
		{"/generated-output.txt", true},
		{"public/generated-output.txt", false}, // a real file under public/ is NOT the legacy pseudo-file
		{"index.html", false},
		{"styles.css", false},
		{"js/main.js", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsLegacyOutputFile(c.path); got != c.want {
			t.Errorf("IsLegacyOutputFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
