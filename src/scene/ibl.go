package scene

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
)

// Roughness levels of the prefiltered specular map, one mip each: keep in step
// with SPECULAR_LEVELS in common.slang
const specularLevels = 6

// Widths of the maps baked from the source: the sky drawn behind the scene, the
// mirror level of the specular chain, and the diffuse irradiance
const (
	skyWidth        = 2048
	specularWidth   = 512
	irradianceWidth = 32
	// GGX samples per specular texel; the source mip each reads keeps 128 smooth
	specularSamples = 128
)

// A float RGB equirectangular image, row 0 at the top (+Y)
//
// Its direction mapping is envUV in common.slang: both sides must agree
type equirect struct {
	width, height int
	pix           []float32 // RGB, tightly packed
}

// Decodes a Radiance .hdr (RGBE, flat or new-style RLE scanlines), the format
// Blender and the asset converter write an environment as
func loadHDR(path string) (*equirect, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)

	// Header lines end at a blank one; the resolution line follows it
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("hdr header: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("hdr resolution: %w", err)
	}
	var width, height int
	if _, err := fmt.Sscanf(line, "-Y %d +X %d", &height, &width); err != nil {
		return nil, fmt.Errorf("hdr resolution %q: only -Y H +X W is supported", strings.TrimSpace(line))
	}

	img := &equirect{width: width, height: height, pix: make([]float32, width*height*3)}
	scanline := make([]byte, width*4)
	for y := 0; y < height; y++ {
		if err := readScanline(reader, scanline, width); err != nil {
			return nil, fmt.Errorf("hdr row %d: %w", y, err)
		}
		for x := 0; x < width; x++ {
			rgbe := scanline[x*4 : x*4+4]
			if rgbe[3] == 0 {
				continue
			}
			scale := float32(math.Ldexp(1, int(rgbe[3])-(128+8)))
			i := (y*width + x) * 3
			img.pix[i], img.pix[i+1], img.pix[i+2] = float32(rgbe[0])*scale, float32(rgbe[1])*scale, float32(rgbe[2])*scale
		}
	}
	return img, nil
}

// Reads one RGBE scanline into out, interleaved; RLE stores each channel as its own run list
func readScanline(reader *bufio.Reader, out []byte, width int) error {
	var head [4]byte
	if _, err := io.ReadFull(reader, head[:]); err != nil {
		return err
	}
	// Anything but the 2,2,hi,lo marker is a flat scanline that started with a pixel
	if head[0] != 2 || head[1] != 2 || head[2]&0x80 != 0 || width < 8 || width > 0x7fff {
		copy(out, head[:])
		_, err := io.ReadFull(reader, out[4:])
		return err
	}
	if int(head[2])<<8|int(head[3]) != width {
		return fmt.Errorf("scanline width mismatch")
	}
	for channel := 0; channel < 4; channel++ {
		for x := 0; x < width; {
			count, err := reader.ReadByte()
			if err != nil {
				return err
			}
			// Above 128 is one value repeated, otherwise that many literal values
			if count > 128 {
				run := int(count) - 128
				value, err := reader.ReadByte()
				if err != nil {
					return err
				}
				if x+run > width {
					return fmt.Errorf("run overflows scanline")
				}
				for ; run > 0; run-- {
					out[x*4+channel] = value
					x++
				}
			} else {
				if count == 0 || x+int(count) > width {
					return fmt.Errorf("bad literal run")
				}
				for n := 0; n < int(count); n++ {
					value, err := reader.ReadByte()
					if err != nil {
						return err
					}
					out[x*4+channel] = value
					x++
				}
			}
		}
	}
	return nil
}

// A constant-colour environment, what a scene without <environment> is lit by
func flatEquirect(value float32) *equirect {
	img := &equirect{width: 8, height: 4, pix: make([]float32, 8*4*3)}
	for i := range img.pix {
		img.pix[i] = value
	}
	return img
}

// Caps every channel, which takes the sun out of the image-based light: a scene
// light carries it instead, the only way it can cast a shadow
func (img *equirect) clamp(limit float32) {
	for i, v := range img.pix {
		img.pix[i] = min(v, limit)
	}
}

// Multiplies every channel
func (img *equirect) scale(factor float32) {
	for i := range img.pix {
		img.pix[i] *= factor
	}
}

// 2x2 box average, wrapping in x
func (img *equirect) half() *equirect {
	width, height := max(img.width/2, 1), max(img.height/2, 1)
	out := &equirect{width: width, height: height, pix: make([]float32, width*height*3)}
	for y := 0; y < height; y++ {
		y0, y1 := min(2*y, img.height-1), min(2*y+1, img.height-1)
		for x := 0; x < width; x++ {
			x0, x1 := (2*x)%img.width, (2*x+1)%img.width
			for c := 0; c < 3; c++ {
				out.pix[(y*width+x)*3+c] = 0.25 * (img.pix[(y0*img.width+x0)*3+c] + img.pix[(y0*img.width+x1)*3+c] +
					img.pix[(y1*img.width+x0)*3+c] + img.pix[(y1*img.width+x1)*3+c])
			}
		}
	}
	return out
}

// Bilinear lookup at u, v in [0, 1], wrapping in u and clamping in v
func (img *equirect) bilinear(u, v float64) [3]float32 {
	fx := u*float64(img.width) - 0.5
	fy := min(max(v*float64(img.height)-0.5, 0), float64(img.height-1))
	x0f, y0f := math.Floor(fx), math.Floor(fy)
	tx, ty := float32(fx-x0f), float32(fy-y0f)
	x0 := ((int(x0f) % img.width) + img.width) % img.width
	x1 := (x0 + 1) % img.width
	y0 := int(y0f)
	y1 := min(y0+1, img.height-1)
	var out [3]float32
	for c := 0; c < 3; c++ {
		a := img.pix[(y0*img.width+x0)*3+c]*(1-tx) + img.pix[(y0*img.width+x1)*3+c]*tx
		b := img.pix[(y1*img.width+x0)*3+c]*(1-tx) + img.pix[(y1*img.width+x1)*3+c]*tx
		out[c] = a*(1-ty) + b*ty
	}
	return out
}

// Direction of a uv, the inverse of envUV in common.slang (without its rotation)
func equirectDir(u, v float64) [3]float64 {
	phi := (u - 0.5) * 2 * math.Pi
	lat := (0.5 - v) * math.Pi
	return [3]float64{math.Cos(lat) * math.Cos(phi), math.Sin(lat), math.Cos(lat) * math.Sin(phi)}
}

// envUV in common.slang, without its rotation
func equirectUV(d [3]float64) (float64, float64) {
	u := 0.5 + math.Atan2(d[2], d[0])/(2*math.Pi)
	v := 0.5 - math.Asin(min(max(d[1], -1), 1))/math.Pi
	return u, v
}

// The source halved down to a few texels: chain[i] is the source at 1/2^i
type equirectChain []*equirect

func buildChain(src *equirect) equirectChain {
	chain := equirectChain{src}
	for src.width > 4 {
		src = src.half()
		chain = append(chain, src)
	}
	return chain
}

// The first level no wider than width
func (chain equirectChain) atWidth(width int) *equirect {
	for _, level := range chain {
		if level.width <= width {
			return level
		}
	}
	return chain[len(chain)-1]
}

// Trilinear lookup of a direction at a fractional level of the chain
func (chain equirectChain) sample(d [3]float64, level float64) [3]float32 {
	u, v := equirectUV(d)
	level = min(max(level, 0), float64(len(chain)-1))
	lo := int(level)
	hi := min(lo+1, len(chain)-1)
	t := float32(level - float64(lo))
	a, b := chain[lo].bilinear(u, v), chain[hi].bilinear(u, v)
	return [3]float32{a[0]*(1-t) + b[0]*t, a[1]*(1-t) + b[1]*t, a[2]*(1-t) + b[2]*t}
}

// Fills every texel of a width x height equirect from its direction, rows in parallel
func bake(width, height int, texel func(d [3]float64) [3]float32) *equirect {
	out := &equirect{width: width, height: height, pix: make([]float32, width*height*3)}
	var wait sync.WaitGroup
	for y := 0; y < height; y++ {
		wait.Add(1)
		go func(y int) {
			defer wait.Done()
			for x := 0; x < width; x++ {
				c := texel(equirectDir((float64(x)+0.5)/float64(width), (float64(y)+0.5)/float64(height)))
				copy(out.pix[(y*width+x)*3:], c[:])
			}
		}(y)
	}
	wait.Wait()
	return out
}

// Cosine-weighted irradiance over pi, so the shader's diffuse is irradiance * albedo
func bakeIrradiance(chain equirectChain) *equirect {
	// Every texel of a 64-wide source against every output texel: ~1M products
	src := chain.atWidth(2 * irradianceWidth)
	solidAngle := make([]float64, src.height)
	for y := range solidAngle {
		lat := (0.5 - (float64(y)+0.5)/float64(src.height)) * math.Pi
		solidAngle[y] = (2 * math.Pi / float64(src.width)) * (math.Pi / float64(src.height)) * math.Cos(lat)
	}
	return bake(irradianceWidth, irradianceWidth/2, func(n [3]float64) [3]float32 {
		var sum [3]float64
		for y := 0; y < src.height; y++ {
			for x := 0; x < src.width; x++ {
				l := equirectDir((float64(x)+0.5)/float64(src.width), (float64(y)+0.5)/float64(src.height))
				cos := n[0]*l[0] + n[1]*l[1] + n[2]*l[2]
				if cos <= 0 {
					continue
				}
				w := cos * solidAngle[y]
				i := (y*src.width + x) * 3
				sum[0] += float64(src.pix[i]) * w
				sum[1] += float64(src.pix[i+1]) * w
				sum[2] += float64(src.pix[i+2]) * w
			}
		}
		return [3]float32{float32(sum[0] / math.Pi), float32(sum[1] / math.Pi), float32(sum[2] / math.Pi)}
	})
}

// One GGX sample in the frame where the normal is +Z, with the chain level it reads
type ggxSample struct {
	dir    [3]float64
	weight float64 // N.L
	level  float64
}

// The prefiltered specular chain: level k is roughness k / (specularLevels-1),
// at specularWidth / 2^k, so the shader picks its mip by roughness alone
//
// Split-sum with N = V = R, and filtered importance sampling: each sample reads
// the chain level whose texel covers its share of the lobe, which is what keeps
// 128 samples from turning the sun into fireflies
func bakeSpecular(chain equirectChain) []*equirect {
	levels := []*equirect{chain.atWidth(specularWidth)}
	// Solid angle of one texel of the chain's top level, averaged over the sphere
	texelSolidAngle := 4 * math.Pi / float64(chain[0].width*chain[0].height)
	for k := 1; k < specularLevels; k++ {
		roughness := float64(k) / float64(specularLevels-1)
		alpha := roughness * roughness
		a2 := alpha * alpha

		// The same samples serve every texel, rotated into its frame
		var samples []ggxSample
		for i := 0; i < specularSamples; i++ {
			// Hammersley point, the radical inverse in base 2
			e1 := float64(i) / specularSamples
			e2 := float64(reverseBits(uint32(i))) / (1 << 32)
			phi := 2 * math.Pi * e1
			cosTheta := math.Sqrt((1 - e2) / (1 + (a2-1)*e2))
			sinTheta := math.Sqrt(1 - cosTheta*cosTheta)
			h := [3]float64{sinTheta * math.Cos(phi), sinTheta * math.Sin(phi), cosTheta}
			// L = reflect(-V, H) with V = N = +Z
			l := [3]float64{2 * cosTheta * h[0], 2 * cosTheta * h[1], 2*cosTheta*h[2] - 1}
			if l[2] <= 0 {
				continue
			}
			// pdf of L is D * NdotH / (4 VdotH), and VdotH = NdotH here
			d := a2 / (math.Pi * math.Pow(cosTheta*cosTheta*(a2-1)+1, 2))
			sampleSolidAngle := 1 / (specularSamples * d / 4)
			level := max(0.5*math.Log2(sampleSolidAngle/texelSolidAngle)+1, 0)
			samples = append(samples, ggxSample{l, l[2], level})
		}

		// Exactly the mip's size, which Vulkan derives from level 0
		width, height := max(levels[0].width>>k, 1), max(levels[0].height>>k, 1)
		levels = append(levels, bake(width, height, func(n [3]float64) [3]float32 {
			t, b := tangentFrame(n)
			var sum [3]float64
			var total float64
			for _, s := range samples {
				d := [3]float64{
					t[0]*s.dir[0] + b[0]*s.dir[1] + n[0]*s.dir[2],
					t[1]*s.dir[0] + b[1]*s.dir[1] + n[1]*s.dir[2],
					t[2]*s.dir[0] + b[2]*s.dir[1] + n[2]*s.dir[2],
				}
				c := chain.sample(d, s.level)
				sum[0] += float64(c[0]) * s.weight
				sum[1] += float64(c[1]) * s.weight
				sum[2] += float64(c[2]) * s.weight
				total += s.weight
			}
			return [3]float32{float32(sum[0] / total), float32(sum[1] / total), float32(sum[2] / total)}
		}))
	}
	return levels
}

// Two unit vectors perpendicular to n and to each other
func tangentFrame(n [3]float64) ([3]float64, [3]float64) {
	up := [3]float64{0, 1, 0}
	if math.Abs(n[1]) > 0.999 {
		up = [3]float64{1, 0, 0}
	}
	t := cross(up, n)
	length := math.Sqrt(t[0]*t[0] + t[1]*t[1] + t[2]*t[2])
	t = [3]float64{t[0] / length, t[1] / length, t[2] / length}
	return t, cross(n, t)
}

func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func reverseBits(v uint32) uint32 {
	v = (v<<16 | v>>16)
	v = (v&0x55555555)<<1 | (v&0xAAAAAAAA)>>1
	v = (v&0x33333333)<<2 | (v&0xCCCCCCCC)>>2
	v = (v&0x0F0F0F0F)<<4 | (v&0xF0F0F0F0)>>4
	v = (v&0x00FF00FF)<<8 | (v&0xFF00FF00)>>8
	return v
}

// RGBA16F bytes, alpha 1, the format every environment map is uploaded in
func (img *equirect) rgba16f() []byte {
	out := make([]byte, img.width*img.height*8)
	one := float16(1)
	for i := 0; i < img.width*img.height; i++ {
		for c := 0; c < 3; c++ {
			h := float16(img.pix[i*3+c])
			out[i*8+c*2], out[i*8+c*2+1] = byte(h), byte(h>>8)
		}
		out[i*8+6], out[i*8+7] = byte(one), byte(one>>8)
	}
	return out
}

// IEEE half from a float32: rounds to nearest, saturates at 65504, flushes
// denormals to zero — an environment has no use for either extreme
func float16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits>>16) & 0x8000
	if f != f {
		return sign | 0x7e00
	}
	exponent := int(bits>>23&0xff) - 127 + 15
	mantissa := bits & 0x7fffff
	if exponent <= 0 {
		return sign
	}
	if exponent >= 31 {
		return sign | 0x7bff
	}
	h := uint32(exponent)<<10 | mantissa>>13
	// Round half up on the dropped bits; a carry into the exponent is still correct
	if mantissa&0x1000 != 0 {
		h++
	}
	if h >= 0x7c00 {
		return sign | 0x7bff
	}
	return sign | uint16(h)
}
