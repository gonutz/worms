package main

import (
	"image"
	"runtime"
	"time"
	"unsafe"

	"github.com/gonutz/d3d9"
	"github.com/gonutz/tic"
	"github.com/gonutz/w32"
	"github.com/gonutz/win"
	"github.com/gonutz/xcf"
)

func main() {
	defer win.HandlePanics("worms")
	runtime.LockOSThread()
	win.HideConsoleWindow()

	var updatePartialTexture func() // TODO remove this debug function

	opts := win.DefaultOptions()
	opts.Title = "Worms"
	var msg win.MessageHandler
	window, err := win.NewWindow(opts, msg.Callback)
	check(err)
	var windowedPlacement w32.WINDOWPLACEMENT
	msg.OnKeyDown = func(key uintptr, opt win.KeyOptions) {
		switch key {
		case w32.VK_SPACE:
			if !opt.WasDown() {
				updatePartialTexture()
			}
		case w32.VK_F11:
			if win.IsFullscreen(window) {
				win.DisableFullscreen(window, windowedPlacement)
			} else {
				windowedPlacement = win.EnableFullscreen(window)
			}
		case w32.VK_ESCAPE:
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

	const (
		vertexFmt       = d3d9.FVF_XYZRHW | d3d9.FVF_TEX1
		floatsPerVertex = 6
	)
	check(device.SetFVF(vertexFmt))

	// load level
	levelCanvas, err := xcf.LoadFromFile("map.xcf")
	check(err)

	// create a back buffer texture to render everything to pixel-perfectly,
	// then stretch-blit that onto the actual backbuffer with some good-looking
	// interpolation
	backbuf, err := device.CreateTexture(
		uint(levelCanvas.Width),
		uint(levelCanvas.Height),
		1,
		d3d9.USAGE_RENDERTARGET,
		d3d9.FMT_A8R8G8B8,
		d3d9.POOL_DEFAULT,
		0,
	)
	check(err)
	defer backbuf.Release()

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

	updatePartialTexture = func() {
		defer tic.Toc()("update texture")
		const (
			left   = 100
			top    = 90
			radius = 21
		)
		for x := -radius; x <= radius; x++ {
			for y := -radius; y <= radius; y++ {
				if x*x+y*y <= radius*radius {
					i := level.PixOffset(left+x, top+y)
					if level.Pix[i+3] < 50 {
						level.Pix[i+3] = 0
					} else {
						level.Pix[i+3] -= 50
					}
				}
			}
		}
		updatePartialRect(
			levelTex, level,
			left-radius, top-radius, 2*radius+1, 2*radius+1,
		)
	}

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
	scale := 1.0
	win.RunMainGameLoop(func() {
		time.Sleep(0)

		backBufSurface, err := backbuf.GetSurfaceLevel(0)
		check(err)
		defer backBufSurface.Release()

		check(device.SetSamplerState(0, d3d9.SAMP_MINFILTER, d3d9.TEXF_NONE))
		check(device.SetSamplerState(0, d3d9.SAMP_MAGFILTER, d3d9.TEXF_NONE))
		check(device.SetRenderTarget(0, backBufSurface))

		device.Clear(nil, d3d9.CLEAR_TARGET, d3d9.ColorRGB(50, 100, 200), 1, 0)
		device.BeginScene()

		renderTex(
			device, backTex,
			0, 0,
			background.Bounds().Dx(), background.Bounds().Dy(),
		)
		renderTex(
			device, levelTex,
			0, 0,
			level.Bounds().Dx(), level.Bounds().Dy(),
		)

		check(device.SetSamplerState(0, d3d9.SAMP_MINFILTER, d3d9.TEXF_LINEAR))
		check(device.SetSamplerState(0, d3d9.SAMP_MAGFILTER, d3d9.TEXF_LINEAR))
		bb, err := device.GetBackBuffer(0, 0, d3d9.BACKBUFFER_TYPE_MONO)
		check(err)
		defer bb.Release()
		check(device.SetRenderTarget(0, bb))
		scale *= 1.005
		renderTex(device, backbuf, 0, 0, int(scale*400+0.5), int(scale*200+0.5))

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

func updatePartialRect(tex *d3d9.Texture, img *image.RGBA, left, top, width, height int) error {
	rect, err := tex.LockRect(
		0,
		&d3d9.RECT{
			Left:   int32(left),
			Top:    int32(top),
			Right:  int32(left + width),
			Bottom: int32(top + height),
		},
		d3d9.LOCK_DISCARD,
	)
	if err != nil {
		return err
	}
	rect.SetAllBytes(
		img.Pix[img.PixOffset(left, top):img.PixOffset(left+width, top+height-1)],
		img.Stride,
	)
	return tex.UnlockRect(0)
}
