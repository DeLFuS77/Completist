// Package platform defines what a plugin for a kind of system does. The core knows files,
// games and sets; a plugin knows how the files of its systems are read and hashed.
package platform

import (
	"fmt"
	"io"

	"github.com/DeLFuS77/completist/internal/systems"
)

// Hashes are the checksums of some content, hex and lower case.
type Hashes struct {
	Size int64  `json:"size"`
	CRC  string `json:"crc"`
	MD5  string `json:"md5"`
	SHA1 string `json:"sha1"`
	// how the content was read: "raw", "headerless", "byteswapped"...
	Form string `json:"form"`
}

// Plugin reads the files of a kind of system.
type Plugin interface {
	// ID is the name systems refer to in their Plugin field.
	ID() string
	// Hash returns the checksums a file may be listed under in a DAT. A file can have several:
	// with and without a copier header, or in another byte order. The first is the file as is.
	Hash(system *systems.System, r io.Reader, size int64) ([]Hashes, error)
}

var plugins = map[string]Plugin{}

// Register adds a plugin; plugins call it from their init function.
func Register(p Plugin) {
	plugins[p.ID()] = p
}

// For returns the plugin of a system.
func For(system *systems.System) (Plugin, error) {
	p, ok := plugins[system.Plugin]
	if !ok {
		return nil, fmt.Errorf("no plugin %q for %s", system.Plugin, system.ID)
	}
	return p, nil
}
