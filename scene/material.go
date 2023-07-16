package scene

import (
	// "bufio"
	// "fmt"
	// "os"
	// "strconv"
	// "strings"

	"github.com/go-gl/mathgl/mgl32"
)

type Material struct {
  Alpha float32
  Ambient mgl32.Vec3
  Diffuse mgl32.Vec3
  Specular mgl32.Vec3
  Shininess float32
  Texture uint32
  NormalMap uint32
}
