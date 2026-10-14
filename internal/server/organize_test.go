package server

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// waitOrganize waits for the change to the files to end.
func (f *fullFixture) waitOrganize() {
	f.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if organizing.TryLock() {
			organizing.Unlock()
			// and for the scan it may have started
			f.scanner.Exclusive(func() {})
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("the change did not end")
}

func (f *fullFixture) names(dir string) []string {
	entries, _ := os.ReadDir(filepath.Join(f.library, dir))
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

type planJSON struct {
	Actions []struct {
		ID    string
		Game  string
		Moves []struct{ From, To string }
		Sheet *struct{ From, To string }
	}
	Skipped []struct{ Path, Reason string }
	Done    int
}

func TestRenameAndUndo(t *testing.T) {
	f := newFullFixture(t)
	a, b, taken := content(71, 64), content(72, 64), content(73, 64)
	track1, track2 := content(74, 2352*4), content(75, 2352*2)
	f.do("POST", "/api/dats/import", gbDat(map[string][]byte{"Alpha (World)": a, "Beta (Japan)": b, "Gamma (USA)": taken}))
	f.do("POST", "/api/dats/import", "clrmamepro (\n\tname \"Sony - PlayStation\"\n)\n"+
		"game (\n\tname \"Disc Game (Europe)\"\n"+romLine("Disc Game (Europe) (Track 1).bin", track1)+")\n")
	f.write("gb/alpha.gb", a)
	f.write("gb/beta (j).gb", b)
	f.write("gb/gamma.gb", taken)
	f.write("gb/Gamma (USA).gb", []byte("not the game"))
	f.write("psx/dg/dg1.bin", track1)
	f.write("psx/dg/dg2.bin", track2)
	f.write("psx/dg/dg.cue", []byte("FILE \"dg1.bin\" BINARY\r\n  TRACK 01 MODE2/2352\r\n    INDEX 01 00:00:00\r\nFILE \"dg2.bin\" BINARY\r\n  TRACK 02 AUDIO\r\n    INDEX 01 00:00:00\r\n"))
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.scanner.Run(context.Background(), "test")

	var plan planJSON
	f.list("/api/organize?system=gb", &plan)
	if len(plan.Actions) != 2 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "Another file has this name already." {
		t.Fatalf("plan: %+v", plan)
	}
	ids := []string{plan.Actions[0].ID, plan.Actions[1].ID}
	if rec, _ := f.do("POST", "/api/organize", map[string]any{"system": "gb", "kind": "rename", "ids": ids}); rec.Code != 202 {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body)
	}
	f.waitOrganize()
	if got := strings.Join(f.names("gb"), "|"); got != "Alpha (World).gb|Beta (Japan).gb|Gamma (USA).gb|gamma.gb" {
		t.Fatalf("after renaming: %s", got)
	}
	// the store knows the new names, nothing is read again
	var sys struct{ Have int }
	f.list("/api/systems/gb", &sys)
	if sys.Have != 3 {
		t.Fatalf("have: %d", sys.Have)
	}

	// a disc: its tracks and its CUE sheet
	f.list("/api/organize?system=psx", &plan)
	if len(plan.Actions) != 1 || plan.Actions[0].Sheet == nil {
		t.Fatalf("disc plan: %+v", plan)
	}
	f.do("POST", "/api/organize", map[string]any{"system": "psx", "ids": []string{plan.Actions[0].ID}})
	f.waitOrganize()
	if got := strings.Join(f.names("psx/dg"), "|"); got != "Disc Game (Europe) (Track 1).bin|Disc Game (Europe) (Track 2).bin|Disc Game (Europe).cue" {
		t.Fatalf("disc: %s", got)
	}
	cue, _ := os.ReadFile(filepath.Join(f.library, "psx/dg/Disc Game (Europe).cue"))
	if !strings.Contains(string(cue), "FILE \"Disc Game (Europe) (Track 2).bin\" BINARY\r\n") {
		t.Fatalf("cue: %q", cue)
	}
	f.list("/api/organize?system=psx", &plan)
	if plan.Done != 1 || len(plan.Actions) != 0 {
		t.Fatalf("again: %+v", plan)
	}

	// undone, newest first
	var history []struct {
		ID     int64
		Kind   string
		Games  int
		Undone bool
	}
	f.list("/api/organize/history", &history)
	if len(history) != 2 || history[0].Games != 1 || history[1].Games != 2 {
		t.Fatalf("history: %+v", history)
	}
	for _, h := range history {
		if rec, _ := f.do("POST", "/api/organize/history/"+strconv.FormatInt(h.ID, 10)+"/undo", nil); rec.Code != 202 {
			t.Fatalf("undo: %d %s", rec.Code, rec.Body)
		}
		f.waitOrganize()
	}
	if got := strings.Join(f.names("gb"), "|"); got != "Gamma (USA).gb|alpha.gb|beta (j).gb|gamma.gb" {
		t.Fatalf("undone: %s", got)
	}
	if got := strings.Join(f.names("psx/dg"), "|"); got != "dg.cue|dg1.bin|dg2.bin" {
		t.Fatalf("disc undone: %s", got)
	}
	cue, _ = os.ReadFile(filepath.Join(f.library, "psx/dg/dg.cue"))
	if !strings.Contains(string(cue), "FILE \"dg2.bin\" BINARY") {
		t.Fatalf("cue undone: %q", cue)
	}
	if rec, _ := f.do("POST", "/api/organize/history/"+strconv.FormatInt(history[0].ID, 10)+"/undo", nil); rec.Code != 409 {
		t.Fatalf("undone twice: %d", rec.Code)
	}
}

func TestPackAndUndo(t *testing.T) {
	f := newFullFixture(t)
	a := []byte(strings.Repeat("compressible game data ", 400))
	f.do("POST", "/api/dats/import", gbDat(map[string][]byte{"Alpha (World)": a}))
	f.write("gb/alpha.gb", a)
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.scanner.Run(context.Background(), "test")

	var plan planJSON
	f.list("/api/organize?system=gb&kind=zip", &plan)
	if len(plan.Actions) != 1 || !strings.HasSuffix(plan.Actions[0].Moves[0].To, "Alpha (World).zip") {
		t.Fatalf("plan: %+v", plan)
	}
	f.do("POST", "/api/organize", map[string]any{"system": "gb", "kind": "zip", "ids": []string{plan.Actions[0].ID}, "removeOriginals": true})
	f.waitOrganize()
	if got := strings.Join(f.names("gb"), "|"); got != "Alpha (World).zip" {
		t.Fatalf("packed: %s", got)
	}
	z, err := zip.OpenReader(filepath.Join(f.library, "gb", "Alpha (World).zip"))
	if err != nil || len(z.File) != 1 || z.File[0].Name != "Alpha (World).gb" {
		t.Fatalf("zip: %v", err)
	}
	z.Close()
	var sys struct {
		Have  int
		Files struct{ Files int }
	}
	f.list("/api/systems/gb", &sys)
	if sys.Have != 1 || sys.Files.Files != 1 {
		t.Fatalf("after packing: %+v", sys)
	}
	var history []struct {
		ID    int64
		Saved int64
	}
	f.list("/api/organize/history", &history)
	if len(history) != 1 || history[0].Saved <= 0 {
		t.Fatalf("history: %+v", history)
	}
	f.do("POST", "/api/organize/history/"+strconv.FormatInt(history[0].ID, 10)+"/undo", nil)
	f.waitOrganize()
	if got := strings.Join(f.names("gb"), "|"); got != "alpha.gb" {
		t.Fatalf("unpacked: %s", got)
	}
	back, _ := os.ReadFile(filepath.Join(f.library, "gb", "alpha.gb"))
	if string(back) != string(a) {
		t.Fatal("the original is not the same")
	}
	// read-only libraries are left alone
	f.do("PUT", "/api/libraries/1", map[string]any{"name": "lib", "readOnly": true})
	f.list("/api/organize?system=gb&kind=zip", &plan)
	if len(plan.Actions) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("read only: %+v", plan)
	}
}
