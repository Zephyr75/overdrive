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
func (backend *VKBackend) CreatePipeline(spec renderer.PipelineSpec) (renderer.PipelineHandle, error) { 
	info := &pipelineInfo{spec: spec, valid: true}
	if err := backend.buildPipeline(info); err != nil {
		return 0, err
	}
	backend.pipelines = append(backend.pipelines, info)
	return renderer.PipelineHandle(len(backend.pipelines)), nil
}

// Rebuilds every live pipeline, re-reading the SPIR-V modules first
//
// The old objects are retired rather than destroyed: a frame in flight may still
// reference them
func (backend *VKBackend) ReloadPipelines() error { // TODO: review
	_ = vk.DeviceWaitIdle(backend.vkDevice)
	for name, module := range backend.vkShaderModules {
		vk.DestroyShaderModule(backend.vkDevice, module)
		delete(backend.vkShaderModules, name)
	}
	for _, info := range backend.pipelines {
		if !info.valid {
			continue
		}
		old := info.pipeline
		if err := backend.buildPipeline(info); err != nil {
			info.pipeline = old
			return err
		}
		vk.DestroyPipeline(backend.vkDevice, old)
	}
	return nil
}

// Create the Vulkan pipeline described by `info.spec`
func (backend *VKBackend) buildPipeline(info *pipelineInfo) error { 
    // Grab the spec that defines the pipeline layout, stages, etc.
    spec := info.spec
    stages := spec.Stages

    // If no explicit stages are supplied, infer them from the pipeline kind
    if len(stages) == 0 {
        if spec.Kind == renderer.PipelineCompute {
            stages = []renderer.ShaderStage{renderer.StageCompute}
        } else {
            stages = []renderer.ShaderStage{renderer.StageVertex, renderer.StageFragment}
        }
    }

	// ---- compute pipeline ------------------------------------------------
    if spec.Kind == renderer.PipelineCompute {
        // Load the compute shader module.
        module, err := backend.module(spec.Shader, renderer.StageCompute)
        if err != nil { return err }

        // Create the compute pipeline object.
        pipeline, err := vk.CreateComputePipeline(backend.vkDevice, vk.ComputePipelineCreateInfo{
            Layout: backend.vkPipelineLayout,
            Stage: vk.PipelineShaderStageCreateInfo{
                Stage: vk.ShaderStageCompute, Module: module, Name: "main",
            },
        })
        if err != nil { return err }

        info.pipeline, info.bindPoint = pipeline, vk.PipelineBindPointCompute
        return nil
    }

	// ---- graphics pipeline -----------------------------------------------
    // Assemble stage info
    stageInfos := make([]vk.PipelineShaderStageCreateInfo, 0, len(stages))
    for _, stage := range stages {
        // Load the shader module for the given stage.
        module, err := backend.module(spec.Shader, stage)
        if err != nil { return err }

        stageInfos = append(stageInfos, vk.PipelineShaderStageCreateInfo{
            Stage: toVkShaderStageFlags(stage), Module: module, Name: "main",
        })
    }

    // Convert output formats and blend state to Vulkan structures
    colorFormats := make([]vk.Format, len(spec.ColorFormats))
    blends := make([]vk.PipelineColorBlendAttachmentState, len(spec.ColorFormats))
    for i, format := range spec.ColorFormats {
        colorFormats[i] = backend.toVkFormat(format)
        blends[i] = toVkBlendAttachment(spec.Blend)
    }

    // Create the full graphics pipeline
    pipeline, err := vk.CreateGraphicsPipeline(backend.vkDevice, vk.GraphicsPipelineCreateInfo{
        Layout:             backend.vkPipelineLayout,
        Stages:             stageInfos,
        VertexInputState:   backend.vertexInput(spec.Vertex),
        InputAssemblyState: &vk.PipelineInputAssemblyStateCreateInfo{Topology: vk.PrimitiveTopologyTriangleList},
        ViewportState:      &vk.PipelineViewportStateCreateInfo{ViewportCount: 1, ScissorCount: 1},
        RasterizationState: &vk.PipelineRasterizationStateCreateInfo{
            PolygonMode: vk.PolygonModeFill,
            CullMode:    toVkCullModeFlags(spec.Cull),
            FrontFace:   toVkFrontFace(spec.FrontFace),
            LineWidth:   1,
        },
        MultisampleState: &vk.PipelineMultisampleStateCreateInfo{RasterizationSamples: toVkSampleCountFlags(spec.Samples)},
        DepthStencilState: &vk.PipelineDepthStencilStateCreateInfo{
            DepthTestEnable:  spec.DepthCompare != renderer.CompareNone,
            DepthWriteEnable: spec.DepthWrite,
            DepthCompareOp:   toVkCompareOp(spec.DepthCompare),
        },
        ColorBlendState: &vk.PipelineColorBlendStateCreateInfo{Attachments: blends},
        DynamicState: &vk.PipelineDynamicStateCreateInfo{
            DynamicStates: []vk.DynamicState{vk.DynamicStateViewport, vk.DynamicStateScissor},
        },
        // Use the rendering‑info instead of a render‑pass when using dynamic rendering.
        Rendering: &vk.PipelineRenderingCreateInfo{
            DepthAttachmentFormat:  backend.toVkFormat(spec.DepthFormat),
            ColorAttachmentFormats: colorFormats,
        },
    })
    if err != nil { return err }

    info.pipeline, info.bindPoint = pipeline, vk.PipelineBindPointGraphics
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
			Format: backend.toVkFormat(attr.Format), Offset: uint32(attr.Offset),
		}
	}
	return &vk.PipelineVertexInputStateCreateInfo{
		Bindings:   []vk.VertexInputBinding{{Binding: 0, Stride: uint32(layout.Stride), InputRate: rate}},
		Attributes: attrs,
	}
}

// Loads one precompiled SPIR-V stage, caching the module by set and stage
func (backend *VKBackend) module(name string, stage renderer.ShaderStage) (vk.ShaderModule, error) {
	key := name + "." + stage.Suffix()
	// If module is already loaded, return it
	if module, ok := backend.vkShaderModules[key]; ok {
		return module, nil
	}

	// Otherwise, read its code and load it
	path := paths.Shader(key + ".spv")
	code, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read SPIR-V %s: %w (run ./build_shaders.sh)", path, err)
	}
	module, err := vk.CreateShaderModule(backend.vkDevice, code)
	if err != nil {
		return 0, err
	}
	backend.vkShaderModules[key] = module
	return module, nil
}

// Resolves a pipeline handle, nil for 0, out-of-range or destroyed entries
func (backend *VKBackend) pipeline(handle renderer.PipelineHandle) *pipelineInfo { // TODO: review
	if handle == 0 || int(handle) > len(backend.pipelines) || !backend.pipelines[handle-1].valid {
		return nil
	}
	return backend.pipelines[handle-1]
}
