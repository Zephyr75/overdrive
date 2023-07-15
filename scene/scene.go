package scene

var (
  Cam Camera = NewCamera()
)

type SceneXml struct {
  CamXml CameraXml `xml:"camera"`
  MeshesXml []MeshXml `xml:"mesh"`
  LightsXml []LightXml `xml:"light"`
}

type Scene struct {
  Cam Camera
  Meshes []Mesh
  Lights []Light
}
