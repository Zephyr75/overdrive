package core

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// A frame captured off the swapchain and written to a PNG
type screenshot struct {
	path string
	// Which frame to capture. Physics and the shadow allocator both need a few
	// frames to settle, so capturing frame 0 would photograph a scene mid-fall
	frame  int
	buffer renderer.BufferHandle
	taken  bool
}

// Records the copy out of this frame's swapchain image, if this is the frame to capture
func (shot *screenshot) record(backend renderer.Backend, frame renderer.Frame, n int) bool { // TODO: review
	if shot == nil || shot.taken || n != shot.frame {
		return false
	}
	width, height := settings.WindowWidth, settings.WindowHeight
	if shot.buffer == 0 {
		shot.buffer, _ = backend.CreateBuffer(renderer.BufferInfo{
			Name: "screenshot", Size: uint64(width * height * 4),
			Usage: renderer.BufferCopyDst, Location: renderer.LocationHost,
		})
	}
	frame.Copy(renderer.CopySpec{
		SrcImage: renderer.BackbufferImage, DstBuffer: shot.buffer,
		Extent: [3]int{width, height, 1}, Aspect: renderer.AspectColor,
	})
	return true
}

// Reads the captured buffer back and writes it as a PNG, after the frame that
// recorded the copy has been submitted
func (shot *screenshot) write(backend renderer.Backend) error { // TODO: review
	width, height := settings.WindowWidth, settings.WindowHeight
	pixels := backend.ReadBuffer(shot.buffer)
	if len(pixels) < width*height*4 {
		return fmt.Errorf("screenshot: read %d bytes, wanted %d", len(pixels), width*height*4)
	}
	shot.taken = true

	// The swapchain is BGRA8; PNG wants RGBA
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		img.Pix[i*4+0] = pixels[i*4+2]
		img.Pix[i*4+1] = pixels[i*4+1]
		img.Pix[i*4+2] = pixels[i*4+0]
		img.Pix[i*4+3] = 255
	}

	file, err := os.Create(shot.path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}
