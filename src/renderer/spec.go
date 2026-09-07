package renderer

// --- formats -----------------------------------------------------------------

// Format is what one texel or one vertex attribute holds
type Format int

const (
	FormatNone Format = iota
	FormatRGBA8
	FormatRGBA8Srgb
	FormatBGRA8
	FormatR8
	FormatR16F
	FormatRG16F
	FormatRGBA16F
	FormatR11G11B10F
	FormatR32U
	FormatR32F
	FormatRG32F
	FormatRGB32F
	FormatRGBA32F
	FormatDepth32F
	FormatBC5
	FormatBC6H
	FormatBC7
	FormatBC7Srgb
	FormatBackbuffer      // whatever format the swapchain was created with
	FormatBackbufferDepth // whatever the backend's depth buffer was created with
)

// --- images ------------------------------------------------------------------

// ImageUsage is every way an image will be reached
type ImageUsage uint32

const (
	ImageSampled ImageUsage = 1 << iota
	ImageStorage
	ImageColorAttachment
	ImageDepthAttachment
	ImageCopySrc
	ImageCopyDst
)

// ImageKind is the shape of an image and of the view that samples it
type ImageKind int

const (
	Image2D ImageKind = iota
	Image2DArray
	ImageCube
	Image3D
)

// Aspect is which plane of an image an attachment, view or copy refers to
type Aspect int

const (
	AspectColor Aspect = iota
	AspectDepth
)

// ImageSpec describes an image by what it is, never by what it is for
type ImageSpec struct {
	Name          string // debug label, and the name a validation message shows
	Width, Height int
	Depth         int // 3D only : 0 gets default value of 1
	Layers        int // 0 gets default value of 1 : a cube needs 6
	Format        Format
	Usage         ImageUsage
	Kind          ImageKind
	Samples       int // 0 means 1
	Sampler       SamplerHandle
	Hot           bool // marks an image the shaders tap many times per fragment
	HotSlot       int  // index dedicated descriptor used by the shader to access it
}

// ViewSpec narrows an image to one mip, one slice and one aspect, which is how a
// mip chain or a cube face becomes an attachment
type ViewSpec struct {
	Name       string
	Kind       ImageKind
	BaseLayer  int
	LayerCount int // 0 means all remaining
	Aspect     Aspect
}

// ImageData is CPU pixels for a whole image or one region of it
type ImageData struct {
	Pixels        []byte
	X, Y, Z       int
	Width, Height int
	Depth         int // 0 means 1
	BaseLayer     int
	LayerCount    int // 0 means 1
}

// --- buffers -----------------------------------------------------------------

// bitmask (1/2/4/8/16/32) to select buffer usage type
type BufferUsage uint32

const (
	BufferVertex   BufferUsage = 1 << iota // store vertices
	BufferIndex                            // store indices
	BufferStorage                          // SSBO (Shader Storage Buffer Object) : shader can read and write to it
	BufferIndirect                         // readable by the GPU command processor before shaders run
	BufferCopySrc                          // on top of other usage types : allow other buffer to copy from it
	BufferCopyDst                          // on top of other usage types : allow other buffer to copy to it
)

// BufferLocation is where a buffer lives, and therefore how it is written
type BufferLocation int

const (
	// cpu-visible and persistently mapped, so an update is a slower memcpy
	LocationHost BufferLocation = iota
	// device-local : faster, written through a copy and read back through one
	LocationDevice
)

type BufferSpec struct {
	Name     string
	Size     uint64
	Usage    BufferUsage
	Location BufferLocation
	Data     any // optional initial contents: a pointer to a value, or a slice
}

// --- meshes ------------------------------------------------------------------

// MeshSpec pairs a vertex buffer with this face group's index list
type MeshSpec struct {
	Name     string
	Vertices BufferHandle // several meshes share one vertex buffer
	Indices  []uint32
	// bytes per vertex, used to derive the vertex count of a non-indexed mesh
	Stride int
	// how many vertices to draw when there are no indices, 0 derives it from stride
	Count int
}

// --- samplers ----------------------------------------------------------------

type FilterType int

const (
	FilterNearest FilterType = iota
	FilterLinear
)

type OutsideMode int

const (
	OutsideRepeat OutsideMode = iota
	OutsideMirroredRepeat
	OutsideClampToEdge
	OutsideClampToBorder
)

type BorderColor int

const (
	BorderBlack BorderColor = iota
	BorderWhite
)

type SamplerSpec struct {
	Name                         string
	Mag, Min, Mipmap             FilterType // magnification, minification, mipmap
	OutsideU, OutsideV, OutsideW OutsideMode
	Border                       BorderColor
	MaxAnisotropy                float32
	MinLod, MaxLod               float32
	Compare                      CompareOperation
}

// --- pipelines ---------------------------------------------------------------

type CullMode int

const (
	CullBack CullMode = iota
	CullFront
	CullNone
)

type WindingDirection int

const (
	// Front faces wind counter-clockwise, which is what a flipped viewport leaves
	WindingCounterClockwise WindingDirection = iota
	WindingClockwise
)

// CompareOperation is a depth test or a sampler comparison
type CompareOperation int

const (
	CompareNone CompareOperation = iota // no test at all
	CompareNever
	CompareLess
	CompareEqual
	CompareLessEqual
	CompareGreater
	CompareNotEqual
	CompareGreaterEqual
	CompareAlways
)

type BlendMode int

const (
	BlendNone BlendMode = iota
	// Source alpha over destination, the overlay and transparent surfaces
	BlendAlpha
	// One-one, for a bloom chain or any accumulation pass
	BlendAdd
)

type ShaderStage int

const (
	StageVertex ShaderStage = iota
	StageFragment
	StageGeometry
	StageCompute
)

// The file suffix build_shaders.sh writes for a stage
func (spec ShaderStage) Suffix() string { // TODO: review
	switch spec {
	case StageFragment:
		return "frag"
	case StageGeometry:
		return "geo"
	case StageCompute:
		return "comp"
	default:
		return "vert"
	}
}

type PipelineKind int

const (
	PipelineGraphics PipelineKind = iota
	PipelineCompute
)

// VertexAttr is one attribute of the vertex stream a pipeline reads
type VertexAttr struct {
	Location int
	Format   Format
	Offset   int
}

// VertexLayout is the whole of one vertex binding
type VertexLayout struct {
	Stride int
	Attrs  []VertexAttr
	// Per instance rather than per vertex
	Instanced bool
}

// PipelineSpec is everything baked into one pipeline object: which shaders, how
// vertices are read, how fragments are tested and blended, and which attachment
// formats it is compatible with.
//
// Replaces the old CreateShader/SetCullMode/SetDepthCompare trio and the pass
// table that inferred winding and blending from what a target looked like.
type PipelineSpec struct {
	Name string
	Kind PipelineKind
	// The shader set in shaders/vk, e.g. "forward": each stage is loaded from
	// <Shader>.<suffix>.spv
	Shader string
	// The stages to load. Empty means vertex and fragment for a graphics
	// pipeline, compute for a compute one
	Stages []ShaderStage

	Vertex VertexLayout

	Cull         CullMode
	FrontFace    WindingDirection
	DepthCompare CompareOperation
	DepthWrite   bool
	Blend        BlendMode

	// The attachment formats this pipeline will be used with, which under
	// dynamic rendering is what it is compatible with
	ColorFormats []Format
	DepthFormat  Format
	Samples      int // 0 means 1
}

// --- passes ------------------------------------------------------------------

// Attachment is one image a pass renders into
type Attachment struct {
	View ViewHandle
	// Where the multisampled attachment resolves to, or NoView
	Resolve ViewHandle
	// Clear before the pass; nil loads what the target already holds. A depth
	// attachment reads element 0 as the depth value
	Clear *[4]float32
	// Keep the result. False lets a multisampled attachment be discarded once
	// it has been resolved
	Store bool
}

// PassSpec is a whole render pass: what it draws into, what it samples, and what
// it is called in a capture and in Timings
type PassSpec struct {
	Name string
	// Plural: a G-buffer, a velocity target, a probe capture
	Color []Attachment
	// nil for a colour-only post pass
	Depth *Attachment
	// Images this pass samples. They are transitioned before it opens, which is
	// the one thing a pass cannot learn from its own attachments
	Reads []Handle
	// 6 renders a cube probe in one pass
	Layers int
	// The negative-height viewport, which makes clip space y-up and flips
	// winding with it
	FlipY bool
}

// --- draws and dispatches ----------------------------------------------------

// IndirectRef names where a draw or dispatch reads its arguments from
type IndirectRef struct {
	Buffer BufferHandle
	Offset uint64
	Count  int
	Stride int
}

// DrawCall is one draw. Push carries whatever addresses the pipeline's shaders
// declare, positionally; the backend never looks inside them.
type DrawCall struct {
	Pipeline  PipelineHandle
	Mesh      MeshHandle
	Push      [4]Address
	Instances int // 0 and 1 both mean one
	Indirect  *IndirectRef
}

// DispatchCall is one compute dispatch. Groups is workgroups, not threads: the
// local size lives in the shader, so a 1920x1080 image at 8x8 is {240, 135, 1}.
type DispatchCall struct {
	Pipeline PipelineHandle
	Push     [4]Address
	Groups   [3]int
	Indirect *IndirectRef
}

// ComputeSpec names what a dispatch touches
//
// The one asymmetry in the design: a Pass learns what to transition from its
// attachments, but a dispatch reaches its resources through descriptors and
// device addresses, which the backend cannot inspect. So it is told.
type ComputeSpec struct {
	Name   string
	Reads  []Handle
	Writes []Handle
}

// --- copies and clears -------------------------------------------------------

// CopySpec is one image-to-image, image-to-buffer, buffer-to-image or
// buffer-to-buffer copy. Exactly one source and one destination are set.
type CopySpec struct {
	SrcImage  ImageHandle
	DstImage  ImageHandle
	SrcBuffer BufferHandle
	DstBuffer BufferHandle

	// Texels, for an image end; bytes, for a buffer end
	SrcOffset [3]int
	DstOffset [3]int
	SrcBytes  uint64
	DstBytes  uint64

	// Texels for an image copy, bytes for buffer-to-buffer (Extent[0])
	Extent [3]int

	SrcLayer, DstLayer int
	Layers             int // 0 means 1
	Aspect             Aspect
}

// ClearSpec zeroes a storage image before the compute pass that accumulates
// into it, outside any render pass
type ClearSpec struct {
	Image ImageHandle
	Color [4]float32
}

// --- capabilities ------------------------------------------------------------

// Feature is an optional capability. Caps.Features reports what the device
// actually granted, which is not always what Request asked for.
type Feature int

const (
	FeatureCompute Feature = iota
)

type Features map[Feature]bool

// Request is what Init asks the device for
type Request struct {
	Features []Feature
}

type Capacities struct {
	MaxAnisotropy float32
	// Sample counts the device can attach, as a bitmask of counts
	SampleCounts int
	// What the backbuffer actually rasterises at, which a pipeline drawn into
	// it has to match
	BackbufferSamples int
	// Whether a format can be sampled and attached on this device
	Formats  func(Format) bool
	Features Features
}
