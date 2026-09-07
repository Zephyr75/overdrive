package vulkan

import (
	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// The engine's enums translated into Vulkan's. One file, so a question about
// what a spec value becomes has one place to look.

// The Vulkan format an engine format names, the two backend-chosen entries
// resolving to what the swapchain and the depth buffer were actually created
// with
func (backend *VKBackend) format(format renderer.Format) vk.Format { // TODO: review
	switch format {
	case renderer.FormatNone:
		return vk.FormatUndefined
	case renderer.FormatRGBA8:
		return vk.FormatR8G8B8A8Unorm
	case renderer.FormatRGBA8Srgb:
		return vk.FormatR8G8B8A8Srgb
	case renderer.FormatBGRA8:
		return vk.FormatB8G8R8A8Unorm
	case renderer.FormatR8:
		return vk.FormatR8Unorm
	case renderer.FormatR16F:
		return vk.FormatR16Sfloat
	case renderer.FormatRG16F:
		return vk.FormatR16G16Sfloat
	case renderer.FormatRGBA16F:
		return vk.FormatR16G16B16A16Sfloat
	case renderer.FormatR11G11B10F:
		return vk.FormatR11G11B10UfloatPack32
	case renderer.FormatR32U:
		return vk.FormatR32Uint
	case renderer.FormatR32F:
		return vk.FormatR32Sfloat
	case renderer.FormatRG32F:
		return vk.FormatR32G32Sfloat
	case renderer.FormatRGB32F:
		return vk.FormatR32G32B32Sfloat
	case renderer.FormatRGBA32F:
		return vk.FormatR32G32B32A32Sfloat
	case renderer.FormatDepth32F, renderer.FormatBackbufferDepth:
		return depthFormat
	case renderer.FormatBC5:
		return vk.FormatBC5UnormBlock
	case renderer.FormatBC6H:
		return vk.FormatBC6HUfloatBlock
	case renderer.FormatBC7:
		return vk.FormatBC7UnormBlock
	case renderer.FormatBC7Srgb:
		return vk.FormatBC7SrgbBlock
	case renderer.FormatBackbuffer:
		return backend.swapFormat
	}
	return vk.FormatUndefined
}

func imageUsage(usage renderer.ImageUsage) vk.ImageUsageFlags { // TODO: review
	var out vk.ImageUsageFlags
	if usage&renderer.ImageSampled != 0 {
		out |= vk.ImageUsageSampled
	}
	if usage&renderer.ImageStorage != 0 {
		out |= vk.ImageUsageStorage
	}
	if usage&renderer.ImageColorAttachment != 0 {
		out |= vk.ImageUsageColorAttachment
	}
	if usage&renderer.ImageDepthAttachment != 0 {
		out |= vk.ImageUsageDepthStencilAttachment
	}
	if usage&renderer.ImageCopySrc != 0 {
		out |= vk.ImageUsageTransferSrc
	}
	if usage&renderer.ImageCopyDst != 0 {
		out |= vk.ImageUsageTransferDst
	}
	return out
}

func bufferUsage(usage renderer.BufferUsage) vk.BufferUsageFlags { // TODO: review
	var out vk.BufferUsageFlags
	if usage&renderer.BufferVertex != 0 {
		out |= vk.BufferUsageVertexBuffer
	}
	if usage&renderer.BufferIndex != 0 {
		out |= vk.BufferUsageIndexBuffer
	}
	if usage&renderer.BufferStorage != 0 {
		out |= vk.BufferUsageStorageBuffer
	}
	if usage&renderer.BufferIndirect != 0 {
		out |= vk.BufferUsageIndirectBuffer
	}
	if usage&renderer.BufferCopySrc != 0 {
		out |= vk.BufferUsageTransferSrc
	}
	if usage&renderer.BufferCopyDst != 0 {
		out |= vk.BufferUsageTransferDst
	}
	return out
}

func sampleCount(n int) vk.SampleCountFlags { // TODO: review
	switch {
	case n >= 8:
		return vk.SampleCount8Bit
	case n >= 4:
		return vk.SampleCount4Bit
	case n >= 2:
		return vk.SampleCount2Bit
	}
	return vk.SampleCount1Bit
}

func samplesToInt(samples vk.SampleCountFlags) int { // TODO: review
	switch samples {
	case vk.SampleCount8Bit:
		return 8
	case vk.SampleCount4Bit:
		return 4
	case vk.SampleCount2Bit:
		return 2
	}
	return 1
}

func viewType(kind renderer.ImageKind, layers uint32) vk.ImageViewType { // TODO: review
	switch kind {
	case renderer.Image2DArray:
		return vk.ImageViewType2DArray
	case renderer.ImageCube:
		if layers > 6 {
			return vk.ImageViewTypeCubeArray
		}
		return vk.ImageViewTypeCube
	case renderer.Image3D:
		return vk.ImageViewType3D
	}
	return vk.ImageViewType2D
}

func aspectOf(aspect vk.ImageAspectFlags) renderer.Aspect { // TODO: review
	if aspect == vk.ImageAspectDepth {
		return renderer.AspectDepth
	}
	return renderer.AspectColor
}

func cullMode(mode renderer.CullMode) vk.CullModeFlags { // TODO: review
	switch mode {
	case renderer.CullFront:
		return vk.CullModeFront
	case renderer.CullNone:
		return vk.CullModeNone
	}
	return vk.CullModeBack
}

func frontFace(winding renderer.WindingDirection) vk.FrontFace { // TODO: review
	if winding == renderer.WindingClockwise {
		return vk.FrontFaceClockwise
	}
	return vk.FrontFaceCounterClockwise
}

func compareOp(op renderer.CompareOperation) vk.CompareOp { // TODO: review
	switch op {
	case renderer.CompareNever:
		return vk.CompareOpNever
	case renderer.CompareEqual:
		return vk.CompareOpEqual
	case renderer.CompareLessEqual:
		return vk.CompareOpLessOrEqual
	case renderer.CompareGreater:
		return vk.CompareOpGreater
	case renderer.CompareNotEqual:
		return vk.CompareOpNotEqual
	case renderer.CompareGreaterEqual:
		return vk.CompareOpGreaterOrEqual
	case renderer.CompareAlways:
		return vk.CompareOpAlways
	}
	return vk.CompareOpLess
}

func filter(filterMode renderer.FilterType) vk.Filter { // TODO: review
	if filterMode == renderer.FilterNearest {
		return vk.FilterNearest
	}
	return vk.FilterLinear
}

func mipmapMode(filterMode renderer.FilterType) vk.SamplerMipmapMode { // TODO: review
	if filterMode == renderer.FilterNearest {
		return vk.SamplerMipmapModeNearest
	}
	return vk.SamplerMipmapModeLinear
}

func addressMode(mode renderer.OutsideMode) vk.SamplerAddressMode { // TODO: review
	switch mode {
	case renderer.OutsideMirroredRepeat:
		return vk.SamplerAddressModeMirroredRepeat
	case renderer.OutsideClampToEdge:
		return vk.SamplerAddressModeClampToEdge
	case renderer.OutsideClampToBorder:
		return vk.SamplerAddressModeClampToBorder
	}
	return vk.SamplerAddressModeRepeat
}

func borderColor(color renderer.BorderColor) vk.BorderColor { // TODO: review
	if color == renderer.BorderWhite {
		return vk.BorderColorOpaqueWhiteFloat
	}
	return vk.BorderColorOpaqueBlackFloat
}

func shaderStage(stage renderer.ShaderStage) vk.ShaderStageFlags { // TODO: review
	switch stage {
	case renderer.StageFragment:
		return vk.ShaderStageFragment
	case renderer.StageGeometry:
		return vk.ShaderStageGeometry
	case renderer.StageCompute:
		return vk.ShaderStageCompute
	}
	return vk.ShaderStageVertex
}

// The blend state of one colour attachment
func blendAttachment(mode renderer.BlendMode) vk.PipelineColorBlendAttachmentState { // TODO: review
	att := vk.PipelineColorBlendAttachmentState{
		ColorWriteMask: vk.ColorComponentR | vk.ColorComponentG | vk.ColorComponentB | vk.ColorComponentA,
	}
	switch mode {
	case renderer.BlendAlpha:
		att.BlendEnable = true
		att.SrcColorBlendFactor = vk.BlendFactorSrcAlpha
		att.DstColorBlendFactor = vk.BlendFactorOneMinusSrcAlpha
		att.ColorBlendOp = vk.BlendOpAdd
		att.SrcAlphaBlendFactor = vk.BlendFactorOne
		att.DstAlphaBlendFactor = vk.BlendFactorZero
		att.AlphaBlendOp = vk.BlendOpAdd
	case renderer.BlendAdd:
		att.BlendEnable = true
		att.SrcColorBlendFactor = vk.BlendFactorOne
		att.DstColorBlendFactor = vk.BlendFactorOne
		att.ColorBlendOp = vk.BlendOpAdd
		att.SrcAlphaBlendFactor = vk.BlendFactorOne
		att.DstAlphaBlendFactor = vk.BlendFactorOne
		att.AlphaBlendOp = vk.BlendOpAdd
	}
	return att
}

// --- samplers ----------------------------------------------------------------

// Creates a sampler from its whole state, the caller deciding filtering,
// wrapping, border and whether it compares
func (backend *VKBackend) CreateSampler(spec renderer.SamplerSpec) renderer.SamplerHandle { // TODO: review
	aniso := spec.MaxAnisotropy
	if limit := backend.physicalDeviceProperties.MaxSamplerAnisotropy; aniso > limit {
		// Lower a request the device cannot meet: the config file is written
		// once, the GPU it runs on is not
		aniso = limit
	}
	sampler, err := vk.CreateSampler(backend.device, vk.SamplerCreateInfo{
		MagFilter: filter(spec.Mag), MinFilter: filter(spec.Min),
		MipmapMode:       mipmapMode(spec.Mipmap),
		AddressModeU:     addressMode(spec.OutsideU),
		AddressModeV:     addressMode(spec.OutsideV),
		AddressModeW:     addressMode(spec.OutsideW),
		AnisotropyEnable: aniso > 1,
		MaxAnisotropy:    aniso,
		MinLod:           spec.MinLod,
		MaxLod:           spec.MaxLod,
		BorderColor:      borderColor(spec.Border),
		CompareEnable:    spec.Compare != renderer.CompareNone,
		CompareOp:        compareOp(spec.Compare),
	})
	fatal(err, "create sampler "+spec.Name)
	backend.samplers = append(backend.samplers, sampler)
	return renderer.SamplerHandle(len(backend.samplers) - 1)
}

// The Vulkan sampler a handle names, handle 0 being the built-in repeat sampler
func (backend *VKBackend) samplerOf(handle renderer.SamplerHandle) vk.Sampler { // TODO: review
	if int(handle) >= len(backend.samplers) {
		return backend.samplers[0]
	}
	return backend.samplers[handle]
}
