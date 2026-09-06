// Package renderer defines the backend abstraction: the Backend/Frame/Pass/
// Compute interfaces, opaque resource handles, and the typed uniform blocks the
// scene fills. Nothing here names a graphics API, and nothing above this package
// may import one.
package renderer

// What a handle refers to, so one Destroy and one Slot can serve every kind
type handleKind uint8

const (
	kindNone handleKind = iota
	kindImage
	kindView
	kindBuffer
	kindMesh
	kindSampler
	kindPipeline
	kindAccel
)

// Handle is any resource the backend owns. The methods are unexported, so only
// this package's handle types satisfy it and the backend's type switch is total.
type Handle interface {
	kind() handleKind
	index() uint32
}

// The handle types, each an index into a table the backend keeps. Zero is
// always "none": no image, no view, no pipeline.
type (
	ImageHandle    uint32
	ViewHandle     uint32
	BufferHandle   uint32
	MeshHandle     uint32
	SamplerHandle  uint32
	PipelineHandle uint32
	AccelHandle    uint32
)

func (handle ImageHandle) kind() handleKind    { return kindImage }      // TODO: review
func (handle ImageHandle) index() uint32       { return uint32(handle) } // TODO: review
func (handle ViewHandle) kind() handleKind     { return kindView }       // TODO: review
func (handle ViewHandle) index() uint32        { return uint32(handle) } // TODO: review
func (handle BufferHandle) kind() handleKind   { return kindBuffer }     // TODO: review
func (handle BufferHandle) index() uint32      { return uint32(handle) } // TODO: review
func (handle MeshHandle) kind() handleKind     { return kindMesh }       // TODO: review
func (handle MeshHandle) index() uint32        { return uint32(handle) } // TODO: review
func (handle SamplerHandle) kind() handleKind  { return kindSampler }    // TODO: review
func (handle SamplerHandle) index() uint32     { return uint32(handle) } // TODO: review
func (handle PipelineHandle) kind() handleKind { return kindPipeline }   // TODO: review
func (handle PipelineHandle) index() uint32    { return uint32(handle) } // TODO: review
func (handle AccelHandle) kind() handleKind    { return kindAccel }      // TODO: review
func (handle AccelHandle) index() uint32       { return uint32(handle) } // TODO: review

// Kind and Index expose a handle's identity to a backend, which cannot see the
// unexported methods from its own package
func Kind(handle Handle) int     { return int(handle.kind()) } // TODO: review
func Index(handle Handle) uint32 { return handle.index() }     // TODO: review

// The kinds, for a backend's type switch
const (
	KindNone     = int(kindNone)
	KindImage    = int(kindImage)
	KindView     = int(kindView)
	KindBuffer   = int(kindBuffer)
	KindMesh     = int(kindMesh)
	KindSampler  = int(kindSampler)
	KindPipeline = int(kindPipeline)
	KindAccel    = int(kindAccel)
)

// The two views the backend owns and resizes with the window, so a pass on the
// screen is an ordinary pass with ordinary attachments
const (
	// No attachment
	NoView ViewHandle = 0
	// The swapchain image acquired for this frame. When the backend
	// multisamples, a pass on it renders into the multisampled image and
	// resolves here, which is why this is a view and not an image
	Backbuffer ViewHandle = 1
	// The depth buffer sized to the swapchain, discarded every frame
	BackbufferDepth ViewHandle = 2
)

// The swapchain image this frame is drawing into, as an ordinary image handle
//
// Reserved so a copy can name it: everything else about the backbuffer is
// reached through the Backbuffer view. Slot and Destroy do nothing with it
const BackbufferImage ImageHandle = 1

// A GPU virtual address, which is how every uniform and storage block reaches a
// shader. Frame-scoped when it came from Frame.Upload
type Address uint64
