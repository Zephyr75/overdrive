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
func loadRGBA(path string) (pixels []byte, width, height int, err error) { // TODO: review
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

// Decodes six cube faces, checking they agree on a size the backend can upload as one image
func loadCubeFaces(paths [6]string) (faces [6][]byte, width, height int, err error) { // TODO: review
	for i, path := range paths {
		pixels, faceWidth, faceHeight, faceErr := loadRGBA(path)
		if faceErr != nil {
			return faces, 0, 0, fmt.Errorf("cubemap face %s: %w", path, faceErr)
		}
		if i == 0 {
			width, height = faceWidth, faceHeight
		} else if faceWidth != width || faceHeight != height {
			return faces, 0, 0, fmt.Errorf("cubemap face %s: %dx%d, expected %dx%d", path, faceWidth, faceHeight, width, height)
		}
		faces[i] = pixels
	}
	return faces, width, height, nil
}
