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

// Converts a parsed XML camera into engine coordinates, front from the exported vector or yaw and pitch
func (camera CameraXml) toCamera() Camera { 
	pos := utils.ParseVec3(camera.Pos)
	front := utils.ParseVec3(camera.Front)
	up := utils.ParseVec3(camera.Up)
	pos = mgl32.Vec3{pos[0], pos[2], -pos[1]}
	// front = mgl32.Vec3{front[0], front[2], front[1]}
	// up = mgl32.Vec3{up[0], up[2], up[1]}
	up = mgl32.Vec3{0.0, 1.0, 0.0}

	// Yaw and pitch from the exported front rather than the XML's own, which are
	// Blender Euler angles (90 degrees of X is level there): the inverse of the
	// direction formula input.DefaultMouseCallback rebuilds front with
	yaw, pitch := camera.Yaw, camera.Pitch
	if front.Len() > 0 {
		front = mgl32.Vec3{front[0], front[2], -front[1]}.Normalize()
		pitch = mgl32.RadToDeg(float32(-math.Asin(float64(front[1]))))
		yaw = mgl32.RadToDeg(float32(math.Atan2(float64(-front[0]), float64(-front[2]))))
	}
	// A scene written by hand may give only yaw and pitch, in the engine's own convention
	front = mgl32.Vec3{
		-float32(math.Cos(float64(mgl32.DegToRad(pitch))) * math.Sin(float64(mgl32.DegToRad(yaw)))),
		-float32(math.Sin(float64(mgl32.DegToRad(pitch)))),
		-float32(math.Cos(float64(mgl32.DegToRad(pitch))) * math.Cos(float64(mgl32.DegToRad(yaw)))),
	}

	return Camera{
		Name:  camera.Name,
		Type:  camera.Type,
		Pos:   pos,
		Front: front,
		Up:    up,
		Yaw:   yaw,
		Pitch: pitch,
		Fov:   camera.Fov,
	}
}
