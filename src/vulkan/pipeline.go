package vulkan

import (
	"fmt"
	"os"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/paths"
	"github.com/Zephyr75/overdrive/renderer"
)

// One pipeline object, and the spec it was built from so ReloadPipelines can
// rebuild it after the SPIR-V on disk changed
type pipelineInfo struct {
	spec      renderer.PipelineSpec
	pipeline  vk.Pipeline
	bindPoint vk.PipelineBindPoint
	valid     bool
}

// Builds a pipeline from its whole state: shaders, vertex layout, raster, depth,
// blend and the attachment formats it is compatible with
//
// Replaces the old lazy table that inferred winding, blending and sample count
// from what a target looked like — a pass's conventions are the pipeline's now
func (backend *VKBackend) CreatePipeline(spec renderer.PipelineSpec) (renderer.PipelineHandle, error) { // TODO: review
	entry := &pipelineInfo{spec: spec, valid: true}
	if err := backend.buildPipeline(entry); err != nil {
		return 0, err
	}
	backend.pipelines = append(backend.pipelines, entry)
	return renderer.PipelineHandle(len(backend.pipelines) - 1 + 1), nil
}

// Rebuilds every live pipeline, re-reading the SPIR-V modules first
//
// The old objects are retired rather than destroyed: a frame in flight may still
// reference them
func (backend *VKBackend) ReloadPipelines() error { // TODO: review
	_ = vk.DeviceWaitIdle(backend.device)
	for name, module := range backend.modules {
		vk.DestroyShaderModule(backend.device, module)
		delete(backend.modules, name)
	}
	for _, entry := range backend.pipelines {
		if !entry.valid {
			continue
		}
		old := entry.pipeline
		if err := backend.buildPipeline(entry); err != nil {
			entry.pipeline = old
			return err
		}
		vk.DestroyPipeline(backend.device, old)
	}
	return nil
}

// Creates the Vulkan pipeline an entry's spec describes, into e.pipeline
func (backend *VKBackend) buildPipeline(entry *pipelineInfo) error { // TODO: review
	spec := entry.spec
	stages := spec.Stages
	if len(stages) == 0 {
		if spec.Kind == renderer.PipelineCompute {
			stages = []renderer.ShaderStage{renderer.StageCompute}
		} else {
			stages = []renderer.ShaderStage{renderer.StageVertex, renderer.StageFragment}
		}
	}

	if spec.Kind == renderer.PipelineCompute {
		module, err := backend.module(spec.Shader, renderer.StageCompute)
		if err != nil {
			return err
		}
		pipeline, err := vk.CreateComputePipeline(backend.device, vk.ComputePipelineCreateInfo{
			Layout: backend.pipelineLayout,
			Stage: vk.PipelineShaderStageCreateInfo{
				Stage: vk.ShaderStageCompute, Module: module, Name: "main",
			},
		})
		if err != nil {
			return err
		}
		entry.pipeline, entry.bindPoint = pipeline, vk.PipelineBindPointCompute
		return nil
	}

	stageInfos := make([]vk.PipelineShaderStageCreateInfo, 0, len(stages))
	for _, stage := range stages {
		module, err := backend.module(spec.Shader, stage)
		if err != nil {
			return err
		}
		stageInfos = append(stageInfos, vk.PipelineShaderStageCreateInfo{
			Stage: shaderStage(stage), Module: module, Name: "main",
		})
	}

	colorFormats := make([]vk.Format, len(spec.ColorFormats))
	blends := make([]vk.PipelineColorBlendAttachmentState, len(spec.ColorFormats))
	for i, format := range spec.ColorFormats {
		colorFormats[i] = backend.format(format)
		blends[i] = blendAttachment(spec.Blend)
	}

	pipeline, err := vk.CreateGraphicsPipeline(backend.device, vk.GraphicsPipelineCreateInfo{
		Layout:             backend.pipelineLayout,
		Stages:             stageInfos,
		VertexInputState:   backend.vertexInput(spec.Vertex),
		InputAssemblyState: &vk.PipelineInputAssemblyStateCreateInfo{Topology: vk.PrimitiveTopologyTriangleList},
		ViewportState:      &vk.PipelineViewportStateCreateInfo{ViewportCount: 1, ScissorCount: 1},
		RasterizationState: &vk.PipelineRasterizationStateCreateInfo{
			PolygonMode: vk.PolygonModeFill,
			CullMode:    cullMode(spec.Cull),
			FrontFace:   frontFace(spec.FrontFace),
			LineWidth:   1,
		},
		MultisampleState: &vk.PipelineMultisampleStateCreateInfo{RasterizationSamples: sampleCount(spec.Samples)},
		DepthStencilState: &vk.PipelineDepthStencilStateCreateInfo{
			DepthTestEnable:  spec.DepthCompare != renderer.CompareNone,
			DepthWriteEnable: spec.DepthWrite,
			DepthCompareOp:   compareOp(spec.DepthCompare),
		},
		ColorBlendState: &vk.PipelineColorBlendStateCreateInfo{Attachments: blends},
		DynamicState: &vk.PipelineDynamicStateCreateInfo{
			DynamicStates: []vk.DynamicState{vk.DynamicStateViewport, vk.DynamicStateScissor},
		},
		// Under dynamic rendering this is what a pipeline is compatible with,
		// in place of pointing at a render-pass object
		Rendering: &vk.PipelineRenderingCreateInfo{
			DepthAttachmentFormat:  backend.format(spec.DepthFormat),
			ColorAttachmentFormats: colorFormats,
		},
	})
	if err != nil {
		return err
	}
	entry.pipeline, entry.bindPoint = pipeline, vk.PipelineBindPointGraphics
	return nil
}

// Builds the vertex input state a layout describes, or none when it has no
// attributes — a fullscreen pass generating its own positions has no stream
func (backend *VKBackend) vertexInput(layout renderer.VertexLayout) *vk.PipelineVertexInputStateCreateInfo { // TODO: review
	if len(layout.Attrs) == 0 {
		return &vk.PipelineVertexInputStateCreateInfo{}
	}
	rate := vk.VertexInputRateVertex
	if layout.Instanced {
		rate = vk.VertexInputRateInstance
	}
	attrs := make([]vk.VertexInputAttribute, len(layout.Attrs))
	for i, attr := range layout.Attrs {
		attrs[i] = vk.VertexInputAttribute{
			Location: uint32(attr.Location), Binding: 0,
			Format: backend.format(attr.Format), Offset: uint32(attr.Offset),
		}
	}
	return &vk.PipelineVertexInputStateCreateInfo{
		Bindings:   []vk.VertexInputBinding{{Binding: 0, Stride: uint32(layout.Stride), InputRate: rate}},
		Attributes: attrs,
	}
}

// Loads one precompiled SPIR-V stage, caching the module by set and stage
func (backend *VKBackend) module(name string, stage renderer.ShaderStage) (vk.ShaderModule, error) { // TODO: review
	key := name + "." + stage.Suffix()
	if module, ok := backend.modules[key]; ok {
		return module, nil
	}
	path := paths.Shader(key + ".spv")
	code, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read SPIR-V %s: %w (run ./build_shaders.sh)", path, err)
	}
	module, err := vk.CreateShaderModule(backend.device, code)
	if err != nil {
		return 0, err
	}
	backend.modules[key] = module
	return module, nil
}

// Resolves a pipeline handle, nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) pipeline(handle renderer.PipelineHandle) *pipelineInfo { // TODO: review
	if handle == 0 || int(handle) > len(backend.pipelines) || !backend.pipelines[handle-1].valid {
		return nil
	}
	return backend.pipelines[handle-1]
}
