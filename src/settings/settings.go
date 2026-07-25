package settings

var (
	WindowWidth  int = 1920
	WindowHeight int = 1080
	ShadowWidth  int = 1024
	ShadowHeight int = 1024
)

// Returns the window's aspect ratio, for the camera projection
func AspectRatio() float32 {
	return float32(WindowWidth) / float32(WindowHeight)
}

// Returns the shadow map's aspect ratio, for the cube shadow projection
func ShadowAspectRatio() float32 {
	return float32(ShadowWidth) / float32(ShadowHeight)
}
