module github.com/Zephyr75/overdrive/demo

go 1.26.3

require (
	github.com/Zephyr75/gutter v0.1.2
	github.com/Zephyr75/overdrive v0.0.0
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260628091122-0bd588dc30cf
	github.com/go-gl/mathgl v1.2.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/goki/freetype v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mdouchement/hdr v0.2.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go-vulkan v0.0.0 // indirect
	golang.org/x/image v0.6.0 // indirect
)

// The engine from this repository, not a published version
replace github.com/Zephyr75/overdrive => ../src

// Go honours only the main module's replaces, so the engine's own are repeated here
replace go-vulkan => ../../go-vulkan

replace github.com/Zephyr75/gutter => ../../gutter
