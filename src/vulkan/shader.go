package vulkan

import (
	"fmt"
	"os"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// shaderEntry holds the compiled SPIR-V modules plus the pipelines built from
// them. Unlike OpenGL there is no single "program" object: a pipeline bakes in
// the pass's attachment formats and the mesh's vertex layout, so one shader
// needs one pipeline per combination it is actually drawn with.
type shaderEntry struct {
	vert, frag, geo vk.ShaderModule
	pipelines       [passCount][layoutCount]vk.Pipeline
}

func (b *VKBackend) CreateShader(name string, hasGeometry bool) (renderer.ShaderHandle, error) {
	var e shaderEntry
	var err error
	if e.vert, err = b.loadModule(name, "vert"); err != nil {
		return 0, err
	}
	if e.frag, err = b.loadModule(name, "frag"); err != nil {
		return 0, err
	}
	if hasGeometry {
		if e.geo, err = b.loadModule(name, "geo"); err != nil {
			return 0, err
		}
	}

	// No pipeline is built here: which ones are needed depends on the passes
	// and meshes this shader is drawn with, so they are built lazily.
	b.shaders = append(b.shaders, e)
	return renderer.ShaderHandle(len(b.shaders)), nil // handle 0 stays invalid
}

func (b *VKBackend) loadModule(name, stage string) (vk.ShaderModule, error) {
	path := fmt.Sprintf("shaders/vk/%s.%s.spv", name, stage)
	code, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read SPIR-V %s: %w (run ./build_shaders.sh)", path, err)
	}
	return vk.CreateShaderModule(b.device, code)
}

func (b *VKBackend) shader(h renderer.ShaderHandle) *shaderEntry {
	if h == 0 || int(h) > len(b.shaders) {
		return nil
	}
	return &b.shaders[h-1]
}

// getPipeline returns the pipeline for this (shader, pass, layout), building it
// on first use.
func (b *VKBackend) getPipeline(s *shaderEntry, pass passKind, layout vertexLayout) vk.Pipeline {
	if p := s.pipelines[pass][layout]; p != 0 {
		return p
	}

	stages := []vk.PipelineShaderStageCreateInfo{
		{Stage: vk.ShaderStageVertex, Module: s.vert, Name: "main"},
	}
	if s.geo != 0 {
		stages = append(stages, vk.PipelineShaderStageCreateInfo{
			Stage: vk.ShaderStageGeometry, Module: s.geo, Name: "main",
		})
	}
	stages = append(stages, vk.PipelineShaderStageCreateInfo{
		Stage: vk.ShaderStageFragment, Module: s.frag, Name: "main",
	})

	ci := vk.GraphicsPipelineCreateInfo{
		Layout:             b.pipelineLayout,
		Stages:             stages,
		VertexInputState:   vertexInputState(pass, layout),
		InputAssemblyState: &vk.PipelineInputAssemblyStateCreateInfo{Topology: vk.PrimitiveTopologyTriangleList},
		ViewportState:      &vk.PipelineViewportStateCreateInfo{ViewportCount: 1, ScissorCount: 1},
		RasterizationState: &vk.PipelineRasterizationStateCreateInfo{
			PolygonMode: vk.PolygonModeFill,
			// Vulkan's y-down framebuffer flips winding relative to OpenGL. The
			// main pass's negative-height viewport flips it back, so GL's CCW
			// front face survives there; shadow passes use a positive viewport
			// and therefore need CW.
			FrontFace: frontFace(pass),
			LineWidth: 1,
		},
		MultisampleState: &vk.PipelineMultisampleStateCreateInfo{RasterizationSamples: vk.SampleCount1Bit},
		DepthStencilState: &vk.PipelineDepthStencilStateCreateInfo{
			DepthTestEnable: true,
			// The UI overlay composites over the finished scene, so it tests
			// (its triangle sits on the near plane) but must not write.
			DepthWriteEnable: layout != layoutFullscreen,
			DepthCompareOp:   vk.CompareOpLess, // dynamic; this is only the default
		},
		ColorBlendState: colorBlendState(pass),
		DynamicState: &vk.PipelineDynamicStateCreateInfo{
			DynamicStates: []vk.DynamicState{
				vk.DynamicStateViewport, vk.DynamicStateScissor,
				vk.DynamicStateCullMode, vk.DynamicStateDepthCompareOp,
			},
		},
		// Dynamic rendering: the pipeline declares the attachment formats it
		// will be used with instead of pointing at a render-pass object.
		Rendering: renderingInfo(pass, b.swapFormat),
	}

	p, err := vk.CreateGraphicsPipeline(b.device, ci)
	fatal(err, "create graphics pipeline")
	s.pipelines[pass][layout] = p
	return p
}

func vertexInputState(pass passKind, layout vertexLayout) *vk.PipelineVertexInputStateCreateInfo {
	if layout == layoutFullscreen {
		// The UI quad: clip-space position(3) | uv(2), 20-byte stride.
		return &vk.PipelineVertexInputStateCreateInfo{
			Bindings: []vk.VertexInputBinding{{Binding: 0, Stride: 5 * 4, InputRate: vk.VertexInputRateVertex}},
			Attributes: []vk.VertexInputAttribute{
				{Location: 0, Binding: 0, Format: vk.FormatR32G32B32Sfloat, Offset: 0},
				{Location: 1, Binding: 0, Format: vk.FormatR32G32Sfloat, Offset: 3 * 4},
			},
		}
	}

	stride := uint32(8 * 4)
	if layout == layoutSkybox {
		stride = 3 * 4
	}
	attrs := []vk.VertexInputAttribute{
		{Location: 0, Binding: 0, Format: vk.FormatR32G32B32Sfloat, Offset: 0},
	}
	// Only the main pass consumes normals and UVs; the depth-only shaders take
	// position alone, and declaring unread attributes would be rejected.
	if layout == layoutMesh && pass == passMain {
		attrs = append(attrs,
			vk.VertexInputAttribute{Location: 1, Binding: 0, Format: vk.FormatR32G32B32Sfloat, Offset: 3 * 4},
			vk.VertexInputAttribute{Location: 2, Binding: 0, Format: vk.FormatR32G32Sfloat, Offset: 6 * 4},
		)
	}
	return &vk.PipelineVertexInputStateCreateInfo{
		Bindings:   []vk.VertexInputBinding{{Binding: 0, Stride: stride, InputRate: vk.VertexInputRateVertex}},
		Attributes: attrs,
	}
}

func frontFace(pass passKind) vk.FrontFace {
	if pass == passMain {
		return vk.FrontFaceCounterClockwise
	}
	return vk.FrontFaceClockwise
}

// Shadow passes have no color attachment, so they get no blend state.
func colorBlendState(pass passKind) *vk.PipelineColorBlendStateCreateInfo {
	if pass != passMain {
		return &vk.PipelineColorBlendStateCreateInfo{}
	}
	return &vk.PipelineColorBlendStateCreateInfo{
		Attachments: []vk.PipelineColorBlendAttachmentState{{
			BlendEnable:         true,
			SrcColorBlendFactor: vk.BlendFactorSrcAlpha,
			DstColorBlendFactor: vk.BlendFactorOneMinusSrcAlpha,
			ColorBlendOp:        vk.BlendOpAdd,
			SrcAlphaBlendFactor: vk.BlendFactorOne,
			DstAlphaBlendFactor: vk.BlendFactorZero,
			AlphaBlendOp:        vk.BlendOpAdd,
			ColorWriteMask:      0xF,
		}},
	}
}

func renderingInfo(pass passKind, swapFormat vk.Format) *vk.PipelineRenderingCreateInfo {
	info := &vk.PipelineRenderingCreateInfo{DepthAttachmentFormat: depthFormat}
	if pass == passMain {
		info.ColorAttachmentFormats = []vk.Format{swapFormat}
	}
	return info
}
