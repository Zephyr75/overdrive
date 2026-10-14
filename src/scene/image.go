package scene

import (
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"runtime"
	"sync"
)

// A texture file decoded and mipped on the CPU, or why it could not be
type decodedTexture struct {
	levels []mipLevel
	err    error
}

// Decodes and mips every material texture the meshes name, a core per file
//
// Decoding is CPU-bound and independent per file, and done one at a time it
// was most of the startup: 16-bit PNGs take up to a second each. Capped at the
// core count so a large scene does not hold every decoded image at once
func decodeTextures(meshes []Mesh) map[string]*decodedTexture {
	textures := map[string]*decodedTexture{}
	for _, mesh := range meshes {
		for _, mat := range mesh.Materials {
			for _, path := range []string{mat.TexturePath, mat.NormalMapPath, mat.RoughnessMapPath, mat.MetallicMapPath} {
				if path != "" {
					textures[path] = &decodedTexture{}
				}
			}
		}
	}
	var wg sync.WaitGroup
	cores := make(chan struct{}, runtime.NumCPU())
	for path, texture := range textures {
		wg.Go(func() {
			cores <- struct{}{}
			defer func() { <-cores }()
			pixels, width, height, err := loadRGBA(path)
			if err != nil {
				texture.err = err
				return
			}
			texture.levels = createMipLevels(pixels, width, height)
		})
	}
	wg.Wait()
	return textures
}

// Decodes an image file into tightly packed RGBA8 pixels
func loadRGBA(path string) (pixels []byte, width, height int, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, 0, 0, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)
	size := rgba.Rect.Size()
	return rgba.Pix, size.X, size.Y, nil
}

// One level of a mip chain, tightly packed RGBA8
type mipLevel struct {
	pixels        []byte
	width, height int
}

// createMipLevels generates a full mip‑map chain from an unfiltered RGBA8 image.
// It repeatedly averages 2×2 texel blocks, rounding to the nearest integer,
// and stores each level as a 4‑byte RGBA slice until the image is 1×1.
func createMipLevels(pixels []byte, width, height int) []mipLevel {
	levels := []mipLevel{{pixels, width, height}}
	for width > 1 || height > 1 {
		nextWidth, nextHeight := max(width/2, 1), max(height/2, 1)
		next := make([]byte, nextWidth*nextHeight*4)
		for y := 0; y < nextHeight; y++ {
			// An odd or 1-texel side reads its last texel twice rather than past the edge
			y0, y1 := min(2*y, height-1), min(2*y+1, height-1)
			for x := 0; x < nextWidth; x++ {
				x0, x1 := min(2*x, width-1), min(2*x+1, width-1)
				for c := 0; c < 4; c++ {
					sum := int(pixels[(y0*width+x0)*4+c]) + int(pixels[(y0*width+x1)*4+c]) +
						int(pixels[(y1*width+x0)*4+c]) + int(pixels[(y1*width+x1)*4+c])
					next[(y*nextWidth+x)*4+c] = byte((sum + 2) / 4)
				}
			}
		}
		pixels, width, height = next, nextWidth, nextHeight
		levels = append(levels, mipLevel{pixels, width, height})
	}
	return levels
}
