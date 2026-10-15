package scene

import (
	"fmt"

	"github.com/Zephyr75/overdrive/renderer"
	"github.com/Zephyr75/overdrive/settings"
)

// The blocks a draw's shaders can reach. The backend pushes four opaque words;
// this struct and words() are the only thing that gives them meaning
type pushBlocks struct {
	// Camera and lights, shared by the whole pass
	frame renderer.Address
	// This object's position and material
	draw renderer.Address
	// The list of shadow tiles the lights sample
	records renderer.Address
	// The one shadow tile being baked right now
	bake renderer.Address
}

// The four addresses the backend pushes, in the order of PushConstants in
// shaders/slang/common.slang
func (blocks pushBlocks) toAddressArray() [4]renderer.Address {
	return [4]renderer.Address{blocks.frame, blocks.draw, blocks.records, blocks.bake}
}

// The push addresses of a draw that reads only its draw block, for the UI overlay
func DrawOnlyAddressArray(draw renderer.Address) [4]renderer.Address {
	return pushBlocks{draw: draw}.toAddressArray()
}

// The push addresses of a full-screen pass that reads only the frame block
func FrameOnlyAddressArray(frame renderer.Address) [4]renderer.Address {
	return pushBlocks{frame: frame}.toAddressArray()
}

// Bytes per vertex of each stream the engine draws
const (
	// position(3) | normal(3) | uv(2)
	meshStride = 8 * 4
	// position(3): the skybox cube
	positionStride = 3 * 4
	// unit-quad position(3) | uv(2): the UI overlay
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
	// A cutout depth pipeline: the uv too, for the alpha map
	positionUV = renderer.VertexLayout{
		Stride: meshStride,
		Attrs: []renderer.VertexAttr{
			{Location: 0, Format: renderer.FormatRGB32F, Offset: 0},
			{Location: 1, Format: renderer.FormatRG32F, Offset: 6 * 4},
		},
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
//
// Each pass that draws meshes has a cutout twin for materials with an alpha
// map, kept apart because a fragment stage that can discard loses its early
// depth test, which the opaque draws should not pay for
type Pipelines struct {
	Forward          renderer.PipelineHandle
	Skybox           renderer.PipelineHandle
	Depth            renderer.PipelineHandle
	DepthPoint       renderer.PipelineHandle
	Prepass          renderer.PipelineHandle
	ForwardCutout    renderer.PipelineHandle
	PrepassCutout    renderer.PipelineHandle
	DepthCutout      renderer.PipelineHandle
	DepthPointCutout renderer.PipelineHandle
	// Translucent fragments, after everything opaque in the main pass
	Transparent renderer.PipelineHandle
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
	if !settings.Current.Debug.NoPrepass {
		forwardCompare = renderer.CompareEqual
	}

	// ---- forward pass ----------------------------------------------------
	// Renders the world geometry with lighting. Uses the same depth comparison
	// as the pre‑pass unless [debug] noPrepass is set, otherwise it writes a normal
	// depth value.
	forward := renderer.PipelineSpec{
		Name: "forward", Shader: "forward", Vertex: meshLayout,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: forwardCompare, DepthWrite: true, Blend: renderer.BlendAlpha,
		ColorFormats: []renderer.Format{renderer.FormatBackbuffer},
		DepthFormat:  renderer.FormatDepth32F,
		Samples:      samples,
	}
	if pipes.Forward, err = backend.CreatePipeline(forward); err != nil {
		return pipes, fmt.Errorf("forward pipeline: %w", err)
	}
	// Drawn only under [debug] noPrepass, when nothing before it cut the texels out
	forward.Name, forward.Shader = "forwardCutout", "forward_cutout"
	if pipes.ForwardCutout, err = backend.CreatePipeline(forward); err != nil {
		return pipes, fmt.Errorf("forward_cutout pipeline: %w", err)
	}
	// Tested against the opaque depth but never writing it, so a surface behind
	// a transparent one is still drawn, and LESS since no prepass drew it
	forward.Name, forward.Shader = "transparent", "transparent"
	forward.DepthCompare, forward.DepthWrite = renderer.CompareLess, false
	if pipes.Transparent, err = backend.CreatePipeline(forward); err != nil {
		return pipes, fmt.Errorf("transparent pipeline: %w", err)
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
	prepass := renderer.PipelineSpec{
		Name: "prepass", Shader: "prepass", Vertex: positionOnly,
		Cull: renderer.CullBack, FrontFace: renderer.WindingCounterClockwise,
		DepthCompare: renderer.CompareLess, DepthWrite: true,
		DepthFormat: renderer.FormatDepth32F, Samples: samples,
	}
	if pipes.Prepass, err = backend.CreatePipeline(prepass); err != nil {
		return pipes, fmt.Errorf("prepass pipeline: %w", err)
	}
	prepass.Name, prepass.Shader, prepass.Vertex = "prepassCutout", "prepass_cutout", positionUV
	if pipes.PrepassCutout, err = backend.CreatePipeline(prepass); err != nil {
		return pipes, fmt.Errorf("prepass_cutout pipeline: %w", err)
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

	// The two again for alpha-mapped casters, which read the uv
	shadow.Vertex = positionUV
	shadow.Name, shadow.Shader = "depthCutout", "depth_cutout"
	if pipes.DepthCutout, err = backend.CreatePipeline(shadow); err != nil {
		return pipes, fmt.Errorf("depth_cutout pipeline: %w", err)
	}
	shadow.Name, shadow.Shader = "depthPointCutout", "depth_point_cutout"
	if pipes.DepthPointCutout, err = backend.CreatePipeline(shadow); err != nil {
		return pipes, fmt.Errorf("depth_point_cutout pipeline: %w", err)
	}

	return pipes, nil
}

// What a run of draws shares: the pass they record into, the pipeline they use,
// and the three block addresses that do not change between them
//
// Only the draw block is uploaded per draw, which is what the split by update
// frequency was for
type drawContext struct {
	// The frame being recorded, used to upload each draw's data
	frame renderer.Frame
	// The pass the draws go into
	pass renderer.Pass
	// The shader setup every draw uses
	pipeline renderer.PipelineHandle
	// The same for a material cut out by an alpha map, which may be pipeline itself
	cutout renderer.PipelineHandle
	// The template note: shared blocks filled once, the draw block per draw
	push pushBlocks
}

// Uploads this draw's block and records the draw with one of the context's pipelines
func (ctx *drawContext) draw(pipeline renderer.PipelineHandle, mesh renderer.MeshHandle, uniforms *renderer.DrawUniforms) {
	push := ctx.push
	push.draw = ctx.frame.Upload(uniforms)
	ctx.pass.Draw(renderer.DrawCall{Pipeline: pipeline, Mesh: mesh, Push: push.toAddressArray()})
}
