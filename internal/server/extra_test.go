package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func (f *fullFixture) raw(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr, req.Host = "127.0.0.1:1", "localhost"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *fullFixture) list(path string, into any) {
	f.t.Helper()
	rec := f.raw(path)
	if rec.Code != 200 {
		f.t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		f.t.Fatal(err)
	}
}

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		out[f.Name] = string(b)
	}
	return out
}

// A set of two games, one version of each: the wishlist, the statistics and the exports.
func TestWishlistStatsAndExports(t *testing.T) {
	var mutex sync.Mutex
	messages := []string{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mutex.Lock()
		messages = append(messages, body["message"].(string))
		mutex.Unlock()
	}))
	defer hook.Close()

	f := newFullFixture(t)
	a, b, bj := content(31, 64), content(32, 64), content(33, 64)
	dat := "clrmamepro (\n\tname \"Nintendo - Game Boy\"\n)\n" +
		"game (\n\tname \"Alpha (World)\"\n" + romLine("Alpha (World).gb", a) + ")\n" +
		"game (\n\tname \"Beta & Co (USA)\"\n" + romLine("Beta & Co (USA).gb", b) + ")\n" +
		"game (\n\tname \"Beta & Co (Japan)\"\n\tcloneof \"Beta & Co (USA)\"\n" + romLine("Beta & Co (Japan).gb", bj) + ")\n"
	f.do("POST", "/api/dats/import", dat)
	f.do("PUT", "/api/notifications", map[string]any{"webhookUrl": hook.URL, "language": "en", "newGames": true, "wishlist": true})
	f.write("gb/Alpha (World).gb", a)
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	f.scanner.Run(context.Background(), "test")

	// the wishlist takes only games of a DAT
	if rec, _ := f.do("PUT", "/api/wishlist/gb/Made%20up", nil); rec.Code != 404 {
		t.Fatalf("made up: %d", rec.Code)
	}
	if rec, _ := f.do("PUT", "/api/wishlist/gb/Beta%20%26%20Co%20(USA)", nil); rec.Code != 204 {
		t.Fatalf("wish: %d %s", rec.Code, rec.Body)
	}
	var wishes []map[string]any
	f.list("/api/wishlist", &wishes)
	if len(wishes) != 1 || wishes[0]["owned"] != false || wishes[0]["title"] != "Beta & Co" {
		t.Fatalf("wishlist: %v", wishes)
	}
	var games struct{ Items []map[string]any }
	f.list("/api/systems/gb/games?filter=wanted", &games)
	if len(games.Items) != 1 || games.Items[0]["wanted"] != true {
		t.Fatalf("wanted: %v", games.Items)
	}

	// another version of the wished game arrives: counting one version of each, it is got
	f.write("gb/Beta & Co (Japan).gb", bj)
	f.scanner.Run(context.Background(), "test")
	f.list("/api/wishlist", &wishes)
	if wishes[0]["owned"] != true {
		f.list("/api/systems/gb/games", &games)
		t.Fatalf("after the scan: %v\n%v", wishes, games.Items)
	}
	mutex.Lock()
	got := strings.Join(messages, "\n")
	mutex.Unlock()
	if !strings.Contains(got, "From your wishlist, now in your collection: Beta & Co (USA)") || !strings.Contains(got, "New in your collection: Beta & Co (Japan)") {
		t.Fatalf("notifications: %q", got)
	}

	// statistics
	var st struct {
		Summary struct{ Have, Total int }
		History []struct {
			Day  string
			Have int
		}
		Makers []struct {
			Name        string
			Have, Total int
		}
		Recent   []struct{ Name string }
		Wishlist struct{ Total, Owned int }
	}
	f.list("/api/stats", &st)
	if len(st.History) != 1 || st.History[0].Have != 2 {
		t.Fatalf("history: %+v", st.History)
	}
	if st.Summary.Have != 2 || st.Summary.Total != 2 || len(st.Makers) != 1 || st.Makers[0].Name != "Nintendo" || st.Wishlist.Owned != 1 || len(st.Recent) != 2 {
		t.Fatalf("stats: %+v", st)
	}

	// RetroArch: the paths of the device, the names of the DAT
	rec := f.raw("/api/export/retroarch?from=" + f.library + "&to=/storage/roms")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("retroarch: %d", rec.Code)
	}
	files := unzip(t, rec.Body.Bytes())
	pl := files["playlists/Nintendo - Game Boy.lpl"]
	if !strings.Contains(pl, `"path": "/storage/roms/gb/Alpha (World).gb"`) || !strings.Contains(pl, `"label": "Beta & Co (Japan)"`) || !strings.Contains(pl, `"db_name": "Nintendo - Game Boy.lpl"`) {
		t.Fatalf("playlist: %s", pl)
	}
	// ES-DE: relative to the folder of the system, the titles
	gl := unzip(t, f.raw("/api/export/esde").Body.Bytes())["gamelists/gb/gamelist.xml"]
	if !strings.Contains(gl, "<path>./Alpha (World).gb</path>") || !strings.Contains(gl, "<name>Beta &amp; Co</name>") {
		t.Fatalf("gamelist: %s", gl)
	}
	if rec := f.raw("/api/export/other"); rec.Code != 404 {
		t.Fatalf("unknown format: %d", rec.Code)
	}

	// removed
	if rec, _ := f.do("DELETE", "/api/wishlist/gb/Beta%20%26%20Co%20(USA)", nil); rec.Code != 204 {
		t.Fatalf("remove: %d", rec.Code)
	}
	f.list("/api/wishlist", &wishes)
	if len(wishes) != 0 {
		t.Fatalf("after removing: %v", wishes)
	}
}

// a box art like a scan: smooth colours and grain, which a PNG keeps badly
func photo(w, h int, alpha bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8(r.UintN(24))
			a := uint8(255)
			if alpha && x < 10 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(x/2) + n, uint8(y/2) + n, uint8((x+y)/4) + n, a})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestBoxArtKeptSmaller(t *testing.T) {
	scan, clear := photo(512, 364, false), photo(512, 364, true)
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Nintendo - Game Boy/Named_Boxarts/Alpha (World).png", "/Nintendo - Game Boy/Named_Snaps/Alpha (World).png":
			w.Write(scan)
		case "/Nintendo - Game Boy/Named_Boxarts/Beta (World).png":
			w.Write(clear)
		default:
			http.NotFound(w, r)
		}
	}))
	defer images.Close()
	f := newFullFixture(t)
	f.covers.BaseURL = images.URL
	f.do("POST", "/api/dats/import", gbDat(map[string][]byte{"Alpha (World)": content(41, 64), "Beta (World)": content(42, 64)}))

	// kept as it comes until chosen
	if rec := f.raw("/api/covers/gb/boxart/Alpha%20(World).png"); rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() != len(scan) {
		t.Fatalf("original: %s %d", rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	if rec, _ := f.do("PUT", "/api/covers", map[string]string{"mode": "tiny"}); rec.Code != 400 {
		t.Fatalf("bad mode: %d", rec.Code)
	}
	if rec, _ := f.do("PUT", "/api/covers", map[string]string{"mode": "high"}); rec.Code != 200 {
		t.Fatalf("mode: %d", rec.Code)
	}
	// the one kept before is converted in the background
	deadline := time.Now().Add(10 * time.Second)
	for f.covers.Usage().Converting && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	rec := f.raw("/api/covers/gb/boxart/Alpha%20(World).png")
	if rec.Header().Get("Content-Type") != "image/jpeg" || rec.Body.Len() >= len(scan)/2 {
		t.Fatalf("converted: %s %d of %d", rec.Header().Get("Content-Type"), rec.Body.Len(), len(scan))
	}
	if _, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	// transparent parts and screenshots stay PNG
	if rec := f.raw("/api/covers/gb/boxart/Beta%20(World).png"); rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() != len(clear) {
		t.Fatalf("transparent: %s", rec.Header().Get("Content-Type"))
	}
	if rec := f.raw("/api/covers/gb/snap/Alpha%20(World).png"); rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() != len(scan) {
		t.Fatalf("snap: %s", rec.Header().Get("Content-Type"))
	}
	var usage map[string]any
	f.list("/api/covers", &usage)
	if usage["mode"] != "high" || usage["images"] != 3.0 {
		t.Fatalf("usage: %v", usage)
	}
}
