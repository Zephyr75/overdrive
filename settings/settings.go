package settings

var (
	WindowWidth  int = 1920
	WindowHeight int = 1080
	ShadowWidth  int = 1024
	ShadowHeight int = 1024
)

func AspectRatio() float32 {
	return float32(WindowWidth) / float32(WindowHeight)
}

func ShadowAspectRatio() float32 {
	return float32(ShadowWidth) / float32(ShadowHeight)
}
