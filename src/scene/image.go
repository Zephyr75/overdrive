package scene

import (
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

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

// Box-filters RGBA8 pixels down to 1x1, level 0 being the input itself
//
// Averages the stored bytes, so an sRGB albedo is filtered in gamma space: a
// little dark at distance, kept for simplicity
func mipChain(pixels []byte, width, height int) []mipLevel {
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
