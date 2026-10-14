// Package disc is the plugin for CD systems. A disc is identified by its first track, the data
// track, which the libretro DATs list: as a BIN file of raw sectors, or read from a CHD.
package disc

import (
	"io"

	"github.com/DeLFuS77/completist/internal/platform"
	"github.com/DeLFuS77/completist/internal/platform/cartridge"
	"github.com/DeLFuS77/completist/internal/systems"
)

func init() {
	platform.Register(Plugin{})
}

// Plugin hashes disc tracks as they are: no headers, no other byte orders.
type Plugin struct{}

func (Plugin) ID() string { return "disc" }

func (Plugin) Hash(system *systems.System, r io.Reader, size int64) ([]platform.Hashes, error) {
	// the cartridge plugin streams the file; without a header it hashes it as it is
	return cartridge.Plugin{}.Hash(&systems.System{ID: system.ID}, r, size)
}
