package scene

import (
	"fmt"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// Where each block's address sits in a draw's push constant, matching the
// PushConstants struct in shaders/slang/common.slang. The backend pushes four
// opaque words; these names are the only thing that gives them meaning
const (
	PushFrame = iota
	PushDraw
	PushRecords
	PushBake
)

// Bytes per vertex of each stream the engine draws
const (
	// position(3) | normal(3) | uv(2)
	meshStride = 8 * 4
	// position(3): the skybox cube
	positionStride = 3 * 4
	// clip-space position(3) | uv(2): the UI overlay
	overlayStride = 5 * 4
)

// The vertex layouts, named rather than inlined because a depth-only pipeline
// reads the first attribute of the same stream the forward one reads whole
var (
	// A shadow or prepass pipeline declares position alone: an attribute the
	// shader never reads is rejected at pipeline creation
	positionOnly = renderer.VertexLayout{
		Stride: meshStride,
		Attrs:  []renderer.VertexAttr{{Location: 0, Format: renderer.FormatRGB32F, Offset: 0}},
	}
	meshLayout = renderer.VertexLayout{
		Stride: meshStride,
		Attrs: []renderer.VertexAttr{
			{Location: 0, Format: renderer.FormatRGB32F, Offset: 0},
			{Location: 1, Format: renderer.FormatRGB32F, Offset: 3 * 4},
			{Location: 2, Format: renderer.FormatRG32F, Offset: 6 * 4},
		},
	}
	skyboxLayout = renderer.VertexLayout{
		Stride: positionStride,
		Attrs:  []renderer.VertexAttr{{Location: 0, Format: renderer.FormatRGB32F, Offset: 0}},
	}
)

// Every pipeline the scene draws with, built once at startup
//
// A pipeline bakes what used to be pass-scoped state: winding, depth compare,
// blending and the attachment formats. Which is why the shadow passes need no
// SetCullMode and the forward pass no SetDepthCompare
type Pipelines struct {
	Forward    renderer.PipelineHandle
	Skybox     renderer.PipelineHandle
	Depth      renderer.PipelineHandle
	DepthPoint renderer.PipelineHandle
	Prepass    renderer.PipelineHandle
}

// Builds all rendering pipelines: the sample count comes from the swapchain and is
// applied to every pipeline that writes to the backbuffer
func NewPipelines(backend renderer.Backend) (Pipelines, error) { 
	// Number of samples the swapchain renders with (0 = no MSAA).
	samples := backend.Capacities().BackbufferSamples
	var pipes Pipelines
	var err error

	// Determine which depth comparison to use for the forward pass.
	// If a depth pre‑pass is enabled we want EQUAL so the forward shader
	// runs only once per pixel; otherwise we use a normal less‑than.
	forwardCompare := renderer.CompareLess
	if settings.DepthPrepass {
		forwardCompare = renderer.CompareEqual
	}

	// ---- forward pass ----------------------------------------------------
	// Renders the world geometry with lighting. Uses the same depth comparison
	// as the pre‑pass when DepthPrepass is true, otherwise it writes a normal
	// depth value.
	pipes.Forward, err = backend.CreatePipeline(renderer.PipelineSpec{
		Name: "forward", Shader: "forward", Vertex: meshLayout,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: forwardCompare, DepthWrite: true, Blend: renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatDepth32F,
		Samples:      samples,
	})
	if err != nil {
		return pipes, fmt.Errorf("forward pipeline: %w", err)
	}

	// ---- skybox ----------------------------------------------------------
	// Draws the skybox cube behind everything. Uses less‑equal depth so it
	// sits exactly on the far plane without being occluded.
	pipes.Skybox, err = backend.CreatePipeline(renderer.PipelineSpec{
		Name: "skybox", Shader: "skybox", Vertex: skyboxLayout,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: renderer.CompareLessEqual, DepthWrite: true, Blend: renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatDepth32F,
		Samples:      samples,
	})
	if err != nil {
		return pipes, fmt.Errorf("skybox pipeline: %w", err)
	}

	// ---- pre‑pass --------------------------------------------------------
	// Depth‑only pass that writes a depth buffer for the forward pass.
	// No colour is written.
	pipes.Prepass, err = backend.CreatePipeline(renderer.PipelineSpec{
		Name: "prepass", Shader: "prepass", Vertex: positionOnly,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: renderer.CompareLess, DepthWrite: true,
		DepthFormat: renderer.FormatDepth32F, Samples: samples,
	})
	if err != nil {
		return pipes, fmt.Errorf("prepass pipeline: %w", err)
	}

	// ---- shadow atlas depth passes ---------------------------------------
	// These run with 1‑sample (no MSAA) and use a positive viewport
	// (clockwise winding), they write depth to the shadow atlases
	shadow := renderer.PipelineSpec{
		Vertex: positionOnly,
		Cull:   renderer.CullBack, FrontFace: renderer.WindingClockwise,
		DepthCompare: renderer.CompareLess, DepthWrite: true,
		DepthFormat: renderer.FormatDepth32F, Samples: 1,
	}

	// Plane / cube depth pass
	shadow.Name, shadow.Shader = "depth", "depth"
	if pipes.Depth, err = backend.CreatePipeline(shadow); err != nil {
		return pipes, fmt.Errorf("depth pipeline: %w", err)
	}

	// Point‑light depth pass – writes radial distance into the fragment.
	shadow.Name, shadow.Shader = "depthPoint", "depth_point"
	if pipes.DepthPoint, err = backend.CreatePipeline(shadow); err != nil {
		return pipes, fmt.Errorf("depth_point pipeline: %w", err)
	}

	return pipes, nil
}


// What a run of draws shares: the pass they record into, the pipeline they use,
// and the three block addresses that do not change between them
//
// Only the draw block is uploaded per draw, which is what the split by update
// frequency was for
type drawContext struct {
	frame    renderer.Frame
	pass     renderer.Pass
	pipeline renderer.PipelineHandle
	push     [4]renderer.Address
}

// Uploads this draw's block and records the draw
func (ctx *drawContext) draw(mesh renderer.MeshHandle, uniforms *renderer.DrawUniforms) { // TODO: review
	push := ctx.push
	push[PushDraw] = ctx.frame.Upload(uniforms)
	ctx.pass.Draw(renderer.DrawCall{Pipeline: ctx.pipeline, Mesh: mesh, Push: push})
}
