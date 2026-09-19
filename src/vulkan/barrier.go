package vulkan

import (
	"go-vulkan/vk"
)

// What a resource is about to be used for. Every operation declares one, the
// backend remembers the last, and the table below turns the pair into a
// barrier — which is the whole of the synchronisation in this package.
type use int

const (
	useNone use = iota
	useSampled
	// A buffer read through a device address: no layout, every shader stage
	useShaderRead
	useColorAttach
	useDepthAttach
	useCopySrc
	useCopyDst
	useStorage
	useIndirect
	usePresent
)

// The layout, stage and access one use implies
//
// The layout column applies to images only: a buffer has no layout, so a buffer
// barrier reads the other two columns and ignores it.
type useInfo struct {
	layout vk.ImageLayout
	stage  vk.PipelineStageFlags2
	access vk.AccessFlags2
	// Whether this use writes, which is what makes a same-use transition still
	// need a barrier: two consecutive reads do not, two writes do
	write bool
}

var useTable = [...]useInfo{
	useNone:        {vk.ImageLayoutUndefined, vk.PipelineStage2None, vk.Access2None, false},
	useSampled:     {vk.ImageLayoutShaderReadOnlyOptimal, vk.PipelineStage2FragmentShader | vk.PipelineStage2ComputeShader, vk.Access2ShaderSampledRead, false},
	useShaderRead:  {vk.ImageLayoutShaderReadOnlyOptimal, vk.PipelineStage2VertexShader | vk.PipelineStage2FragmentShader | vk.PipelineStage2ComputeShader, vk.Access2ShaderRead | vk.Access2ShaderStorageRead, false},
	useColorAttach: {vk.ImageLayoutColorAttachmentOptimal, vk.PipelineStage2ColorAttachmentOutput, vk.Access2ColorAttachmentWrite, true},
	useDepthAttach: {vk.ImageLayoutDepthAttachmentOptimal, vk.PipelineStage2EarlyFragmentTests | vk.PipelineStage2LateFragmentTests, vk.Access2DepthStencilAttachmentWrite, true},
	useCopySrc:     {vk.ImageLayoutTransferSrcOptimal, vk.PipelineStage2Transfer, vk.Access2TransferRead, false},
	useCopyDst:     {vk.ImageLayoutTransferDstOptimal, vk.PipelineStage2Transfer, vk.Access2TransferWrite, true},
	useStorage:     {vk.ImageLayoutGeneral, vk.PipelineStage2ComputeShader, vk.Access2ShaderStorageRead | vk.Access2ShaderStorageWrite, true},
	useIndirect:    {vk.ImageLayoutUndefined, vk.PipelineStage2DrawIndirect, vk.Access2IndirectCommandRead, false},
	usePresent:     {vk.ImageLayoutPresentSrcKHR, vk.PipelineStage2None, vk.Access2None, false},
}

// Transitions a whole image from current use to a new use
func (backend *VKBackend) useImage(commandBuffer vk.CommandBuffer, image *image, want use) {
	if image == nil || image.vkImage == 0 {
		return
	}
	from, to := useTable[image.use], useTable[want]
	// Avoid useless barrier
	if image.use == want && !to.write {
		return
	}
	vk.CmdPipelineBarrier2(commandBuffer, vk.DependencyInfo{Image: []vk.ImageMemoryBarrier2{{
		// Stage: which earlier work must finish and which later work may not start
		SrcStageMask: from.stage, DstStageMask: to.stage,
		// Access: caches are incoherent so write the new value out and drop the reader's outdated copy
		SrcAccessMask: from.access, DstAccessMask: to.access,
		// Layout: rearrange the pixels, the reader cannot use the arrangement the writer left
		OldLayout: from.layout, NewLayout: to.layout,
		// Queue family: an ownership handover, ignored while the engine has a single queue
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored, DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Image: image.vkImage,
		SubresourceRange: vk.ImageSubresourceRange{
			AspectMask: image.vkAspect, BaseMipLevel: 0, LevelCount: 1,
			BaseArrayLayer: 0, LayerCount: image.layerCount,
		},
	}}})
	image.use = want
}

// Transitions a buffer, which is stage and access masks alone
func (backend *VKBackend) useBuffer(commandBuffer vk.CommandBuffer, info *bufferInfo, want use) { // TODO: review
	if info == nil || info.vkBuffer == 0 {
		return
	}
	to := useTable[want]
	if info.use == want && !to.write {
		return
	}
	from := useTable[info.use]
	vk.CmdPipelineBarrier2(commandBuffer, vk.DependencyInfo{Buffer: []vk.BufferMemoryBarrier2{{
		SrcStageMask: from.stage, SrcAccessMask: from.access,
		DstStageMask: to.stage, DstAccessMask: to.access,
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored, DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Buffer: info.vkBuffer, Size: vk.WholeSize,
	}}})
	info.use = want
}
