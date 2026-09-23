package vulkan

import (
	"fmt"
	"math"
	"os"
	"unsafe"

	"go-vulkan/vk"

	"github.com/Zephyr75/overdrive/renderer"
)

// Everything one in-flight frame owns: its command buffer, sync objects and
// upload arena
type frame struct {
	vkCommandBuffer    vk.CommandBuffer // command buffer for recording commands
	vkFence            vk.Fence         // signals when GPU has finished the frame
	vkAcquireSemaphore vk.Semaphore     // signals when image is ready to be presented
	vkArenaBuffer      vk.Buffer // holds the frame's uniform arena
	vmaArenaAlloc      vk.VmaAllocation // vma allocation of the arena buffer
	arenaMapped        unsafe.Pointer // CPU pointer to the mapped arena region
	arenaAddr          uint64 // GPU-side address of the uniform arena buffer
	arenaUsed          uint64 // number of bytes already stored in the arena, max size is constant `arenaSize`
}

// The recording handles. A Pass value cannot exist outside Frame.Pass, so the
// ordering rules that used to be runtime guards are scope now: a copy inside a
// render pass, or a dispatch inside one, does not compile.
type vkFrame struct {
	VKBackend       *VKBackend
	vkCommandBuffer vk.CommandBuffer
}

type vkPass struct {
	VKBackend       *VKBackend
	vkCommandBuffer vk.CommandBuffer
	flipY           bool
	width, height   int
}

type vkCompute struct {
	VKBackend       *VKBackend
	vkCommandBuffer vk.CommandBuffer
}

// Frame records one rendering frame.
// It waits on the previous frame’s fence, acquires a swap‑chain image,
// resets/records a command buffer, submits it, and presents the image.
func (backend *VKBackend) Frame(record func(renderer.Frame)) { // TODO: review
	// No backend means nothing to do
	if backend.vkDevice == 0 {
		return
	}

	// Grab the bookkeeping entry for the current frame
	frame := &backend.frames[backend.frameIndex]

	// Wait for the GPU to finish the previous command buffer for this frame
	fatalVk(vk.WaitForFences(backend.vkDevice, []vk.Fence{frame.vkFence},
		true, math.MaxUint64), "wait frame fence")

	// Acquire the next swapchain image. If the swapchain is out‑of‑date
	// (resized) we recreate it and return: the caller will rebuild its
	// own per‑frame targets on the next Frame call.
	idx, err := vk.AcquireNextImageKHR(backend.vkDevice, backend.vkSwapchain,
		math.MaxUint64, frame.vkAcquireSemaphore, 0)
	if err == vk.ErrOutOfDateKHR {
		backend.recreateSwapchain()
		return
	}
	if err != nil && err != vk.SuboptimalKHR {
		fmt.Fprintf(os.Stderr, "vulkan: acquire failed: %v\n", err)
	}
	backend.imageIndex = idx

	// Reset the frame fence and clear the arena‑usage counter
	fatalVk(vk.ResetFences(backend.vkDevice, []vk.Fence{frame.vkFence}),
		"reset frame fence")
	frame.arenaUsed = 0
	backend.frameCounter++
	backend.drainRetired()

	// Reset and begin the command buffer that will record this frame
	fatalVk(vk.ResetCommandBuffer(frame.vkCommandBuffer),
		"reset command buffer")
	fatalVk(vk.BeginCommandBuffer(frame.vkCommandBuffer,
		vk.CommandBufferUsageOneTimeSubmit), "begin command buffer")

	// Bind the single descriptor set that holds all bindless (accessed by linear index) descriptors
	vk.CmdBindDescriptorSets(frame.vkCommandBuffer, vk.PipelineBindPointGraphics,
		backend.vkPipelineLayout, 0, []vk.DescriptorSet{backend.vkDescriptorSet})
	vk.CmdBindDescriptorSets(frame.vkCommandBuffer, vk.PipelineBindPointCompute,
		backend.vkPipelineLayout, 0, []vk.DescriptorSet{backend.vkDescriptorSet})

	// Mark the swapchain image as unused
	backend.swapchainImages[backend.imageIndex].use = useNone

	// Record copies of all pending uploads
	// Example: shadow map has been updated mid-frame but is still being read,
	// then we wait for the beginning of next frame to upload the new shadow map value
	// and use it when rendering the next frame
	backend.executePendingUploads(frame.vkCommandBuffer)

	// TODO here2
	// Record the user’s frame closure, passing a thin wrapper that hides
	// the backend and exposes the command buffer
	backend.recording = true
	record(&vkFrame{VKBackend: backend, vkCommandBuffer: frame.vkCommandBuffer})
	backend.recording = false

	// Record a transition to the present layout for the swapchain image
	backend.useImage(frame.vkCommandBuffer,
		&backend.swapchainImages[backend.imageIndex], usePresent)
	fatalVk(vk.EndCommandBuffer(frame.vkCommandBuffer),
		"end command buffer")

	// Submit the command buffer to the GPU, signalling the render
	// semaphore for the image when finished
	fatalVk(vk.QueueSubmit2(backend.vkQueue, []vk.SubmitInfo2{{
		WaitSemaphores: []vk.SemaphoreSubmitInfo{{Semaphore: frame.vkAcquireSemaphore,
			StageMask: vk.PipelineStage2ColorAttachmentOutput}},
		CommandBuffers: []vk.CommandBuffer{frame.vkCommandBuffer},
		SignalSemaphores: []vk.SemaphoreSubmitInfo{{Semaphore: backend.vkRenderSemaphores[backend.imageIndex],
			StageMask: vk.PipelineStage2AllCommands}},
	}}, frame.vkFence), "queue submit")

	// Present the swapchain image. Handle OutOfDate/Suboptimal by recreating
	// the swapchain: the caller will rebuild its targets next frame
	if err := vk.QueuePresentKHR(backend.vkQueue,
		backend.vkRenderSemaphores[backend.imageIndex],
		backend.vkSwapchain, backend.imageIndex); err != nil {
		if err == vk.ErrOutOfDateKHR || err == vk.SuboptimalKHR {
			backend.recreateSwapchain()
		} else {
			fmt.Fprintf(os.Stderr, "vulkan: present failed: %v\n", err)
		}
	}

	// Advance the frame index for the next iteration
	backend.frameIndex = (backend.frameIndex + 1) % framesInFlight
}

// --- Frame -------------------------------------------------------------------

// Copies a block into this frame's arena and returns its device address
//
// Frame-scoped: the arena resets every frame, so an address kept across frames
// points at another frame's data. An overflow panics rather than wrapping — an
// overflowed frame is already wrong, and wrapping made it wrong silently
func (frame *vkFrame) Upload(data any) renderer.Address { // TODO: review
	info := &frame.VKBackend.frames[frame.VKBackend.frameIndex]
	ptr, n := getDataPointer(data)
	if n == 0 {
		return renderer.Address(info.arenaAddr)
	}
	// 64-byte aligned, keeping each block on a cache line
	info.arenaUsed = (info.arenaUsed + 63) &^ 63
	if info.arenaUsed+n > arenaSize {
		panic(fmt.Sprintf("vulkan: uniform arena overflow at %d bytes (cap %d)", info.arenaUsed+n, arenaSize))
	}
	memoryCopy(unsafe.Add(info.arenaMapped, info.arenaUsed), ptr, n)
	addr := info.arenaAddr + info.arenaUsed
	info.arenaUsed += n
	return renderer.Address(addr)
}

// Runs one render pass: transitions everything it names, opens dynamic
// rendering, and closes it again
func (frame *vkFrame) Pass(spec renderer.PassSpec, record func(renderer.Pass)) { // TODO: review
	backend, commandBuffer := frame.VKBackend, frame.vkCommandBuffer
	backend.transitionReads(commandBuffer, spec.Reads)

	width, height := 0, 0
	color := make([]vk.RenderingAttachmentInfo, 0, len(spec.Color))
	for _, attachment := range spec.Color {
		att, attachWidth, attachHeight := backend.colorAttachment(commandBuffer, attachment)
		if att.ImageView == 0 {
			continue
		}
		if width == 0 {
			width, height = attachWidth, attachHeight
		}
		color = append(color, att)
	}

	var depthPtr *vk.RenderingAttachmentInfo
	if spec.Depth != nil {
		view, img, attachWidth, attachHeight, _ := backend.view(spec.Depth.View)
		if view != 0 {
			backend.useImage(commandBuffer, img, useDepthAttach)
			att := vk.RenderingAttachmentInfo{
				ImageView:   view,
				ImageLayout: vk.ImageLayoutDepthAttachmentOptimal,
				LoadOp:      vk.AttachmentLoadOpLoad,
				StoreOp:     storeOp(spec.Depth.Store),
			}
			if clear := spec.Depth.Clear; clear != nil {
				att.LoadOp = vk.AttachmentLoadOpClear
				att.ClearValue = vk.ClearDepthStencil(clear[0], 0)
			}
			depthPtr = &att
			if width == 0 {
				width, height = attachWidth, attachHeight
			}
		}
	}
	if width == 0 || height == 0 {
		fmt.Fprintf(os.Stderr, "vulkan: pass %q has no live attachment, skipped\n", spec.Name)
		return
	}

	layers := uint32(spec.Layers)
	if layers == 0 {
		layers = 1
	}

	backend.beginLabel(commandBuffer, spec.Name)

	vk.CmdBeginRendering(commandBuffer, vk.RenderingInfo{
		RenderArea:       vk.Rect2D{Extent: vk.Extent2D{Width: uint32(width), Height: uint32(height)}},
		LayerCount:       layers,
		ColorAttachments: color,
		DepthAttachment:  depthPtr,
	})

	pass := &vkPass{VKBackend: backend, vkCommandBuffer: commandBuffer, flipY: spec.FlipY, width: width, height: height}
	pass.Viewport(0, 0, width, height)
	backend.vkBoundPipeline = 0
	record(pass)

	vk.CmdEndRendering(commandBuffer)
	backend.endLabel(commandBuffer, spec.Name)
}

// Runs one compute pass, outside any render pass
//
// A dispatch reaches its resources through descriptors and device addresses,
// which the backend cannot inspect — so ComputeSpec names them and this is where
// they are transitioned
func (frame *vkFrame) Compute(spec renderer.ComputeSpec, record func(renderer.Compute)) { // TODO: review
	backend, commandBuffer := frame.VKBackend, frame.vkCommandBuffer
	backend.transitionReads(commandBuffer, spec.Reads)
	for _, height := range spec.Writes {
		switch renderer.Kind(height) {
		case renderer.KindImage:
			backend.useImage(commandBuffer, backend.image(renderer.ImageHandle(renderer.Index(height))), useStorage)
		case renderer.KindBuffer:
			backend.useBuffer(commandBuffer, backend.buffer(renderer.BufferHandle(renderer.Index(height))), useStorage)
		}
	}

	backend.beginLabel(commandBuffer, spec.Name)
	backend.vkBoundPipeline = 0
	record(&vkCompute{VKBackend: backend, vkCommandBuffer: commandBuffer})
	backend.endLabel(commandBuffer, spec.Name)
}

// Transitions everything a pass declares it samples or reads
func (backend *VKBackend) transitionReads(commandBuffer vk.CommandBuffer, reads []renderer.Handle) { // TODO: review
	for _, height := range reads {
		switch renderer.Kind(height) {
		case renderer.KindImage:
			entry := backend.image(renderer.ImageHandle(renderer.Index(height)))
			if entry == nil {
				continue
			}
			want := useSampled
			if entry.usage&renderer.ImageSampled == 0 {
				want = useStorage
			}
			backend.useImage(commandBuffer, entry, want)
		case renderer.KindBuffer:
			backend.useBuffer(commandBuffer, backend.buffer(renderer.BufferHandle(renderer.Index(height))), useShaderRead)
		}
	}
}

// Builds one colour attachment, resolving the reserved backbuffer view into the
// multisampled image plus its resolve target when the backend multisamples
func (backend *VKBackend) colorAttachment(commandBuffer vk.CommandBuffer, attachment renderer.Attachment) (vk.RenderingAttachmentInfo, int, int) { // TODO: review
	view, img, width, height, _ := backend.view(attachment.View)
	if view == 0 {
		return vk.RenderingAttachmentInfo{}, 0, 0
	}
	backend.useImage(commandBuffer, img, useColorAttach)

	att := vk.RenderingAttachmentInfo{
		ImageView:   view,
		ImageLayout: vk.ImageLayoutColorAttachmentOptimal,
		LoadOp:      vk.AttachmentLoadOpLoad,
		StoreOp:     storeOp(attachment.Store),
	}
	if clear := attachment.Clear; clear != nil {
		att.LoadOp = vk.AttachmentLoadOpClear
		att.ClearValue = vk.ClearColor(clear[0], clear[1], clear[2], clear[3])
	}

	// A multisampled pass names its own colour image and resolves into the
	// backbuffer, which is the one view that is not the caller's
	resolve := attachment.Resolve
	if resolve != renderer.NoView {
		var resolveView vk.ImageView
		var rimg *image
		if resolve == renderer.Backbuffer {
			rimg = &backend.swapchainImages[backend.imageIndex]
			resolveView = rimg.vkView
		} else {
			resolveView, rimg, _, _, _ = backend.view(resolve)
		}
		if resolveView != 0 {
			backend.useImage(commandBuffer, rimg, useColorAttach)
			att.ResolveImageView = resolveView
			att.ResolveImageLayout = vk.ImageLayoutColorAttachmentOptimal
			att.ResolveMode = vk.ResolveModeAverage
			att.StoreOp = vk.AttachmentStoreOpDontCare
		}
	}
	return att, width, height
}

func storeOp(store bool) vk.AttachmentStoreOp { // TODO: review
	if store {
		return vk.AttachmentStoreOpStore
	}
	return vk.AttachmentStoreOpDontCare
}

// Copies between images and buffers, outside any pass
func (frame *vkFrame) Copy(spec renderer.CopySpec) { // TODO: review
	backend, commandBuffer := frame.VKBackend, frame.vkCommandBuffer
	layers := uint32(spec.Layers)
	if layers == 0 {
		layers = 1
	}
	ext := vk.Extent3D{Width: uint32(spec.Extent[0]), Height: uint32(spec.Extent[1]), Depth: uint32(spec.Extent[2])}
	if ext.Depth == 0 {
		ext.Depth = 1
	}
	aspect := vk.ImageAspectFlags(vk.ImageAspectColor)
	if spec.Aspect == renderer.AspectDepth {
		aspect = vk.ImageAspectDepth
	}

	src, dst := backend.image(spec.SrcImage), backend.image(spec.DstImage)
	sbuf, dbuf := backend.buffer(spec.SrcBuffer), backend.buffer(spec.DstBuffer)

	switch {
	case src != nil && dst != nil:
		if !backend.inBounds(src, spec.SrcOffset, spec.Extent) || !backend.inBounds(dst, spec.DstOffset, spec.Extent) {
			fmt.Fprintln(os.Stderr, "vulkan: image copy out of bounds, ignored")
			return
		}
		backend.useImage(commandBuffer, src, useCopySrc)
		backend.useImage(commandBuffer, dst, useCopyDst)
		vk.CmdCopyImage(commandBuffer, src.vkImage, vk.ImageLayoutTransferSrcOptimal,
			dst.vkImage, vk.ImageLayoutTransferDstOptimal, []vk.ImageCopy{{
				AspectMask:        aspect,
				SrcBaseArrayLayer: uint32(spec.SrcLayer),
				SrcOffset:         vk.Offset2D{X: int32(spec.SrcOffset[0]), Y: int32(spec.SrcOffset[1])},
				DstBaseArrayLayer: uint32(spec.DstLayer),
				DstOffset:         vk.Offset2D{X: int32(spec.DstOffset[0]), Y: int32(spec.DstOffset[1])},
				LayerCount:        layers, Extent: ext,
			}})
	case src != nil && dbuf != nil:
		backend.useImage(commandBuffer, src, useCopySrc)
		backend.useBuffer(commandBuffer, dbuf, useCopyDst)
		vk.CmdCopyImageToBuffer(commandBuffer, src.vkImage, vk.ImageLayoutTransferSrcOptimal, dbuf.vkBuffer,
			[]vk.BufferImageCopy{{
				BufferOffset: spec.DstBytes, AspectMask: aspect,
				BaseArrayLayer: uint32(spec.SrcLayer), LayerCount: layers,
				ImageOffset: vk.Offset2D{X: int32(spec.SrcOffset[0]), Y: int32(spec.SrcOffset[1])},
				ImageExtent: ext,
			}})
	case sbuf != nil && dst != nil:
		backend.useBuffer(commandBuffer, sbuf, useCopySrc)
		backend.useImage(commandBuffer, dst, useCopyDst)
		vk.CmdCopyBufferToImage(commandBuffer, sbuf.vkBuffer, dst.vkImage, vk.ImageLayoutTransferDstOptimal,
			[]vk.BufferImageCopy{{
				BufferOffset: spec.SrcBytes, AspectMask: aspect,
				BaseArrayLayer: uint32(spec.DstLayer), LayerCount: layers,
				ImageOffset: vk.Offset2D{X: int32(spec.DstOffset[0]), Y: int32(spec.DstOffset[1])},
				ImageExtent: ext,
			}})
	case sbuf != nil && dbuf != nil:
		backend.useBuffer(commandBuffer, sbuf, useCopySrc)
		backend.useBuffer(commandBuffer, dbuf, useCopyDst)
		vk.CmdCopyBuffer(commandBuffer, sbuf.vkBuffer, dbuf.vkBuffer, []vk.BufferCopy{{
			SrcOffset: spec.SrcBytes, DstOffset: spec.DstBytes, Size: uint64(spec.Extent[0]),
		}})
	default:
		fmt.Fprintln(os.Stderr, "vulkan: copy with no live source or destination, ignored")
	}
}

// Whether a rect fits inside an image, an out-of-bounds copy being a device loss
// rather than a clipped one
func (backend *VKBackend) inBounds(entry *image, off [3]int, ext [3]int) bool { // TODO: review
	return ext[0] > 0 && ext[1] > 0 &&
		off[0] >= 0 && off[1] >= 0 &&
		off[0]+ext[0] <= entry.width && off[1]+ext[1] <= entry.height
}

// Clears a colour image outside any pass
func (frame *vkFrame) Clear(spec renderer.ClearSpec) { // TODO: review
	entry := frame.VKBackend.image(spec.Image)
	if entry == nil {
		return
	}
	frame.VKBackend.useImage(frame.vkCommandBuffer, entry, useCopyDst)
	vk.CmdClearColorImage(frame.vkCommandBuffer, entry.vkImage, vk.ImageLayoutTransferDstOptimal, spec.Color,
		vk.ImageSubresourceRange{
			AspectMask: entry.vkAspect, BaseMipLevel: 0, LevelCount: 1,
			BaseArrayLayer: 0, LayerCount: entry.layerCount,
		})
}

// --- Pass --------------------------------------------------------------------

// Narrows the viewport and scissor to a rect of the pass's target
//
// A flipped pass gets a negative-height viewport, which makes clip space y-up
// and inverts winding with it — which is why a pipeline drawn there declares
// counter-clockwise front faces
func (pass *vkPass) Viewport(x, y, width, height int) { // TODO: review
	viewport := vk.Viewport{X: float32(x), Y: float32(y), Width: float32(width), Height: float32(height), MaxDepth: 1}
	if pass.flipY {
		viewport.Y = float32(y + height)
		viewport.Height = -float32(height)
	}
	vk.CmdSetViewport(pass.vkCommandBuffer, viewport)
	vk.CmdSetScissor(pass.vkCommandBuffer, vk.Rect2D{
		Offset: vk.Offset2D{X: int32(x), Y: int32(y)},
		Extent: vk.Extent2D{Width: uint32(width), Height: uint32(height)},
	})
}

func (pass *vkPass) Draw(call renderer.DrawCall) { // TODO: review
	backend := pass.VKBackend
	pipeline := backend.pipeline(call.Pipeline)
	mesh := backend.mesh(call.Mesh)
	if pipeline == nil || mesh == nil {
		return
	}
	backend.bind(pass.vkCommandBuffer, pipeline)
	backend.push(pass.vkCommandBuffer, call.Push)

	if vertexBuffer := backend.buffer(mesh.vertices); vertexBuffer != nil {
		vk.CmdBindVertexBuffer(pass.vkCommandBuffer, 0, vertexBuffer.vkBuffer, 0)
	}
	instances := uint32(call.Instances)
	if instances == 0 {
		instances = 1
	}
	if mesh.indexed {
		vk.CmdBindIndexBuffer(pass.vkCommandBuffer, mesh.vkIndexBuffer, 0, vk.IndexTypeUint32)
	}
	if call.Indirect != nil {
		indirectBuffer := backend.buffer(call.Indirect.Buffer)
		if indirectBuffer == nil {
			return
		}
		backend.useBuffer(pass.vkCommandBuffer, indirectBuffer, useIndirect)
		if mesh.indexed {
			vk.CmdDrawIndexedIndirect(pass.vkCommandBuffer, indirectBuffer.vkBuffer, call.Indirect.Offset, uint32(call.Indirect.Count), uint32(call.Indirect.Stride))
		} else {
			vk.CmdDrawIndirect(pass.vkCommandBuffer, indirectBuffer.vkBuffer, call.Indirect.Offset, uint32(call.Indirect.Count), uint32(call.Indirect.Stride))
		}
		return
	}
	if mesh.indexed {
		vk.CmdDrawIndexed(pass.vkCommandBuffer, mesh.count, instances, 0, 0, 0)
		return
	}
	vk.CmdDraw(pass.vkCommandBuffer, mesh.count, instances, 0, 0)
}

// --- Compute -----------------------------------------------------------------

func (compute *vkCompute) Dispatch(call renderer.DispatchCall) { // TODO: review
	backend := compute.VKBackend
	pipeline := backend.pipeline(call.Pipeline)
	if pipeline == nil {
		return
	}
	backend.bind(compute.vkCommandBuffer, pipeline)
	backend.push(compute.vkCommandBuffer, call.Push)
	if call.Indirect != nil {
		indirectBuffer := backend.buffer(call.Indirect.Buffer)
		if indirectBuffer == nil {
			return
		}
		backend.useBuffer(compute.vkCommandBuffer, indirectBuffer, useIndirect)
		vk.CmdDispatchIndirect(compute.vkCommandBuffer, indirectBuffer.vkBuffer, call.Indirect.Offset)
		return
	}
	vk.CmdDispatch(compute.vkCommandBuffer, uint32(max1(call.Groups[0])), uint32(max1(call.Groups[1])), uint32(max1(call.Groups[2])))
}

// A dispatch of zero groups is a no-op the caller never means
func max1(value int) int { // TODO: review
	if value < 1 {
		return 1
	}
	return value
}

// --- shared recording helpers ------------------------------------------------

// Binds a pipeline, skipping the call when it is already bound
func (backend *VKBackend) bind(commandBuffer vk.CommandBuffer, pipeline *pipelineInfo) { // TODO: review
	if pipeline.pipeline == backend.vkBoundPipeline {
		return
	}
	vk.CmdBindPipeline(commandBuffer, pipeline.bindPoint, pipeline.pipeline)
	backend.vkBoundPipeline = pipeline.pipeline
}

// Pushes the four opaque addresses a draw or dispatch carries
//
// The backend never looks inside them: which block each slot points at is the
// shader's declaration and the caller's business
func (backend *VKBackend) push(commandBuffer vk.CommandBuffer, addrs [4]renderer.Address) { // TODO: review
	vk.CmdPushConstants(commandBuffer, backend.vkPipelineLayout, pushStages, 0, pushConstantSize, unsafe.Pointer(&addrs))
}

// --- capture labels ----------------------------------------------------------

// Opens a labelled region, which is what groups a RenderDoc capture by pass
//
// An unnamed pass opens none, so the two calls are conditioned identically and
// the regions cannot end up unbalanced
func (backend *VKBackend) beginLabel(commandBuffer vk.CommandBuffer, name string) { // TODO: review
	if backend.hasLabels && name != "" {
		vk.CmdBeginDebugLabel(commandBuffer, name)
	}
}

func (backend *VKBackend) endLabel(commandBuffer vk.CommandBuffer, name string) { // TODO: review
	if backend.hasLabels && name != "" {
		vk.CmdEndDebugLabel(commandBuffer)
	}
}
