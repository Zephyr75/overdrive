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
	vkArenaBuffer      vk.Buffer        // holds the frame's uniform arena
	vmaArenaAlloc      vk.VmaAllocation // vma allocation of the arena buffer
	arenaMapped        unsafe.Pointer   // CPU pointer to the mapped arena region
	arenaAddr          uint64           // GPU-side address of the same arena region
	arenaUsed          uint64           // number of bytes already stored in the arena, max size is constant `arenaSize`
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
func (backend *VKBackend) Frame(record func(renderer.Frame)) {
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

	// Record the user’s frame closure, passing a thin wrapper that hides
	// the backend and exposes the command buffer
	backend.recording = true
	record(&vkFrame{VKBackend: backend, vkCommandBuffer: frame.vkCommandBuffer})
	backend.recording = false

	// Record a transition to the present layout for the swapchain image
	backend.recordUseImage(frame.vkCommandBuffer,
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
	err = vk.QueuePresentKHR(backend.vkQueue,
		backend.vkRenderSemaphores[backend.imageIndex],
		backend.vkSwapchain, backend.imageIndex)
	if err != nil {
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
func (vkFrame *vkFrame) Upload(data any) renderer.Address {
	frame := &vkFrame.VKBackend.frames[vkFrame.VKBackend.frameIndex]
	ptr, n := getDataPointer(data)
	if n == 0 {
		return renderer.Address(frame.arenaAddr)
	}
	// 64-byte aligned, keeping each block on a cache line
	frame.arenaUsed = (frame.arenaUsed + 63) &^ 63
	if frame.arenaUsed+n > arenaSize {
		panic(fmt.Sprintf("vulkan: uniform arena overflow at %d bytes (cap %d)", frame.arenaUsed+n, arenaSize))
	}
	// Use CPU-side mapped address to copy data
	memoryCopy(unsafe.Add(frame.arenaMapped, frame.arenaUsed), ptr, n)
	// Update GPU-side address
	addr := frame.arenaAddr + frame.arenaUsed
	frame.arenaUsed += n
	return renderer.Address(addr)
}

// Pass records a Vulkan render pass that may contain colour and/or depth
// attachments. It transitions attachments, starts a dynamic rendering
// block, invokes the supplied closure, and then ends the block.
func (vkFrame *vkFrame) Pass(spec renderer.PassSpec, record func(renderer.Pass)) { 
	backend, commandBuffer := vkFrame.VKBackend, vkFrame.vkCommandBuffer
	// Transition any images that will be read by this pass
	backend.transitionReads(commandBuffer, spec.Reads)

	// Build the list of colour attachments and determine render area
	width, height := 0, 0
	color := make([]vk.RenderingAttachmentInfo, 0, len(spec.Color))
	for _, attachment := range spec.Color {
		att, attachWidth, attachHeight := backend.buildColorAttachment(commandBuffer, attachment)
		if att.ImageView == 0 {
			continue
		} // ignore unused attachment
		if width == 0 {
			width, height = attachWidth, attachHeight
		} // use first attachment’s size
		color = append(color, att)
	}

	// Prepare the optional depth attachment
	var depthPtr *vk.RenderingAttachmentInfo
	if spec.Depth != nil {
		view, img, attachWidth, attachHeight, _ := backend.view(spec.Depth.View)
		if view != 0 {
			backend.recordUseImage(commandBuffer, img, useDepthAttach)
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

	// No usable attachment → skip the pass
	if width == 0 || height == 0 {
		fmt.Fprintf(os.Stderr, "vulkan: pass %q has no live attachment, skipped\n", spec.Name)
		return
	}

	// Determine how many layers to render
	layers := uint32(spec.Layers)
	if layers == 0 {
		layers = 1
	}

	// Start profiling label
	backend.beginLabel(commandBuffer, spec.Name)

	// Record begin dynamic rendering with the gathered attachments
	vk.CmdBeginRendering(commandBuffer, vk.RenderingInfo{
		RenderArea:       vk.Rect2D{Extent: vk.Extent2D{Width: uint32(width), Height: uint32(height)}},
		LayerCount:       layers,
		ColorAttachments: color,
		DepthAttachment:  depthPtr,
	})

	// Provide the pass object to the caller and run the closure
	pass := &vkPass{VKBackend: backend, vkCommandBuffer: commandBuffer, flipY: spec.FlipY, width: width, height: height}
	pass.Viewport(0, 0, width, height)
	backend.vkBoundPipeline = 0
	record(pass)

	// Record end the rendering block and finish the profiling label
	vk.CmdEndRendering(commandBuffer)
	backend.endLabel(commandBuffer, spec.Name)
}

// Runs one compute pass, outside any render pass
func (vkFrame *vkFrame) Compute(spec renderer.ComputeSpec, record func(renderer.Compute)) { 
	backend, commandBuffer := vkFrame.VKBackend, vkFrame.vkCommandBuffer
	backend.transitionReads(commandBuffer, spec.Reads)
	for _, height := range spec.Writes {
		switch renderer.Kind(height) {
		case renderer.KindImage:
			backend.recordUseImage(commandBuffer, backend.image(renderer.ImageHandle(renderer.Index(height))), useStorage)
		case renderer.KindBuffer:
			backend.recordUseBuffer(commandBuffer, backend.buffer(renderer.BufferHandle(renderer.Index(height))), useStorage)
		}
	}

	backend.beginLabel(commandBuffer, spec.Name)
	backend.vkBoundPipeline = 0
	record(&vkCompute{VKBackend: backend, vkCommandBuffer: commandBuffer})
	backend.endLabel(commandBuffer, spec.Name)
}

// Prepares images and buffers for reading
func (backend *VKBackend) transitionReads(commandBuffer vk.CommandBuffer, reads []renderer.Handle) {
	for _, height := range reads {
		// Determine the kind of handle (image or buffer)
		switch renderer.Kind(height) {
		case renderer.KindImage:
			// Resolve the actual image object
			image := backend.image(renderer.ImageHandle(renderer.Index(height)))
			if image == nil {
				continue
			}

			// Decide which layout we need:
			// - If the image was created with ImageSampled, we need a sampled layout
			// - Otherwise, we need a general storage layout
			want := useSampled
			if image.usage&renderer.ImageSampled == 0 {
				want = useStorage
			}

			// Record the transition/descriptor bind for this image
			backend.recordUseImage(commandBuffer, image, want)
		case renderer.KindBuffer:
			// Resolve the buffer and mark it for shader read
			backend.recordUseBuffer(commandBuffer, backend.buffer(renderer.BufferHandle(renderer.Index(height))), useShaderRead)
		}
	}
}

// Builds one colour attachment, resolving the reserved backbuffer view into the
// multisampled image plus its resolve target when the backend multisamples
func (backend *VKBackend) buildColorAttachment(commandBuffer vk.CommandBuffer, attachment renderer.Attachment) (vk.RenderingAttachmentInfo, int, int) { 
	view, img, width, height, _ := backend.view(attachment.View)
	if view == 0 {
		return vk.RenderingAttachmentInfo{}, 0, 0
	}
	backend.recordUseImage(commandBuffer, img, useColorAttach)

	vkAttachmentInfo := vk.RenderingAttachmentInfo{
		ImageView:   view,
		ImageLayout: vk.ImageLayoutColorAttachmentOptimal,
		LoadOp:      vk.AttachmentLoadOpLoad,
		StoreOp:     storeOp(attachment.Store),
	}
	clear := attachment.Clear
	if clear != nil {
		vkAttachmentInfo.LoadOp = vk.AttachmentLoadOpClear
		vkAttachmentInfo.ClearValue = vk.ClearColor(clear[0], clear[1], clear[2], clear[3])
	}

	// A multisampled pass names its own colour image and resolves into the
	// backbuffer, which is the one view that is not the caller's
	resolve := attachment.Resolve
	if resolve != renderer.NoView {
		var resolveView vk.ImageView
		var resolvedImage *image
		if resolve == renderer.Backbuffer {
			resolvedImage = &backend.swapchainImages[backend.imageIndex]
			resolveView = resolvedImage.vkView
		} else {
			resolveView, resolvedImage, _, _, _ = backend.view(resolve)
		}
		if resolveView != 0 {
			backend.recordUseImage(commandBuffer, resolvedImage, useColorAttach)
			vkAttachmentInfo.ResolveImageView = resolveView
			vkAttachmentInfo.ResolveImageLayout = vk.ImageLayoutColorAttachmentOptimal
			vkAttachmentInfo.ResolveMode = vk.ResolveModeAverage
			vkAttachmentInfo.StoreOp = vk.AttachmentStoreOpDontCare
		}
	}
	return vkAttachmentInfo, width, height
}

func storeOp(store bool) vk.AttachmentStoreOp { 
	if store {
		return vk.AttachmentStoreOpStore
	}
	return vk.AttachmentStoreOpDontCare
}

// Copy moves data between different Vulkan resources, regardless of
// whether the source and destination are both images, both buffers, or
// an image and a buffer. It runs outside any rendering pass.
func (vkFrame *vkFrame) Copy(spec renderer.CopySpec) {
	backend, commandBuffer := vkFrame.VKBackend, vkFrame.vkCommandBuffer
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
	// Image to Image copy: Handles moving data between two images (e.g., static to dynamic atlas)
	case src != nil && dst != nil:
		if !backend.inBounds(src, spec.SrcOffset, spec.Extent) || !backend.inBounds(dst, spec.DstOffset, spec.Extent) {
			fmt.Fprintln(os.Stderr, "vulkan: image copy out of bounds, ignored")
			return
		}
		backend.recordUseImage(commandBuffer, src, useCopySrc)
		backend.recordUseImage(commandBuffer, dst, useCopyDst)
		vk.CmdCopyImage(commandBuffer, src.vkImage, vk.ImageLayoutTransferSrcOptimal,
			dst.vkImage, vk.ImageLayoutTransferDstOptimal, []vk.ImageCopy{{
				AspectMask:        aspect,
				SrcBaseArrayLayer: uint32(spec.SrcLayer),
				SrcOffset:         vk.Offset2D{X: int32(spec.SrcOffset[0]), Y: int32(spec.SrcOffset[1])},
				DstBaseArrayLayer: uint32(spec.DstLayer),
				DstOffset:         vk.Offset2D{X: int32(spec.DstOffset[0]), Y: int32(spec.DstOffset[1])},
				LayerCount:        layers, Extent: ext,
			}})
	// Image to Buffer copy: Handles moving data from an image (e.g., a sampled texture) into a buffer.
	case src != nil && dbuf != nil:
		backend.recordUseImage(commandBuffer, src, useCopySrc)
		backend.recordUseBuffer(commandBuffer, dbuf, useCopyDst)
		vk.CmdCopyImageToBuffer(commandBuffer, src.vkImage, vk.ImageLayoutTransferSrcOptimal, dbuf.vkBuffer,
			[]vk.BufferImageCopy{{
				BufferOffset: spec.DstBytes, AspectMask: aspect,
				BaseArrayLayer: uint32(spec.SrcLayer), LayerCount: layers,
				ImageOffset: vk.Offset2D{X: int32(spec.SrcOffset[0]), Y: int32(spec.SrcOffset[1])},
				ImageExtent: ext,
			}})
	// Buffer to Image copy: Handles moving data from a buffer (e.g., an index buffer) into an image.
	case sbuf != nil && dst != nil:
		backend.recordUseBuffer(commandBuffer, sbuf, useCopySrc)
		backend.recordUseImage(commandBuffer, dst, useCopyDst)
		vk.CmdCopyBufferToImage(commandBuffer, sbuf.vkBuffer, dst.vkImage, vk.ImageLayoutTransferDstOptimal,
			[]vk.BufferImageCopy{{
				BufferOffset: spec.SrcBytes, AspectMask: aspect,
				BaseArrayLayer: uint32(spec.DstLayer), LayerCount: layers,
				ImageOffset: vk.Offset2D{X: int32(spec.DstOffset[0]), Y: int32(spec.DstOffset[1])},
				ImageExtent: ext,
			}})
	// Buffer to Buffer copy: Copies data directly between two buffers.
	case sbuf != nil && dbuf != nil:
		backend.recordUseBuffer(commandBuffer, sbuf, useCopySrc)
		backend.recordUseBuffer(commandBuffer, dbuf, useCopyDst)
		vk.CmdCopyBuffer(commandBuffer, sbuf.vkBuffer, dbuf.vkBuffer, []vk.BufferCopy{{
			SrcOffset: spec.SrcBytes, DstOffset: spec.DstBytes, Size: uint64(spec.Extent[0]),
		}})
	default:
		fmt.Fprintln(os.Stderr, "vulkan: copy with no live source or destination, ignored")
	}
}

// Whether a rect fits inside an image, an out-of-bounds copy being a device loss
// rather than a clipped one
func (backend *VKBackend) inBounds(entry *image, off [3]int, ext [3]int) bool { 
	return ext[0] > 0 && ext[1] > 0 &&
		off[0] >= 0 && off[1] >= 0 &&
		off[0]+ext[0] <= entry.width && off[1]+ext[1] <= entry.height
}

// Clears a colour image outside any pass
func (vkFrame *vkFrame) Clear(spec renderer.ClearSpec) { 
	entry := vkFrame.VKBackend.image(spec.Image)
	if entry == nil {
		return
	}
	vkFrame.VKBackend.recordUseImage(vkFrame.vkCommandBuffer, entry, useCopyDst)
	vk.CmdClearColorImage(vkFrame.vkCommandBuffer, entry.vkImage, vk.ImageLayoutTransferDstOptimal, spec.Color,
		vk.ImageSubresourceRange{
			AspectMask: entry.vkAspect, BaseMipLevel: 0, LevelCount: 1,
			BaseArrayLayer: 0, LayerCount: entry.layerCount,
		})
}

// --- Pass --------------------------------------------------------------------

// Narrows the viewport and scissor to a rect of the pass's target
func (pass *vkPass) Viewport(x, y, width, height int) {
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

func (pass *vkPass) Draw(call renderer.DrawCall) { 
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
		backend.recordUseBuffer(pass.vkCommandBuffer, indirectBuffer, useIndirect)
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

func (compute *vkCompute) Dispatch(call renderer.DispatchCall) { 
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
		backend.recordUseBuffer(compute.vkCommandBuffer, indirectBuffer, useIndirect)
		vk.CmdDispatchIndirect(compute.vkCommandBuffer, indirectBuffer.vkBuffer, call.Indirect.Offset)
		return
	}
	vk.CmdDispatch(compute.vkCommandBuffer, uint32(max1(call.Groups[0])), uint32(max1(call.Groups[1])), uint32(max1(call.Groups[2])))
}

// A dispatch of zero groups is a no-op the caller never means
func max1(value int) int { 
	if value < 1 {
		return 1
	}
	return value
}

// --- shared recording helpers ------------------------------------------------

// Binds a pipeline, skipping the call when it is already bound
func (backend *VKBackend) bind(commandBuffer vk.CommandBuffer, pipeline *pipelineInfo) { 
	if pipeline.pipeline == backend.vkBoundPipeline {
		return
	}
	vk.CmdBindPipeline(commandBuffer, pipeline.bindPoint, pipeline.pipeline)
	backend.vkBoundPipeline = pipeline.pipeline
}

// Pushes the four opaque addresses a draw or dispatch carries
func (backend *VKBackend) push(commandBuffer vk.CommandBuffer, addrs [4]renderer.Address) { 
	vk.CmdPushConstants(commandBuffer, backend.vkPipelineLayout, pushStages, 0, pushConstantSize, unsafe.Pointer(&addrs))
}

// --- capture labels ----------------------------------------------------------

// Opens a labelled region, which is what groups a RenderDoc capture by pass
func (backend *VKBackend) beginLabel(commandBuffer vk.CommandBuffer, name string) { 
	if backend.hasLabels && name != "" {
		vk.CmdBeginDebugLabel(commandBuffer, name)
	}
}

func (backend *VKBackend) endLabel(commandBuffer vk.CommandBuffer, name string) { 
	if backend.hasLabels && name != "" {
		vk.CmdEndDebugLabel(commandBuffer)
	}
}
