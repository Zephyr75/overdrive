package main

import (
	"fmt"
	_ "image/png"
	"log"
	"os"
	"runtime"
	"strconv"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/raedatoui/glfont"
	"github.com/raedatoui/glutils"
	"github.com/raedatoui/learn-opengl-golang/sections"
	"github.com/raedatoui/learn-opengl-golang/sections/getstarted"
	"github.com/raedatoui/learn-opengl-golang/sections/lighting"
	"github.com/raedatoui/learn-opengl-golang/sections/modelloading"
)

var (
	currentSlide sections.Slide
	slides       []sections.Slide
	covers       map[int]sections.Slide
	slideIndex   = 0
	window       *glfw.Window
	font         *glfont.Font
	keys         map[glfw.Key]bool
	wireframe    int32
)

func init() {
	// This is needed to arrange that main() runs on main thread.
	// See documentation for functions that are only allowed to be called from the main thread.
	runtime.LockOSThread()
}

func init() {
	dir, err := glutils.ImportPathToDir("github.com/raedatoui/learn-opengl-golang")
	if err != nil {
		log.Fatalln("Unable to find Go package in your GOPATH, it's needed to load assets:", err)
	}
	if err := os.Chdir(dir); err != nil {
		log.Panicln("os.Chdir:", err)
	}
}

func keyCallBack(w *glfw.Window, k glfw.Key, s int, a glfw.Action, mk glfw.ModifierKey) {
	if a == glfw.Press {
		if k == glfw.KeyEscape {
			window.SetShouldClose(true)
		}
		if k == glfw.KeySpace {
			gl.GetIntegerv(gl.POLYGON_MODE, &wireframe)
			switch wireframe {
			case gl.FILL:
				gl.PolygonMode(gl.FRONT_AND_BACK, gl.LINE)
			case gl.LINE:
				gl.PolygonMode(gl.FRONT_AND_BACK, gl.POINT)
				gl.PointSize(20.0)
			case gl.POINT:
				gl.PolygonMode(gl.FRONT_AND_BACK, gl.FILL)
			}
		}

		if k >= glfw.Key0 && k <= glfw.Key9 {
			c := int(k) - 48
			if c < len(covers) {
				slideIndex = sections.SlidePosition(slides, covers[c])
				currentSlide.Close()
				currentSlide = slides[slideIndex]
				if err := currentSlide.InitGL(); err != nil {
					log.Fatalf("slide failed %v: ", err)
				}
				return
			}
		}

		newIndex := slideIndex
		if s == 124 {
			newIndex = slideIndex + 1
			if newIndex > len(slides)-1 {
				newIndex = len(slides) - 1
			}
		}
		if s == 123 {
			newIndex = slideIndex - 1
			if newIndex < 0 {
				newIndex = 0
			}
		}
		if newIndex != slideIndex {
			slideIndex = newIndex
			currentSlide.Close()
			currentSlide = slides[newIndex]
			if err := currentSlide.InitGL(); err != nil {
				log.Fatalf("slide failed %v: ", err)
			}
			return
		}
		keys[k] = true

	} else if a == glfw.Release {
		keys[k] = false
	}

	if currentSlide != nil {
		currentSlide.HandleKeyboard(k, s, a, mk, keys)
	}

}

func mouseCallback(w *glfw.Window, xpos float64, ypos float64) {
	if currentSlide != nil {
		currentSlide.HandleMousePosition(xpos, ypos)
	}
}

func scrollCallback(w *glfw.Window, xoff float64, yoff float64) {
	if currentSlide != nil {
		currentSlide.HandleScroll(xoff, yoff)
	}
}

func resizeCallback(w *glfw.Window, width int, height int) {
	sections.WIDTH = float64(width)
	sections.HEIGHT = float64(height)
	sections.Ratio = float32(sections.WIDTH / sections.HEIGHT)
	font.Resize(sections.WIDTH, sections.HEIGHT)
	gl.Viewport(0, 0, int32(width), int32(height))
}

func fileDropCallback(w *glfw.Window, names []string) {
	if currentSlide != nil {
		currentSlide.HandleFiles(names)
	}
}

func setup() (*glfw.Window, error) {
	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, gl.TRUE)

	window, err := glfw.CreateWindow(int(sections.WIDTH), int(sections.HEIGHT), "learnopengl.com in Golang", nil, nil)
	if err != nil {
		return nil, err
	}
	window.MakeContextCurrent()

	// Initialize Glow - this is the equivalent of glew
	if err := gl.Init(); err != nil {
		return nil, err
	}

	// Resize Callback
	window.SetFramebufferSizeCallback(resizeCallback)

	//Keyboard Callback
	window.SetKeyCallback(keyCallBack)
	window.SetCursorPosCallback(mouseCallback)
	window.SetScrollCallback(scrollCallback)

	// File Drag n Drop
	window.SetDropCallback(fileDropCallback)
	version := gl.GoStr(gl.GetString(gl.VERSION))
	glsl := gl.GoStr(gl.GetString(gl.SHADING_LANGUAGE_VERSION))
	fmt.Println("OpenGL version", version, glsl)

	width, height := window.GetFramebufferSize()
	gl.Viewport(0, 0, int32(width), int32(height))

	return window, nil
}

func setupSlides() []sections.Slide {
	// make a slice of pointers to sketch instances
	return []sections.Slide{
		new(sections.TitleSlide),
		new(getstarted.HelloCube),
		new(sections.TitleSlide),
		new(getstarted.HelloWindow),

		new(getstarted.HelloTriangle),
		new(getstarted.TriangleEx1),
		new(getstarted.TriangleEx2),

		new(getstarted.HelloShaders),
		new(getstarted.ShaderEx1),
		new(getstarted.ShaderEx2),
		new(getstarted.ShaderEx3),
		new(getstarted.ShaderEx4),

		new(getstarted.HelloTextures),
		new(getstarted.TexturesEx1),
		new(getstarted.TexturesEx2),
		new(getstarted.TexturesEx3),
		new(getstarted.TexturesEx4),

		new(getstarted.HelloTransformations),
		new(getstarted.TransformationEx1),
		new(getstarted.TransformationEx2),

		new(getstarted.HelloCoordinates),
		new(getstarted.HelloCamera),

		new(sections.TitleSlide),
		new(lighting.LightingColors),
		new(lighting.BasicSpecular),
		new(lighting.Materials),

		new(sections.TitleSlide),
		new(modelloading.ModelLoading),

		new(sections.TitleSlide),
		new(sections.TitleSlide),
		new(sections.TitleSlide),
		new(sections.TitleSlide),
	}
}

func main() {
	// init GLFW
	if err := glfw.Init(); err != nil {
		log.Fatalln("failed to initialize glfw:", err)
	}
	defer glfw.Terminate()

	keys = make(map[glfw.Key]bool)

	// create window
	w, err := setup()
	if err != nil {
		log.Fatalf("cant create window %v", err)
	}
	window = w

	//load font (fontfile, font scale, window width, window height
	f, err := glfont.LoadFont("_assets/fonts/huge_agb_v5.ttf", int32(52), sections.WIDTH, sections.HEIGHT)
	if err != nil {
		log.Fatalf("LoadFont: %v", err)
	}
	font = f
	c := glutils.White.To32()
	font.SetColor(c.R, c.G, c.B, 1.0)

	slides = setupSlides()
	l := len(slides)
	covers = make(map[int]sections.Slide)
	titles := []string{
		"LearnOpenGL in Go\n \n" +
			"0. Test installation using go-gl\n" +
			"1. Getting Started\n" +
			"2. Lighting\n" +
			"3. Model Loading\n" +
			"4. Advanced OpenGL\n" +
			"5. Advanced Lighting\n" +
			"6. PBR\n" +
			"7. In Practice",
		"Section 1: Getting Started",
		"Section 2: Lighting",
		"Section 3: Model Loading",
		"Section 4: Advanced OpenGL",
		"Section 5: Advanced Lighting",
		"Section 6: PBR",
		"Section 7: In Practice",
	}
	count := 0
	titleSlideType := &sections.TitleSlide{}
	for x, slide := range slides {
		c := glutils.StepColor(glutils.Magenta, glutils.Black, l, x)

		if glutils.IsType(slide, titleSlideType) {
			covers[count] = slide
			if err := slide.Init(f, c, titles[count]); err != nil {
				log.Fatalf("Failed setting up sketch: %v", err)
			}
			count += 1
		} else {
			if err := slide.Init(f, c); err != nil {
				log.Fatalf("Failed setting up sketch: %v", err)
			}
		}
	}

	currentSlide = slides[0]

	if err := currentSlide.InitGL(); err != nil {
		log.Fatalf("Failed initializing GL for slide: %v", err)
	}

	// TODO: do we always need to enabled the depth test?
	gl.Enable(gl.DEPTH_TEST)
	//gl.DepthFunc(gl.LESS)

	// TODO: how to effectively blend shapes that use a frag shader with
	// TODO: transparency
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	var maxAttrib int32
	gl.GetIntegerv(gl.MAX_VERTEX_ATTRIBS, &maxAttrib)

	glutils.InitFPS()

	// loop
	for !window.ShouldClose() {

		// Update
		currentSlide.Update()

		//Render
		currentSlide.Draw()
		if currentSlide.DrawText() {
			font.Printf(30, 30, 0.5, currentSlide.GetHeader())
			if currentSlide.GetSubHeader() != "" {
				font.Printf(30, 50, 0.3, currentSlide.GetSubHeader())
			}
		}

		font.Printf(30, float32(sections.HEIGHT)-20, 0.2, currentSlide.GetColorHex())
		fps := "FPS: " + strconv.FormatFloat(glutils.CalcFPS(1.0), 'f', 2, 64)
		font.Printf(float32(sections.WIDTH)-80, float32(sections.HEIGHT)-20, 0.25, fps)

		window.SwapBuffers()
		// Poll Events
		glfw.PollEvents()
	}
	currentSlide.Close()

}
