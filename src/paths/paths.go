// Package paths resolves every runtime file the engine opens against the
// project root, so the working directory stops mattering.
//
// Nothing else in the tree may hold a relative path literal. Before this
// existed, `assets/…`, `textures/…` and `shaders/vk/…` were scattered through
// scene/, vulkan/ and main.go, all of them assuming the process had been
// started from one particular directory — which is why `go test ./scene/`
// silently skipped rather than ran.
package paths

import (
	"os"
	"path/filepath"
	"sync"
)

// Where each kind of file lives, relative to the root
const (
	assetsDir   = "assets"
	configsDir  = "configs"
	meshesDir   = "assets/meshes"
	texturesDir = "assets/textures"
	// The generated SPIR-V, which build_shaders.sh writes next to its sources
	shadersDir = "src/shaders/vk"
)

// Overrides root discovery, for a build whose layout is not the repository's
const rootEnv = "OVERDRIVE_ROOT"

var (
	once sync.Once
	root string
)

// Returns the project root: the directory holding assets/ and src/
//
// Found by walking up from the working directory, so `go run .` from src/ and
// `go test ./scene/` from src/scene/ both resolve to the same place. Falls back
// to the working directory when nothing matches, which turns a misplaced run
// into a file-not-found naming an absolute path rather than a silent skip.
func Root() string {
	once.Do(func() {
		if env := os.Getenv(rootEnv); env != "" {
			root = env
			return
		}
		dir, err := os.Getwd()
		if err != nil {
			root = "."
			return
		}
		for {
			if isRoot(dir) {
				root = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				// Reached the filesystem root without a match
				root = "."
				return
			}
			dir = parent
		}
	})
	return root
}

// Reports whether dir looks like the project root
//
// Both markers are required: assets/ alone is a common enough directory name to
// match something unrelated on the way up.
func isRoot(dir string) bool {
	for _, marker := range [...]string{assetsDir, "src"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
			return false
		}
	}
	return true
}

// Resolves a path under the project root, leaving an absolute one alone
func resolve(parts ...string) string {
	return filepath.Join(append([]string{Root()}, parts...)...)
}

// Returns the path of a top-level asset, such as a scene XML or a font
func Asset(name string) string { return resolve(assetsDir, name) }

// Returns the path of an OBJ or MTL file
func Mesh(name string) string { return resolve(meshesDir, name) }

// Returns the path of a texture, name being allowed a subdirectory ("skybox/top.png")
func Texture(name string) string { return resolve(texturesDir, name) }

// Returns the path of a compiled shader module
func Shader(name string) string { return resolve(shadersDir, name) }

// Returns the path of a settings file
//
// A bare name resolves under configs/; anything carrying a separator is a path
// the user typed and is used as given, so -config /tmp/try.toml works.
func Config(name string) string {
	if filepath.Base(name) != name {
		return name
	}
	return resolve(configsDir, name)
}
