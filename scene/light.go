package scene

import (
  "github.com/go-gl/mathgl/mgl32"
  "overdrive/utils"
	"github.com/go-gl/gl/v4.1-core/gl"

	"overdrive/settings"
)

type LightXml struct {
  Type string `xml:"type,attr"`
  Pos string `xml:"position"`
	Dir string `xml:"direction"`
  Color string `xml:"color"`
  Diffuse float32 `xml:"diffuse"`
  Specular float32 `xml:"specular"`
  Intensity float32 `xml:"intensity"`
}

type Light struct {
  Type int
  Pos mgl32.Vec3 
  Dir mgl32.Vec3 
	CutOff float32
  Color mgl32.Vec3
  Diffuse float32
  Specular float32
  Intensity float32
	DepthMapFBO uint32
  DepthMap uint32
}

func (l LightXml) ToLight() Light {
	t := 0
  pos := utils.ParseVec3(l.Pos)
  dir := utils.ParseVec3(l.Dir)
  color := utils.ParseVec3(l.Color)

  pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
  // dir = utils.EulerToDirection(dir[0], dir[1], dir[2])
	dir = mgl32.Vec3{-dir[0], -dir[2], dir[1]}
	// dir = dir.Add(mgl32.Vec3{0, 1, 0})
	// dir = mgl32.Vec3{0, 1, 0}
	// dir = dir.Mul(180.0 / 3.14)
	switch l.Type {
	case "sun":
		t = 0
	case "point":
		t = 1
	}
  return Light{
    Type: t,
    Pos: pos,
		Dir: dir,
    Color: color,
		Diffuse: l.Diffuse,
		Specular: l.Specular,
    Intensity: l.Intensity,
  }
}

func (l *Light) Setup() {
  gl.GenFramebuffers(1, &l.DepthMapFBO)
  gl.GenTextures(1, &l.DepthMap)
  gl.BindTexture(gl.TEXTURE_2D, l.DepthMap)
  gl.TexImage2D(gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT, int32(settings.ShadowWidth), int32(settings.ShadowHeight), 0, gl.DEPTH_COMPONENT, gl.FLOAT, nil)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_BORDER)
  gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_BORDER)
  borderColor := []float32{1.0, 1.0, 1.0, 1.0}
  gl.TexParameterfv(gl.TEXTURE_2D, gl.TEXTURE_BORDER_COLOR, &borderColor[0])

  gl.BindFramebuffer(gl.FRAMEBUFFER, l.DepthMapFBO)
  gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, l.DepthMap, 0)
  gl.DrawBuffer(gl.NONE)
  gl.ReadBuffer(gl.NONE)
  gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
}
