package core

import (
	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/scene"
)

// One triangle overhanging the screen: every pixel covered once, with no
// diagonal seam between two halves as a quad would have
var fullscreenTriangle = []float32{
	-1, -1, 0,
	3, -1, 0,
	-1, 3, 0,
}

// The full-screen pass that turns the prepass depth into occlusion, which
// forward.slang blurs as it reads: the screenTargets hold its image, this holds
// what draws into it
type occlusionPasses struct {
	occlusion renderer.PipelineHandle
	triangle  renderer.MeshHandle
}

// Builds the triangle and the pipeline, writing occlusion and view distance
func newOcclusionPasses(backend renderer.Backend) (*occlusionPasses, error) {
	passes := &occlusionPasses{}
	buf, _ := backend.CreateBuffer(renderer.BufferSpec{
		Name: "fullscreenTriangle", Usage: renderer.BufferVertex,
		Location: renderer.LocationHost, InitialData: fullscreenTriangle,
	})
	passes.triangle = backend.CreateMesh(renderer.MeshSpec{Name: "fullscreenTriangle", Vertices: buf, Stride: 3 * 4})

	// No culling and no depth: the triangle's winding depends on nothing, and
	// the pass writes a colour image alone
	spec := renderer.PipelineSpec{
		Name: "ssao", Shader: "ssao",
		Vertex: renderer.VertexLayout{
			Stride: 3 * 4,
			Attrs:  []renderer.VertexAttr{{Location: 0, Format: renderer.FormatRGB32F, Offset: 0}},
		},
		Cull:         renderer.CullNone,
		DepthCompare: renderer.CompareNone,
		Blend:        renderer.BlendNone,
		ColorFormats: []renderer.Format{renderer.FormatRG16F},
		Samples:      1,
	}
	var err error
	passes.occlusion, err = backend.CreatePipeline(spec)
	if err != nil {
		return nil, err
	}
	return passes, nil
}

// Writes the occlusion slots and settings into the frame block, before it is uploaded
//
// TexAO stays 0, the white pixel, when nothing will run this frame, which is
// AO off as far as forward.slang can tell
func (passes *occlusionPasses) fillUniforms(backend renderer.Backend, targets *screenTargets, uniforms *renderer.FrameUniforms, enabled bool, radius, intensity float32) {
	uniforms.AORadius, uniforms.AOIntensity = radius, intensity
	if !enabled || targets.ao == 0 {
		return
	}
	uniforms.TexDepth = int32(backend.Slot(targets.sceneDepth))
	uniforms.TexAO = int32(backend.Slot(targets.ao))
}

// The pass overwrites every pixel, so it clears rather than loads what the last
// frame left: a load would also need the attachment's read access synchronised
var unoccluded = [4]float32{1, 1, 1, 1}

// Records the occlusion pass, reading the depth the prepass just wrote
func (passes *occlusionPasses) record(frame renderer.Frame, targets *screenTargets, frameAddr renderer.Address) {
	frame.Pass(renderer.PassSpec{
		Name:  "ssao",
		Color: []renderer.Attachment{{View: targets.aoView, Clear: &unoccluded, Store: true}},
		Reads: []renderer.Handle{targets.sceneDepth},
	}, func(pass renderer.Pass) {
		pass.Draw(renderer.DrawCall{Pipeline: passes.occlusion, Mesh: passes.triangle, Push: scene.FrameOnlyAddressArray(frameAddr)})
	})
}
