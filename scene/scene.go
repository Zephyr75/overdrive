package scene

var (
  Cam Camera = NewCamera()
)

type SceneXml struct {
  Cam CameraXml `xml:"camera"`
  Meshes []MeshXml `xml:"mesh"`
  Lights []LightXml `xml:"light"`
}

type Scene struct {
  Cam Camera
  Meshes []Mesh
  Lights []Light
}
