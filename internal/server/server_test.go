package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeLFuS77/completist/internal/auth"
	"github.com/DeLFuS77/completist/internal/collection"
	"github.com/DeLFuS77/completist/internal/scanner"
	"github.com/DeLFuS77/completist/internal/store"
	"github.com/DeLFuS77/completist/internal/tasks"
)

func content(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 7))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.UintN(256))
	}
	return b
}

func romLine(name string, b []byte) string {
	s := sha1.Sum(b)
	m := md5.Sum(b)
	return fmt.Sprintf("\trom ( name \"%s\" size %d crc %08X md5 %s sha1 %s )\n", name, len(b), crc32.ChecksumIEEE(b),
		strings.ToUpper(hex.EncodeToString(m[:])), strings.ToUpper(hex.EncodeToString(s[:])))
}

type fixture struct {
	t       *testing.T
	handler http.Handler
	scanner *scanner.Scanner
	library string
	cookie  *http.Cookie
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := auth.New(data)
	if err != nil {
		t.Fatal(err)
	}
	log := tasks.Load("")
	progress := collection.NewCache(st)
	sc := scanner.New(st, log, 2)
	sc.OnChange = progress.Invalidate
	srv := &Server{Store: st, Tasks: log, Scanner: sc, Progress: progress, Auth: a, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return &fixture{t: t, handler: srv.Handler(), scanner: sc, library: t.TempDir()}
}

func (f *fixture) do(method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	f.t.Helper()
	var reader io.Reader
	if s, ok := body.(string); ok {
		reader = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Host = "192.168.1.10:3000"
	if f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	out := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (f *fixture) write(rel string, b []byte) {
	f.t.Helper()
	path := filepath.Join(f.library, rel)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// The whole path: a DAT, a library with good, headered, zipped, damaged, unknown and
// duplicate files, a scan, and the counts in the three modes.
func TestCollectionEndToEnd(t *testing.T) {
	f := newFixture(t)

	a := map[string][]byte{
		"Game A (Europe)":     content(1, 4096),
		"Game A (USA)":        content(2, 4096),
		"Game B (USA)":        content(3, 4096),
		"Game C (Japan)":      content(4, 4096),
		"Game D (Europe)":     content(5, 4096),
		"Game E (Europe)":     content(6, 4096),
		"Homebrew (World)":    content(7, 4096),
		"Game F (USA) (Beta)": content(8, 4096),
	}
	var dat strings.Builder
	dat.WriteString("clrmamepro (\n\tname \"Nintendo - Super Nintendo Entertainment System\"\n\tversion \"1\"\n)\n")
	for _, name := range []string{"Game A (Europe)", "Game A (USA)", "Game B (USA)", "Game C (Japan)", "Game D (Europe)", "Game E (Europe)", "Game F (USA) (Beta)"} {
		dat.WriteString("game (\n\tname \"" + name + "\"\n" + romLine(name+".sfc", a[name]) + ")\n")
	}
	dat.WriteString("game (\n\tname \"Homebrew (World)\"\n" + romLine("Homebrew (World) (Aftermarket) (Unl).sfc", a["Homebrew (World)"]) + ")\n")

	rec, out := f.do("POST", "/api/dats/import", dat.String())
	if rec.Code != 200 || out["games"].(float64) != 8 {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}

	f.write("snes/Game A (USA).sfc", a["Game A (USA)"])
	// a copier header in front: the DAT lists the data without it
	f.write("snes/Game B (USA).smc", append(make([]byte, 512), a["Game B (USA)"]...))
	// the name of a game, other content: a damaged or modified dump
	f.write("snes/Game C (Japan).sfc", content(99, 4096))
	f.write("snes/Homebrew (World) (Aftermarket) (Unl).sfc", a["Homebrew (World)"])
	f.write("snes/random.sfc", content(100, 4096))
	// a zip, and the same game again outside it: a duplicate
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("Game D (Europe).sfc")
	w.Write(a["Game D (Europe)"])
	zw.Create("readme.txt")
	zw.Close()
	f.write("snes/Game D (Europe).zip", zbuf.Bytes())
	f.write("other/Game D (Europe).sfc", a["Game D (Europe)"])

	rec, _ = f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	if rec.Code != 201 {
		t.Fatalf("add library: %d %s", rec.Code, rec.Body)
	}
	// adding a library starts a scan; wait for it, then scan again: nothing is read twice
	if err := f.scanner.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}

	check := func(mode string, total, have float64) {
		t.Helper()
		f.do("PUT", "/api/preferences", map[string]any{"defaultMode": mode, "modes": map[string]string{}, "regions": []string{"Europe", "USA"}})
		_, sys := f.do("GET", "/api/systems/snes", nil)
		if sys["total"] != total || sys["have"] != have || sys["missing"] != total-have {
			t.Fatalf("%s: total %v have %v missing %v", mode, sys["total"], sys["have"], sys["missing"])
		}
	}
	check("full", 8, 4)
	check("official", 6, 3)
	check("1g1r", 5, 3)

	_, sys := f.do("GET", "/api/systems/snes", nil)
	files := sys["files"].(map[string]any)
	if files["ok"] != 5.0 || files["bad"] != 1.0 || files["unknown"] != 1.0 || files["duplicates"] != 1.0 {
		t.Fatalf("files: %v", files)
	}

	// the missing games of the system, in 1G1R: C (only a damaged copy) and E
	_, list := f.do("GET", "/api/systems/snes/games?filter=missing", nil)
	names := []string{}
	for _, it := range list["items"].([]any) {
		names = append(names, it.(map[string]any)["name"].(string))
	}
	if strings.Join(names, "|") != "Game C (Japan)|Game E (Europe)" {
		t.Fatalf("missing: %v", names)
	}
	// Game A is shown as the version you have, not the preferred region
	_, list = f.do("GET", "/api/systems/snes/games?filter=have&q=game+a", nil)
	if item := list["items"].([]any)[0].(map[string]any); item["name"] != "Game A (USA)" || item["versions"] != 2.0 {
		t.Fatalf("game a: %v", item)
	}

	_, overview := f.do("GET", "/api/overview", nil)
	summary := overview["summary"].(map[string]any)
	if summary["total"] != 5.0 || summary["have"] != 3.0 {
		t.Fatalf("summary: %v", summary)
	}

	// a file removed from the disk is forgotten by the next scan
	os.Remove(filepath.Join(f.library, "other/Game D (Europe).sfc"))
	f.scanner.Run(context.Background(), "test")
	_, sys = f.do("GET", "/api/systems/snes", nil)
	if files := sys["files"].(map[string]any); files["duplicates"] != 0.0 || files["ok"] != 4.0 {
		t.Fatalf("after removal: %v", files)
	}
}

func TestLoginFlow(t *testing.T) {
	f := newFixture(t)
	// without users the local network has full access
	if rec, _ := f.do("GET", "/api/overview", nil); rec.Code != 200 {
		t.Fatalf("open: %d", rec.Code)
	}
	if rec, _ := f.do("POST", "/api/users", map[string]any{"name": "admin", "password": "password"}); rec.Code != 400 {
		t.Fatalf("a common password must be refused: %d", rec.Code)
	}
	rec, _ := f.do("POST", "/api/users", map[string]any{"name": "admin", "password": "a long secret phrase"})
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := f.do("GET", "/api/overview", nil); rec.Code != 401 {
		t.Fatalf("a login must now be required: %d", rec.Code)
	}
	if rec, _ := f.do("POST", "/api/auth/login", map[string]any{"name": "admin", "password": "wrong password!"}); rec.Code != 401 {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	rec, status := f.do("POST", "/api/auth/login", map[string]any{"name": "admin", "password": "a long secret phrase"})
	if rec.Code != 200 || status["isAdmin"] != true {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SESSION_COOKIE {
			f.cookie = c
		}
	}
	if rec, _ := f.do("GET", "/api/overview", nil); rec.Code != 200 {
		t.Fatalf("with session: %d", rec.Code)
	}
	// a read-only user can look but not change
	if rec, _ := f.do("POST", "/api/users", map[string]any{"name": "viewer", "password": "another long phrase", "role": "viewer"}); rec.Code != 201 {
		t.Fatalf("create viewer: %d %s", rec.Code, rec.Body)
	}
	rec, _ = f.do("POST", "/api/auth/login", map[string]any{"name": "viewer", "password": "another long phrase"})
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SESSION_COOKIE {
			f.cookie = c
		}
	}
	if rec, _ := f.do("GET", "/api/overview", nil); rec.Code != 200 {
		t.Fatalf("viewer read: %d", rec.Code)
	}
	if rec, _ := f.do("POST", "/api/scan", nil); rec.Code != 403 {
		t.Fatalf("viewer scan: %d", rec.Code)
	}
	if rec, _ := f.do("GET", "/api/users", nil); rec.Code != 403 {
		t.Fatalf("viewer users: %d", rec.Code)
	}
}

func TestCrossSiteRequestsAreRefused(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest("POST", "/api/scan", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Host = "localhost:3000"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("cross-site: %d", rec.Code)
	}
}

func TestRemoteWithoutLoginIsRefused(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest("GET", "/api/overview", nil)
	req.RemoteAddr = "203.0.113.9:50000"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("remote: %d", rec.Code)
	}
}

// Without users, a web site that points its own name at this address (DNS rebinding) must not
// act as the administrator from a browser on the local network.
func TestForeignHostWithoutLoginIsRefused(t *testing.T) {
	f := newFixture(t)
	for host, want := range map[string]int{
		"attacker.example.com":     403,
		"rebind.attacker.net:3000": 403,
		"tower:3000":               200,
		"nas.local:3000":           200,
		"192.168.1.10:3000":        200,
		"[::1]:3000":               200,
		"localhost":                200,
	} {
		req := httptest.NewRequest("GET", "/api/overview", nil)
		req.RemoteAddr = "192.168.1.20:50000"
		req.Host = host
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", host, rec.Code, want)
		}
	}
}

func TestNestedLibrariesAreRefused(t *testing.T) {
	f := newFixture(t)
	os.MkdirAll(filepath.Join(f.library, "snes"), 0o755)
	if rec, _ := f.do("POST", "/api/libraries", map[string]any{"path": f.library}); rec.Code != 201 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := f.do("POST", "/api/libraries", map[string]any{"path": filepath.Join(f.library, "snes")}); rec.Code != 409 {
		t.Fatalf("inside: %d", rec.Code)
	}
	if rec, _ := f.do("POST", "/api/libraries", map[string]any{"path": filepath.Dir(f.library)}); rec.Code != 409 {
		t.Fatalf("around: %d", rec.Code)
	}
}

func TestBadOffsetIsHarmless(t *testing.T) {
	f := newFixture(t)
	f.do("POST", "/api/dats/import", "clrmamepro ( name \"Nintendo - Game Boy\" )\ngame ( name \"A (World)\" rom ( name \"A (World).gb\" size 4 crc 01020304 sha1 0102030405060708090a0b0c0d0e0f1011121314 ) )\n")
	if rec, _ := f.do("GET", "/api/systems/gb/games?offset=-5&limit=-1", nil); rec.Code != 200 {
		t.Fatalf("offset: %d %s", rec.Code, rec.Body)
	}
}

// A zip with several games is opened once and every game in it is found.
func TestZipWithSeveralGames(t *testing.T) {
	f := newFixture(t)
	one, two := content(11, 2048), content(12, 2048)
	var dat strings.Builder
	dat.WriteString("clrmamepro (\n\tname \"Nintendo - Game Boy\"\n)\n")
	dat.WriteString("game (\n\tname \"One (World)\"\n" + romLine("One (World).gb", one) + ")\n")
	dat.WriteString("game (\n\tname \"Two (World)\"\n" + romLine("Two (World).gb", two) + ")\n")
	if rec, _ := f.do("POST", "/api/dats/import", dat.String()); rec.Code != 200 {
		t.Fatalf("import: %d %s", rec.Code, rec.Body)
	}
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("One (World).gb")
	w.Write(one)
	w, _ = zw.Create("sub/renamed.gb")
	w.Write(two)
	zw.Close()
	f.write("gb/pack.zip", zbuf.Bytes())
	f.do("POST", "/api/libraries", map[string]any{"path": f.library})
	if err := f.scanner.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	f.do("PUT", "/api/preferences", map[string]any{"defaultMode": "full", "modes": map[string]string{}, "regions": []string{"World"}})
	_, sys := f.do("GET", "/api/systems/gb", nil)
	if sys["have"] != 2.0 || sys["total"] != 2.0 {
		t.Fatalf("zip: have %v of %v (%v)", sys["have"], sys["total"], sys["files"])
	}
}
