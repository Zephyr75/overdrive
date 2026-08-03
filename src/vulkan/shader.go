package vulkan

import (
	"fmt"
	"os"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// The compiled SPIR-V modules plus the pipelines built from them. Unlike
// OpenGL there is no single "program" object, because a pipeline bakes in the
// pass's attachment formats and the mesh's vertex layout, so one shader needs
// one pipeline per combination it is actually drawn with.
type shaderEntry struct {
	vert, frag, geo vk.ShaderModule
	pipelines       [passCount][layoutCount]vk.Pipeline
}

// Loads the SPIR-V modules of a shader set, no pipeline being built yet
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

	// Build no pipeline here, as which ones are needed depends on the passes
	// and meshes this shader is drawn with, so they are built lazily
	b.shaders = append(b.shaders, e)
	return renderer.ShaderHandle(len(b.shaders)), nil // handle 0 stays invalid
}

// Reads one precompiled SPIR-V stage from shaders/vk into a shader module
func (b *VKBackend) loadModule(name, stage string) (vk.ShaderModule, error) {
	path := fmt.Sprintf("shaders/vk/%s.%s.spv", name, stage)
	code, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read SPIR-V %s: %w (run ./build_shaders.sh)", path, err)
	}
	return vk.CreateShaderModule(b.device, code)
}

// Resolves a shader handle, returning nil for 0 or out-of-range entries
func (b *VKBackend) shader(h renderer.ShaderHandle) *shaderEntry {
	if h == 0 || int(h) > len(b.shaders) {
		return nil
	}
	return &b.shaders[h-1]
}

// Returns the pipeline for this (shader, pass, layout), building it on first use
func (b *VKBackend) getPipeline(s *shaderEntry, pass passKind, layout renderer.VertexLayout) vk.Pipeline {
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
			// front face survives there. Shadow passes use a positive viewport
			// and therefore need CW
			FrontFace: frontFace(pass),
			LineWidth: 1,
		},
		MultisampleState: &vk.PipelineMultisampleStateCreateInfo{RasterizationSamples: vk.SampleCount1Bit},
		DepthStencilState: &vk.PipelineDepthStencilStateCreateInfo{
			// An offscreen colour pass has no depth attachment to test against
			DepthTestEnable: pass != passOffscreenColor,
			// Let the UI overlay test but not write, as it composites over the
			// finished scene from the near plane
			DepthWriteEnable: layout != renderer.LayoutPositionUV,
			DepthCompareOp:   vk.CompareOpLess, // dynamic, so this is only the default
		},
		ColorBlendState: colorBlendState(pass),
		DynamicState: &vk.PipelineDynamicStateCreateInfo{
			DynamicStates: []vk.DynamicState{
				vk.DynamicStateViewport, vk.DynamicStateScissor,
				vk.DynamicStateCullMode, vk.DynamicStateDepthCompareOp,
			},
		},
		// Declare the attachment formats this pipeline will be used with, which
		// under dynamic rendering replaces pointing at a render-pass object
		Rendering: renderingInfo(pass, b.swapFormat),
	}

	p, err := vk.CreateGraphicsPipeline(b.device, ci)
	fatal(err, "create graphics pipeline")
	s.pipelines[pass][layout] = p
	return p
}

// Builds the vertex input state for a layout, dropping the attributes a depth-only pass never reads
func vertexInputState(pass passKind, layout renderer.VertexLayout) *vk.PipelineVertexInputStateCreateInfo {
	if layout == renderer.LayoutPositionUV {
		// Describe the UI quad, clip-space position(3) | uv(2), 20-byte stride
		return &vk.PipelineVertexInputStateCreateInfo{
			Bindings: []vk.VertexInputBinding{{Binding: 0, Stride: 5 * 4, InputRate: vk.VertexInputRateVertex}},
			Attributes: []vk.VertexInputAttribute{
				{Location: 0, Binding: 0, Format: vk.FormatR32G32B32Sfloat, Offset: 0},
				{Location: 1, Binding: 0, Format: vk.FormatR32G32Sfloat, Offset: 3 * 4},
			},
		}
	}

	stride := uint32(8 * 4)
	if layout == renderer.LayoutPosition {
		stride = 3 * 4
	}
	attrs := []vk.VertexInputAttribute{
		{Location: 0, Binding: 0, Format: vk.FormatR32G32B32Sfloat, Offset: 0},
	}
	// Add normals and UVs for the passes with a colour attachment, the
	// depth-only shaders taking position alone and declaring unread attributes
	// being rejected
	if layout == renderer.LayoutMesh && (pass == passMain || pass == passOffscreenColor) {
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

// Picks the winding a pass treats as front-facing
//
// The passes that flip the viewport (main and offscreen colour) keep GL's CCW,
// because the flip cancels Vulkan's winding inversion. The shadow passes use a
// positive viewport to match GL's shadow-map memory layout, and pay for it here.
func frontFace(pass passKind) vk.FrontFace {
	if pass == passMain || pass == passOffscreenColor {
		return vk.FrontFaceCounterClockwise
	}
	return vk.FrontFaceClockwise
}

// Builds the alpha-blend state of the passes that own a color attachment, the shadow passes having none to blend into
func colorBlendState(pass passKind) *vk.PipelineColorBlendStateCreateInfo {
	if pass != passMain && pass != passOffscreenColor {
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

// Declares the attachment formats of a pass, which under dynamic rendering is what a pipeline is compatible with
//
// Shadow passes are depth-only; an offscreen colour pass is colour-only, since
// post-processing reads a finished image and needs no depth test.
func renderingInfo(pass passKind, swapFormat vk.Format) *vk.PipelineRenderingCreateInfo {
	switch pass {
	case passMain:
		return &vk.PipelineRenderingCreateInfo{
			DepthAttachmentFormat:  depthFormat,
			ColorAttachmentFormats: []vk.Format{swapFormat},
		}
	case passOffscreenColor:
		return &vk.PipelineRenderingCreateInfo{
			ColorAttachmentFormats: []vk.Format{offscreenColorFormat},
		}
	default:
		return &vk.PipelineRenderingCreateInfo{DepthAttachmentFormat: depthFormat}
	}
}
