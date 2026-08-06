package scene

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// Decodes an image file into tightly packed RGBA8 pixels
func loadRGBA(path string) (pixels []byte, w, h int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, 0, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)
	size := rgba.Rect.Size()
	return rgba.Pix, size.X, size.Y, nil
}

// Decodes six cube faces, checking they agree on a size the backend can upload as one image
func loadCubeFaces(paths [6]string) (faces [6][]byte, w, h int, err error) {
	for i, p := range paths {
		pix, fw, fh, e := loadRGBA(p)
		if e != nil {
			return faces, 0, 0, fmt.Errorf("cubemap face %s: %w", p, e)
		}
		if i == 0 {
			w, h = fw, fh
		} else if fw != w || fh != h {
			return faces, 0, 0, fmt.Errorf("cubemap face %s: %dx%d, expected %dx%d", p, fw, fh, w, h)
		}
		faces[i] = pix
	}
	return faces, w, h, nil
}
