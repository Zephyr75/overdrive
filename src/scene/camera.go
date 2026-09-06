package scene

import (
	"github.com/Zephyr75/overdrive/utils"
	"github.com/go-gl/mathgl/mgl32"
	"math"
)

type Camera struct {
	Name  string
	Type  string
	Pos   mgl32.Vec3
	Front mgl32.Vec3
	Up    mgl32.Vec3
	Yaw   float32
	Pitch float32
	Fov   float32
}

type CameraXml struct {
	Name  string  `xml:"name,attr"`
	Type  string  `xml:"type"`
	Pos   string  `xml:"position"`
	Front string  `xml:"front"`
	Up    string  `xml:"up"`
	Yaw   float32 `xml:"yaw"`
	Pitch float32 `xml:"pitch"`
	Fov   float32 `xml:"fov"`
}

// Teleports the camera to a position
func (camera *Camera) Move(pos mgl32.Vec3) {
	camera.Pos = pos
}

// Points the camera's front vector at a position
func (camera *Camera) LookAt(pos mgl32.Vec3) {
	camera.Front = pos.Sub(camera.Pos).Normalize()
}

// Converts a parsed XML camera into engine coordinates, deriving front from yaw and pitch
func (camera CameraXml) toCamera() Camera {
	pos := utils.ParseVec3(camera.Pos)
	front := utils.ParseVec3(camera.Front)
	up := utils.ParseVec3(camera.Up)
	pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
	// front = mgl32.Vec3{front[0], front[2], front[1]}
	// up = mgl32.Vec3{up[0], up[2], up[1]}
	up = mgl32.Vec3{0.0, 1.0, 0.0}

	var direction mgl32.Vec3
	direction[2] = -float32(math.Cos(float64(mgl32.DegToRad(camera.Pitch))) * math.Cos(float64(mgl32.DegToRad(camera.Yaw))))
	direction[1] = -float32(math.Sin(float64(mgl32.DegToRad(camera.Pitch))))
	direction[0] = -float32(math.Cos(float64(mgl32.DegToRad(camera.Pitch))) * math.Sin(float64(mgl32.DegToRad(camera.Yaw))))
	front = direction.Normalize()

	return Camera{
		Name:  camera.Name,
		Type:  camera.Type,
		Pos:   pos,
		Front: front,
		Up:    up,
		Yaw:   camera.Yaw,
		Pitch: camera.Pitch,
		Fov:   camera.Fov,
	}
}
