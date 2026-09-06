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

// Builds them, given the sample count the backbuffer rasterises at
func NewPipelines(backend renderer.Backend) (Pipelines, error) {
	samples := backend.Capacities().BackbufferSamples
	var pipes Pipelines
	var err error

	// Shade only the fragments the prepass left, so an overdrawn pixel runs the
	// light loop once rather than once per surface stacked behind it. EQUAL
	// rejects a last-bit difference, which is why prepass.slang combines the
	// same matrices in the same order forward.slang does
	forwardCompare := renderer.CompareLess
	if settings.DepthPrepass {
		forwardCompare = renderer.CompareEqual
	}

	// The screen passes flip the viewport, which makes clip space y-up and
	// inverts winding with it, so counter-clockwise stays front-facing
	if pipes.Forward, err = backend.CreatePipeline(renderer.PipelineSpec{
		Name: "forward", Shader: "forward", Vertex: meshLayout,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: forwardCompare, DepthWrite: true, Blend: renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatBackbufferDepth, Samples: samples,
	}); err != nil {
		return pipes, fmt.Errorf("forward pipeline: %w", err)
	}

	// Ties pass, so the cube can sit exactly on the far plane
	if pipes.Skybox, err = backend.CreatePipeline(renderer.PipelineSpec{
		Name: "skybox", Shader: "skybox", Vertex: skyboxLayout,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: renderer.CompareLessEqual, DepthWrite: true, Blend: renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatBackbufferDepth, Samples: samples,
	}); err != nil {
		return pipes, fmt.Errorf("skybox pipeline: %w", err)
	}

	if pipes.Prepass, err = backend.CreatePipeline(renderer.PipelineSpec{
		Name: "prepass", Shader: "prepass", Vertex: positionOnly,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: renderer.CompareLess, DepthWrite: true,
		DepthFormat: renderer.FormatBackbufferDepth, Samples: samples,
	}); err != nil {
		return pipes, fmt.Errorf("prepass pipeline: %w", err)
	}

	// The atlas passes keep a positive viewport and pay for it here: clockwise
	// is what a y-down clip space makes front-facing.
	//
	// Back-face culling, the scene default: front-face culling would bake the
	// far side of a closed mesh and float a sphere above a lit disc of its own
	// size (notes/FEATURES.md)
	shadow := renderer.PipelineSpec{
		Vertex: positionOnly,
		Cull:   renderer.CullBack, FrontFace: renderer.WindingClockwise,
		DepthCompare: renderer.CompareLess, DepthWrite: true,
		DepthFormat: renderer.FormatDepth32F, Samples: 1,
	}
	shadow.Name, shadow.Shader = "depth", "depth"
	if pipes.Depth, err = backend.CreatePipeline(shadow); err != nil {
		return pipes, fmt.Errorf("depth pipeline: %w", err)
	}
	// A face tile stores radial distance, which needs the fragment stage
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
func (ctx *drawContext) draw(mesh renderer.MeshHandle, uniforms *renderer.DrawUniforms) {
	push := ctx.push
	push[PushDraw] = ctx.frame.Upload(uniforms)
	ctx.pass.Draw(renderer.DrawCall{Pipeline: ctx.pipeline, Mesh: mesh, Push: push})
}
