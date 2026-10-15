package scene

import (
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"runtime"
	"sync"
)

// A texture file decoded and mipped on the CPU, or why it could not be
type decodedTexture struct {
	levels []mipLevel
	// Which channel an alpha map reads: .a (3) when the file has a texel that is
	// not opaque there, .r (0) for a grayscale mask, which decodes to opaque gray RGB
	alphaChannel int32
	// Some texel of that channel is below opaque, so a material it masks has
	// fragments for the transparent pass
	translucent bool
	err         error
}

// Decodes and mips every material texture the meshes name, a core per file
//
// Decoding is CPU-bound and independent per file, and done one at a time it
// was most of the startup: 16-bit PNGs take up to a second each. Capped at the
// core count so a large scene does not hold every decoded image at once
func decodeTextures(meshes []Mesh) map[string]*decodedTexture {
	textures := map[string]*decodedTexture{}
	alphaMaps := map[string]bool{}
	for _, mesh := range meshes {
		for _, mat := range mesh.Materials {
			for _, path := range []string{mat.TexturePath, mat.NormalMapPath, mat.RoughnessMapPath, mat.MetallicMapPath, mat.AlphaMapPath} {
				if path != "" {
					textures[path] = &decodedTexture{}
				}
			}
			if mat.AlphaMapPath != "" {
				alphaMaps[mat.AlphaMapPath] = true
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
			if alphaMaps[path] {
				if hasTexelBelow(pixels, 3, 255) {
					texture.alphaChannel = 3
				}
				texture.translucent = hasTexelBelow(pixels, int(texture.alphaChannel), 255)
				preserveCoverage(texture.levels, int(texture.alphaChannel))
			}
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

// Reports whether any RGBA8 texel's channel is below limit
func hasTexelBelow(pixels []byte, channel int, limit byte) bool {
	for i := channel; i < len(pixels); i += 4 {
		if pixels[i] < limit {
			return true
		}
	}
	return false
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

// Rescales an alpha map's channel in every mip below the first so the share of
// texels reaching OpaqueAlpha stays what it is at full size
//
// Averaging 2x2 blocks pulls a foliage edge's alpha towards the middle, so
// without this a distant leaf falls below OpaqueAlpha everywhere and the whole
// canopy moves to the unsorted, shadowless transparent pass. Each level is
// scaled on its own: the next was already averaged from the unscaled one
func preserveCoverage(levels []mipLevel, channel int) {
	target := opaqueCoverage(histogram(levels[0].pixels, channel), 1)
	// Nothing opaque to keep, or nothing else: either way the scale would be 0 or 1
	if target == 0 || target == 1 {
		return
	}
	for _, level := range levels[1:] {
		counts := histogram(level.pixels, channel)
		// Coverage only grows with the scale, so bisect for the one matching target
		low, high := 0.0, 4.0
		for range 16 {
			mid := (low + high) / 2
			if opaqueCoverage(counts, mid) < target {
				low = mid
			} else {
				high = mid
			}
		}
		for i := channel; i < len(level.pixels); i += 4 {
			level.pixels[i] = byte(min(255, math.Round(float64(level.pixels[i])*high)))
		}
	}
}

// How many texels hold each value of one channel
func histogram(pixels []byte, channel int) [256]int {
	var counts [256]int
	for i := channel; i < len(pixels); i += 4 {
		counts[pixels[i]]++
	}
	return counts
}

// The share of texels at or above OpaqueAlpha once scaled by scale
func opaqueCoverage(counts [256]int, scale float64) float64 {
	opaque, total := 0, 0
	for value, count := range counts {
		if min(255, math.Round(float64(value)*scale)) >= OpaqueAlpha*255 {
			opaque += count
		}
		total += count
	}
	return float64(opaque) / float64(total)
}
