package main

import (
	"image"
	"runtime"
	"time"
	"unsafe"

	"github.com/gonutz/d3d9"
	"github.com/gonutz/w32"
	"github.com/gonutz/win"
	"github.com/gonutz/xcf"
)

func main() {
	defer win.HandlePanics("worms")
	runtime.LockOSThread()
	win.HideConsoleWindow()

	opts := win.DefaultOptions()
	opts.Title = "Worms"
	var msg win.MessageHandler
	window, err := win.NewWindow(opts, msg.Callback)
	check(err)
	msg.OnKeyDown = func(key uintptr, _ win.KeyOptions) {
		if key == w32.VK_ESCAPE {
			win.CloseWindow(window)
		}
	}

	// set up D3D9
	d3d, err := d3d9.Create(d3d9.SDK_VERSION)
	check(err)
	defer d3d.Release()

	var createFlags uint32 = d3d9.CREATE_SOFTWARE_VERTEXPROCESSING
	caps, err := d3d.GetDeviceCaps(d3d9.ADAPTER_DEFAULT, d3d9.DEVTYPE_HAL)
	if err == nil &&
		caps.DevCaps&d3d9.DEVCAPS_HWTRANSFORMANDLIGHT != 0 {
		createFlags = d3d9.CREATE_HARDWARE_VERTEXPROCESSING
	}
	windowW, windowH := win.ClientSize(window)
	presentParameters := d3d9.PRESENT_PARAMETERS{
		Windowed:         1,
		HDeviceWindow:    d3d9.HWND(window),
		SwapEffect:       d3d9.SWAPEFFECT_COPY, // so Present can use rects
		BackBufferWidth:  uint32(windowW),
		BackBufferHeight: uint32(windowH),
		BackBufferFormat: d3d9.FMT_UNKNOWN,
		BackBufferCount:  1,
	}
	device, actualPP, err := d3d.CreateDevice(
		d3d9.ADAPTER_DEFAULT,
		d3d9.DEVTYPE_HAL,
		d3d9.HWND(window),
		createFlags,
		presentParameters,
	)
	presentParameters = actualPP
	check(err)
	defer device.Release()

	check(device.SetRenderState(d3d9.RS_CULLMODE, d3d9.CULL_CW))
	check(device.SetRenderState(d3d9.RS_ALPHATESTENABLE, 0))
	check(device.SetRenderState(d3d9.RS_ALPHABLENDENABLE, 1))
	check(device.SetRenderState(d3d9.RS_SRCBLEND, d3d9.BLEND_SRCALPHA))
	check(device.SetRenderState(d3d9.RS_DESTBLEND, d3d9.BLEND_INVSRCALPHA))
	//// TODO does this work?
	//check(device.SetTextureStageState(0, d3d9.TSS_COLOROP, d3d9.TOP_MODULATE))
	//check(device.SetTextureStageState(0, d3d9.TSS_COLORARG1, d3d9.TA_TEXTURE))
	//check(device.SetTextureStageState(0, d3d9.TSS_COLORARG2, d3d9.TA_CURRENT))
	//check(device.SetTextureStageState(0, d3d9.TSS_ALPHAOP, d3d9.TOP_MODULATE))
	//check(device.SetTextureStageState(0, d3d9.TSS_ALPHAARG1, d3d9.TA_CURRENT))
	//check(device.SetTextureStageState(0, d3d9.TSS_ALPHAARG2, d3d9.TA_TEXTURE))
	//

	const (
		vertexFmt       = d3d9.FVF_XYZRHW | d3d9.FVF_TEX1
		floatsPerVertex = 6
	)
	check(device.SetFVF(vertexFmt))

	// load level
	levelCanvas, err := xcf.LoadFromFile("map.xcf")
	check(err)

	background := levelCanvas.GetLayerByName("background").RGBA
	swapRB(background)
	backTex, err := rgbaToTexture(device, background)
	check(err)
	defer backTex.Release()

	level := levelCanvas.GetLayerByName("level").RGBA
	swapRB(level)
	levelTex, err := rgbaToTexture(device, level)
	check(err)
	defer levelTex.Release()

	foreground := levelCanvas.GetLayerByName("foreground").RGBA
	swapRB(foreground)
	foreTex, err := rgbaToTexture(device, foreground)
	check(err)
	defer foreTex.Release()

	renderTex := func(device *d3d9.Device, tex *d3d9.Texture, x, y, width, height int) {
		device.SetTexture(0, tex)
		w := float32(width)
		h := float32(height)
		v := []float32{
			// x, y, z, w, u, v
			0 - 0.5, h - 0.5, 0, 1, 0, 1,
			w - 0.5, h - 0.5, 0, 1, 1, 1,
			0 - 0.5, 0 - 0.5, 0, 1, 0, 0,
			w - 0.5, 0 - 0.5, 0, 1, 1, 0,
		}
		device.DrawPrimitiveUP(
			d3d9.PT_TRIANGLESTRIP,
			2,
			uintptr(unsafe.Pointer(&v[0])),
			floatsPerVertex*4,
		)
	}

	// run main game loop
	win.RunMainGameLoop(func() {
		time.Sleep(0)

		device.Clear(nil, d3d9.CLEAR_TARGET, d3d9.ColorRGB(50, 100, 200), 1, 0)
		device.BeginScene()

		scale := 3
		renderTex(
			device, backTex,
			0, 0,
			scale*background.Bounds().Dx(), scale*background.Bounds().Dy(),
		)
		renderTex(
			device, levelTex,
			0, 0,
			scale*level.Bounds().Dx(), scale*level.Bounds().Dy(),
		)
		renderTex(
			device, foreTex,
			0, 0,
			scale*foreground.Bounds().Dx(), scale*foreground.Bounds().Dy(),
		)

		device.EndScene()
		device.Present(nil, nil, 0, nil)
	})
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func swapRB(img *image.RGBA) {
	p := img.Pix
	for i := 0; i < len(p); i += 4 {
		p[i+0], p[i+2] = p[i+2], p[i+0]
	}
}

func rgbaToTexture(device *d3d9.Device, img *image.RGBA) (*d3d9.Texture, error) {
	tex, err := device.CreateTexture(
		uint(img.Bounds().Dx()),
		uint(img.Bounds().Dy()),
		1,
		d3d9.USAGE_SOFTWAREPROCESSING,
		d3d9.FMT_A8R8G8B8,
		d3d9.POOL_MANAGED,
		0,
	)
	if err != nil {
		return nil, err
	}
	mem, err := tex.LockRect(0, nil, d3d9.LOCK_DISCARD)
	if err != nil {
		return nil, err
	}
	mem.SetAllBytes(img.Pix, img.Stride)
	err = tex.UnlockRect(0)
	if err != nil {
		return nil, err
	}
	return tex, nil
}