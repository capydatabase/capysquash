package views

import "testing"

func TestFormatFileReduction(t *testing.T) {
	for files, want := range map[int]string{0: "none", -2: "none", 1: "-1 file", 5: "-5 files"} {
		if got := formatFileReduction(files); got != want {
			t.Errorf("formatFileReduction(%d) = %q, want %q", files, got, want)
		}
	}
}
