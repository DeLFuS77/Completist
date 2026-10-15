package server

import "testing"

func TestWindowsPath(t *testing.T) {
	for p, want := range map[string]bool{`C:\Users\6Q\Desktop\Roms`: true, `d:/roms`: true, `\\nas\roms`: true, `/roms`: false, `roms`: false, `1:`: false} {
		if windowsPath(p) != want {
			t.Errorf("%q: %v", p, !want)
		}
	}
}
