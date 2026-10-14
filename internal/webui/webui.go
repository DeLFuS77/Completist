// Package webui embeds the built interface (ui/ builds into dist/).
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
)

//go:embed all:dist
var files embed.FS

// FS is the interface, rooted at its index.html.
func FS() http.FileSystem {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

var inlineScript = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)

// ScriptHashes returns the CSP hashes of the inline scripts of index.html: the interface
// starts with one, and the content security policy allows only those.
func ScriptHashes() []string {
	data, err := files.ReadFile("dist/index.html")
	if err != nil {
		return nil
	}
	out := []string{}
	for _, m := range inlineScript.FindAllSubmatch(data, -1) {
		if len(m[1]) == 0 {
			continue
		}
		sum := sha256.Sum256(m[1])
		out = append(out, "sha256-"+base64.StdEncoding.EncodeToString(sum[:]))
	}
	return out
}
