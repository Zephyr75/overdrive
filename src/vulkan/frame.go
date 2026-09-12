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
type frameData struct {
	commandBuffer vk.CommandBuffer
	fence         vk.Fence
	acquireSemaphore    vk.Semaphore
	arena         vk.Buffer
	arenaAlloc    vk.VmaAllocation
	arenaMapped   unsafe.Pointer
	arenaAddr     uint64
	arenaUsed     uint64
}

// The recording handles. A Pass value cannot exist outside Frame.Pass, so the
// ordering rules that used to be runtime guards are scope now: a copy inside a
// render pass, or a dispatch inside one, does not compile.
type vkFrame struct {
	backend       *VKBackend
	commandBuffer vk.CommandBuffer
}

type vkPass struct {
	backend       *VKBackend
	commandBuffer vk.CommandBuffer
	flipY         bool
	width, height int
}

type vkCompute struct {
	backend       *VKBackend
	commandBuffer vk.CommandBuffer
}

// Records and submits one frame
func (backend *VKBackend) Frame(record func(renderer.Frame)) { // TODO: review
	if backend.device == 0 {
		return
	}
	frame := &backend.frames[backend.frameIndex]

	// Throttle the CPU here: without it frame N+2 would overwrite the arena and
	// the command buffer while the GPU still reads them
	fatal(vk.WaitForFences(backend.device, []vk.Fence{frame.fence}, true, math.MaxUint64), "wait frame fence")

	idx, err := vk.AcquireNextImageKHR(backend.device, backend.swapchain, math.MaxUint64, frame.acquireSemaphore, 0)
	// The swapchain is a new size, so every image the caller sized to the old
	// one is too small for this frame's render area. Rebuild and record
	// nothing: the caller compares BackbufferSize before its next Frame and
	// rebuilds its own images then. Returning here rather than re-acquiring is
	// what leaves the acquire semaphore unsignalled, which is the state the
	// next acquire needs it in
	if err == vk.ErrOutOfDateKHR {
		backend.recreateSwapchain()
		return
	}
	if err != nil && err != vk.SuboptimalKHR {
		fmt.Fprintf(os.Stderr, "vulkan: acquire failed: %v\n", err)
	}
	backend.imageIndex = idx

	fatal(vk.ResetFences(backend.device, []vk.Fence{frame.fence}), "reset frame fence")
	frame.arenaUsed = 0
	backend.frameCounter++
	backend.drainRetired()

	fatal(vk.ResetCommandBuffer(frame.commandBuffer), "reset command buffer")
	fatal(vk.BeginCommandBuffer(frame.commandBuffer, vk.CommandBufferUsageOneTimeSubmit), "begin command buffer")

	// One descriptor set for the whole frame, only its contents changing
	vk.CmdBindDescriptorSets(frame.commandBuffer, vk.PipelineBindPointGraphics, backend.pipelineLayout, 0,
		[]vk.DescriptorSet{backend.descriptorSet})
	vk.CmdBindDescriptorSets(frame.commandBuffer, vk.PipelineBindPointCompute, backend.pipelineLayout, 0,
		[]vk.DescriptorSet{backend.descriptorSet})

	// The swapchain image holds nothing worth keeping, so this frame's first
	// pass on it discards rather than loads
	backend.swapchainImages[backend.imageIndex].use = useNone

	// Anything staged during the previous frame's passes, copies being legal
	// only outside a render pass
	backend.flushPendingUploads(frame.commandBuffer)

	backend.recording = true
	record(&vkFrame{backend: backend, commandBuffer: frame.commandBuffer})
	backend.recording = false

	backend.useImage(frame.commandBuffer, &backend.swapchainImages[backend.imageIndex], usePresent)
	fatal(vk.EndCommandBuffer(frame.commandBuffer), "end command buffer")

	// Wait on the frame's semaphore, signal the image's: present waits on the
	// image's own, and the two index spaces are not interchangeable
	fatal(vk.QueueSubmit2(backend.queue, []vk.SubmitInfo2{{
		WaitSemaphores:   []vk.SemaphoreSubmitInfo{{Semaphore: frame.acquireSemaphore, StageMask: vk.PipelineStage2ColorAttachmentOutput}},
		CommandBuffers:   []vk.CommandBuffer{frame.commandBuffer},
		SignalSemaphores: []vk.SemaphoreSubmitInfo{{Semaphore: backend.renderSems[backend.imageIndex], StageMask: vk.PipelineStage2AllCommands}},
	}}, frame.fence), "queue submit")

	if err := vk.QueuePresentKHR(backend.queue, backend.renderSems[backend.imageIndex], backend.swapchain, backend.imageIndex); err != nil {
		if err == vk.ErrOutOfDateKHR || err == vk.SuboptimalKHR {
			backend.recreateSwapchain()
		} else {
			fmt.Fprintf(os.Stderr, "vulkan: present failed: %v\n", err)
		}
	}
	backend.frameIndex = (backend.frameIndex + 1) % framesInFlight
}

// --- Frame -------------------------------------------------------------------

// Copies a block into this frame's arena and returns its device address
//
// Frame-scoped: the arena resets every frame, so an address kept across frames
// points at another frame's data. An overflow panics rather than wrapping — an
// overflowed frame is already wrong, and wrapping made it wrong silently
func (frame *vkFrame) Upload(data any) renderer.Address { // TODO: review
	frameState := &frame.backend.frames[frame.backend.frameIndex]
	ptr, n := dataPtr(data)
	if n == 0 {
		return renderer.Address(frameState.arenaAddr)
	}
	// 64-byte aligned, keeping each block on a cache line
	frameState.arenaUsed = (frameState.arenaUsed + 63) &^ 63
	if frameState.arenaUsed+n > arenaSize {
		panic(fmt.Sprintf("vulkan: uniform arena overflow at %d bytes (cap %d)", frameState.arenaUsed+n, arenaSize))
	}
	memcpy(unsafe.Add(frameState.arenaMapped, frameState.arenaUsed), ptr, n)
	addr := frameState.arenaAddr + frameState.arenaUsed
	frameState.arenaUsed += n
	return renderer.Address(addr)
}

// Runs one render pass: transitions everything it names, opens dynamic
// rendering, and closes it again
func (frame *vkFrame) Pass(spec renderer.PassSpec, record func(renderer.Pass)) { // TODO: review
	backend, commandBuffer := frame.backend, frame.commandBuffer
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

	pass := &vkPass{backend: backend, commandBuffer: commandBuffer, flipY: spec.FlipY, width: width, height: height}
	pass.Viewport(0, 0, width, height)
	backend.boundPipeline = 0
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
	backend, commandBuffer := frame.backend, frame.commandBuffer
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
	backend.boundPipeline = 0
	record(&vkCompute{backend: backend, commandBuffer: commandBuffer})
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
		var rimg *imageInfo
		if resolve == renderer.Backbuffer {
			rimg = &backend.swapchainImages[backend.imageIndex]
			resolveView = rimg.view
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
	backend, commandBuffer := frame.backend, frame.commandBuffer
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
		vk.CmdCopyImage(commandBuffer, src.image, vk.ImageLayoutTransferSrcOptimal,
			dst.image, vk.ImageLayoutTransferDstOptimal, []vk.ImageCopy{{
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
		vk.CmdCopyImageToBuffer(commandBuffer, src.image, vk.ImageLayoutTransferSrcOptimal, dbuf.buffer,
			[]vk.BufferImageCopy{{
				BufferOffset: spec.DstBytes, AspectMask: aspect,
				BaseArrayLayer: uint32(spec.SrcLayer), LayerCount: layers,
				ImageOffset: vk.Offset2D{X: int32(spec.SrcOffset[0]), Y: int32(spec.SrcOffset[1])},
				ImageExtent: ext,
			}})
	case sbuf != nil && dst != nil:
		backend.useBuffer(commandBuffer, sbuf, useCopySrc)
		backend.useImage(commandBuffer, dst, useCopyDst)
		vk.CmdCopyBufferToImage(commandBuffer, sbuf.buffer, dst.image, vk.ImageLayoutTransferDstOptimal,
			[]vk.BufferImageCopy{{
				BufferOffset: spec.SrcBytes, AspectMask: aspect,
				BaseArrayLayer: uint32(spec.DstLayer), LayerCount: layers,
				ImageOffset: vk.Offset2D{X: int32(spec.DstOffset[0]), Y: int32(spec.DstOffset[1])},
				ImageExtent: ext,
			}})
	case sbuf != nil && dbuf != nil:
		backend.useBuffer(commandBuffer, sbuf, useCopySrc)
		backend.useBuffer(commandBuffer, dbuf, useCopyDst)
		vk.CmdCopyBuffer(commandBuffer, sbuf.buffer, dbuf.buffer, []vk.BufferCopy{{
			SrcOffset: spec.SrcBytes, DstOffset: spec.DstBytes, Size: uint64(spec.Extent[0]),
		}})
	default:
		fmt.Fprintln(os.Stderr, "vulkan: copy with no live source or destination, ignored")
	}
}

// Whether a rect fits inside an image, an out-of-bounds copy being a device loss
// rather than a clipped one
func (backend *VKBackend) inBounds(entry *imageInfo, off [3]int, ext [3]int) bool { // TODO: review
	return ext[0] > 0 && ext[1] > 0 &&
		off[0] >= 0 && off[1] >= 0 &&
		off[0]+ext[0] <= entry.width && off[1]+ext[1] <= entry.height
}

// Clears a colour image outside any pass
func (frame *vkFrame) Clear(spec renderer.ClearSpec) { // TODO: review
	entry := frame.backend.image(spec.Image)
	if entry == nil {
		return
	}
	frame.backend.useImage(frame.commandBuffer, entry, useCopyDst)
	vk.CmdClearColorImage(frame.commandBuffer, entry.image, vk.ImageLayoutTransferDstOptimal, spec.Color,
		vk.ImageSubresourceRange{
			AspectMask: entry.aspect, BaseMipLevel: 0, LevelCount: 1,
			BaseArrayLayer: 0, LayerCount: entry.layers,
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
	vk.CmdSetViewport(pass.commandBuffer, viewport)
	vk.CmdSetScissor(pass.commandBuffer, vk.Rect2D{
		Offset: vk.Offset2D{X: int32(x), Y: int32(y)},
		Extent: vk.Extent2D{Width: uint32(width), Height: uint32(height)},
	})
}

func (pass *vkPass) Draw(call renderer.DrawCall) { // TODO: review
	backend := pass.backend
	pipeline := backend.pipeline(call.Pipeline)
	mesh := backend.mesh(call.Mesh)
	if pipeline == nil || mesh == nil {
		return
	}
	backend.bind(pass.commandBuffer, pipeline)
	backend.push(pass.commandBuffer, call.Push)

	if vertexBuffer := backend.buffer(mesh.vertices); vertexBuffer != nil {
		vk.CmdBindVertexBuffer(pass.commandBuffer, 0, vertexBuffer.buffer, 0)
	}
	instances := uint32(call.Instances)
	if instances == 0 {
		instances = 1
	}
	if mesh.indexed {
		vk.CmdBindIndexBuffer(pass.commandBuffer, mesh.indexBuffer, 0, vk.IndexTypeUint32)
	}
	if call.Indirect != nil {
		indirectBuffer := backend.buffer(call.Indirect.Buffer)
		if indirectBuffer == nil {
			return
		}
		backend.useBuffer(pass.commandBuffer, indirectBuffer, useIndirect)
		if mesh.indexed {
			vk.CmdDrawIndexedIndirect(pass.commandBuffer, indirectBuffer.buffer, call.Indirect.Offset, uint32(call.Indirect.Count), uint32(call.Indirect.Stride))
		} else {
			vk.CmdDrawIndirect(pass.commandBuffer, indirectBuffer.buffer, call.Indirect.Offset, uint32(call.Indirect.Count), uint32(call.Indirect.Stride))
		}
		return
	}
	if mesh.indexed {
		vk.CmdDrawIndexed(pass.commandBuffer, mesh.count, instances, 0, 0, 0)
		return
	}
	vk.CmdDraw(pass.commandBuffer, mesh.count, instances, 0, 0)
}

// --- Compute -----------------------------------------------------------------

func (compute *vkCompute) Dispatch(call renderer.DispatchCall) { // TODO: review
	backend := compute.backend
	pipeline := backend.pipeline(call.Pipeline)
	if pipeline == nil {
		return
	}
	backend.bind(compute.commandBuffer, pipeline)
	backend.push(compute.commandBuffer, call.Push)
	if call.Indirect != nil {
		indirectBuffer := backend.buffer(call.Indirect.Buffer)
		if indirectBuffer == nil {
			return
		}
		backend.useBuffer(compute.commandBuffer, indirectBuffer, useIndirect)
		vk.CmdDispatchIndirect(compute.commandBuffer, indirectBuffer.buffer, call.Indirect.Offset)
		return
	}
	vk.CmdDispatch(compute.commandBuffer, uint32(max1(call.Groups[0])), uint32(max1(call.Groups[1])), uint32(max1(call.Groups[2])))
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
	if pipeline.pipeline == backend.boundPipeline {
		return
	}
	vk.CmdBindPipeline(commandBuffer, pipeline.bindPoint, pipeline.pipeline)
	backend.boundPipeline = pipeline.pipeline
}

// Pushes the four opaque addresses a draw or dispatch carries
//
// The backend never looks inside them: which block each slot points at is the
// shader's declaration and the caller's business
func (backend *VKBackend) push(commandBuffer vk.CommandBuffer, addrs [4]renderer.Address) { // TODO: review
	vk.CmdPushConstants(commandBuffer, backend.pipelineLayout, pushStages, 0, pushConstantSize, unsafe.Pointer(&addrs))
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
