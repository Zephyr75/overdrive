module github.com/Zephyr75/overdrive

go 1.26.3

require (
	github.com/Zephyr75/gutter v0.1.2
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260628091122-0bd588dc30cf
	github.com/go-gl/mathgl v1.2.0
)

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/mdouchement/hdr v0.2.4
	github.com/x448/float16 v0.8.4
)

require github.com/lucasb-eyer/go-colorful v1.2.0 // indirect

require (
	github.com/goki/freetype v1.0.1 // indirect
	go-vulkan v0.0.0
	golang.org/x/image v0.6.0 // indirect
)

replace go-vulkan => ../../go-vulkan

// Local gutter until it has a tagged release with the draw list API
replace github.com/Zephyr75/gutter => ../../gutter
