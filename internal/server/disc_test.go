package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/completist/internal/chd"
)

// Discs: a CHD, a CUE sheet with its BIN tracks, and a game on two discs with one of them.
func TestDiscs(t *testing.T) {
	fixture := filepath.Join("..", "chd", "testdata", "disc-default.chd")
	img, err := chd.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	tracks, _ := img.Tracks()
	var track1 bytes.Buffer
	tracks[0].WriteTo(&track1)
	img.Close()

	other, disc1, disc2 := content(51, 2352*4), content(52, 2352*4), content(53, 2352*4)
	dat := "clrmamepro (\n\tname \"Sony - PlayStation\"\n)\n" +
		"game (\n\tname \"Image Game (Europe)\"\n" + romLine("Image Game (Europe) (Track 1).bin", track1.Bytes()) + ")\n" +
		"game (\n\tname \"Cue Game (USA)\"\n" + romLine("Cue Game (USA) (Track 1).bin", other) + ")\n" +
		"game (\n\tname \"Epic (USA) (Disc 1)\"\n" + romLine("Epic (USA) (Disc 1) (Track 1).bin", disc1) + ")\n" +
		"game (\n\tname \"Epic (USA) (Disc 2)\"\n" + romLine("Epic (USA) (Disc 2) (Track 1).bin", disc2) + ")\n"

	f := newFullFixture(t)
	if rec, _ := f.do("POST", "/api/dats/import", dat); rec.Code != 200 {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	chdData, _ := os.ReadFile(fixture)
	f.write("psx/Image Game (Europe).chd", chdData)
	f.write("psx/Cue Game (USA)/Cue Game (USA) (Track 1).bin", other)
	f.write("psx/Cue Game (USA)/Cue Game (USA) (Track 2).bin", content(54, 2352*3))
	f.write("psx/Cue Game (USA)/Cue Game (USA).cue", []byte("FILE \"Cue Game (USA) (Track 1).bin\" BINARY\n"))
	f.write("psx/Epic (USA) (Disc 1).bin", disc1)
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "full", "modes": map[string]string{}, "regions": []string{"USA"}})
	if err := f.scanner.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}

	var games struct {
		Items []struct {
			Name       string
			Owned      bool
			Discs      int
			DiscsOwned int
		}
	}
	f.list("/api/systems/psx/games", &games)
	got := map[string]string{}
	for _, g := range games.Items {
		got[g.Name] = strings.Join([]string{map[bool]string{true: "owned", false: "missing"}[g.Owned], string(rune('0' + g.DiscsOwned)), string(rune('0' + g.Discs))}, " ")
	}
	want := map[string]string{"Image Game (Europe)": "owned 0 0", "Cue Game (USA)": "owned 0 0", "Epic (USA) (Disc 1)": "missing 1 2"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("games: %v", got)
		}
	}
	if len(games.Items) != 3 {
		t.Fatalf("a game on two discs is one entry: %v", got)
	}
	// the audio track is not a file to review
	var sys struct {
		Files struct{ Unknown, Files int }
	}
	f.list("/api/systems/psx", &sys)
	if sys.Files.Unknown != 0 || sys.Files.Files != 3 {
		t.Fatalf("files: %+v", sys.Files)
	}

	// exports open the CUE sheet and the CHD
	files := unzip(t, f.raw("/api/export/retroarch?from="+f.library+"&to=/roms").Body.Bytes())
	pl := files["playlists/Sony - PlayStation.lpl"]
	if !strings.Contains(pl, `"path": "/roms/psx/Cue Game (USA)/Cue Game (USA).cue"`) || !strings.Contains(pl, `"path": "/roms/psx/Image Game (Europe).chd"`) {
		t.Fatalf("playlist: %s", pl)
	}
	gl := unzip(t, f.raw("/api/export/esde").Body.Bytes())["gamelists/psx/gamelist.xml"]
	if !strings.Contains(gl, "<path>./Cue Game (USA)/Cue Game (USA).cue</path>") || !strings.Contains(gl, "<path>./Image Game (Europe).chd</path>") {
		t.Fatalf("gamelist: %s", gl)
	}
}
