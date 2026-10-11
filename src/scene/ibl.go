package scene

import (
	"fmt"
	"os"

	"github.com/mdouchement/hdr"
	"github.com/mdouchement/hdr/codec/rgbe"
	"github.com/x448/float16"
)

// Roughness levels of the prefiltered specular map, one mip each: keep in step
// with SPECULAR_LEVELS in common.slang and the cmgen flags in xml_export.py
const specularLevels = 6

// A float RGB equirectangular image, row 0 at the top (+Y):
// it maps a 360 degrees image to a rectangle
type equirectImage struct {
	width, height int
	pix           []float32 // RGB, tightly packed
}

// Decodes a Radiance .hdr, the format the exporter writes the sky and cmgen the
// baked maps in; XYZE files decode to another type and are refused
func loadHDR(path string) (*equirectImage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoded, err := rgbe.Decode(file)
	if err != nil {
		return nil, err
	}
	img, ok := decoded.(*hdr.RGB)
	if !ok {
		return nil, fmt.Errorf("only RGBE is supported, not %T", decoded)
	}
	// Bounds start at 0,0 and rows are unpadded, so Pix is already tightly packed
	return &equirectImage{width: img.Rect.Dx(), height: img.Rect.Dy(), pix: img.Pix}, nil
}

// A constant-colour environment, what a scene without <environment> is lit by
func flatColorEquirectImage(value float32) *equirectImage {
	img := &equirectImage{width: 8, height: 4, pix: make([]float32, 8*4*3)}
	for i := range img.pix {
		img.pix[i] = value
	}
	return img
}

// Multiplies every channel
func (img *equirectImage) scale(factor float32) {
	for i := range img.pix {
		img.pix[i] *= factor
	}
}

// RGBA16F bytes, alpha 1, the format every environment map is uploaded in
// Vulkan guarantees linear filtering on RGBA16F, but not on RGBA32F.
// Filtering is what blends neighbouring texels and mip levels, which is how the specular lookup works.
// Half floats also use half the memory: the 4096×2048 sky takes 64 MB instead of 128 MB.
func (img *equirectImage) rgba16f() []byte {
	out := make([]byte, img.width*img.height*8)
	for i := 0; i < img.width*img.height; i++ {
		for c := 0; c < 4; c++ {
			value := float32(1)
			if c < 3 {
				value = img.pix[i*3+c]
			}
			// Capped at the largest half: an infinite texel would filter to NaN, which renders black
			h := float16.Fromfloat32(min(value, 65504)).Bits()
			out[i*8+c*2], out[i*8+c*2+1] = byte(h), byte(h>>8)
		}
	}
	return out
}
