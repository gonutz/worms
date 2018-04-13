package main

import (
	"image"
	"runtime"
	"sort"
	"time"
	"unsafe"

	"github.com/gonutz/d3d9"
	"github.com/gonutz/tic"
	"github.com/gonutz/w32"
	"github.com/gonutz/win"
	"github.com/gonutz/xcf"
)

const debug = true

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
				if x*x+y*y <= radius*radius+1 {
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
		xf := float32(x)
		yf := float32(y)
		w := float32(width)
		h := float32(height)
		v := []float32{
			// x, y, z, w, u, v
			xf + 0 - 0.5, yf + h - 0.5, 0, 1, 0, 1,
			xf + w - 0.5, yf + h - 0.5, 0, 1, 1, 1,
			xf + 0 - 0.5, yf + 0 - 0.5, 0, 1, 0, 0,
			xf + w - 0.5, yf + 0 - 0.5, 0, 1, 1, 0,
		}
		device.DrawPrimitiveUP(
			d3d9.PT_TRIANGLESTRIP,
			2,
			uintptr(unsafe.Pointer(&v[0])),
			floatsPerVertex*4,
		)
	}

	// create worm
	worm := parseWorm("worm.xcf")
	swapRB(worm.left)
	swapRB(worm.right)
	swapRB(worm.hitboxImg)
	worm.leftTex, err = rgbaToTexture(device, worm.left)
	check(err)
	defer worm.leftTex.Release()
	worm.rightTex, err = rgbaToTexture(device, worm.right)
	check(err)
	defer worm.rightTex.Release()
	{
		p := worm.hitboxImg.Pix
		for i := 3; i < len(p); i += 4 {
			p[i] /= 4
		}
	}
	worm.hitboxTex, err = rgbaToTexture(device, worm.hitboxImg)
	check(err)
	defer worm.hitboxTex.Release()
	worm.x, worm.y = 50, 15
	dropWorm(&worm, level)

	msg.OnKeyDown = func(key uintptr, opt win.KeyOptions) {
		if opt.WasDown() {
			//return
		}

		switch key {
		case w32.VK_RIGHT:
			moveHor(&worm, level, 1)
		case w32.VK_LEFT:
			moveHor(&worm, level, -2)
		case w32.VK_UP:
			moveWorm(&worm, level, 0, -1)
		case w32.VK_DOWN:
			moveWorm(&worm, level, 0, 1)
		case w32.VK_SPACE:
			updatePartialTexture()
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

	// run main game loop
	scale := 1.0
	win.RunMainGameLoop(func() {
		time.Sleep(0)

		// move the worm
		if !worm.onGround {
			worm.fallSpeed++
		}
		dy := worm.fallSpeed
		for dy > 0 {
			worm.y++
			if collides(&worm, level) {
				worm.y--
				worm.onGround = true
				break
			}
		}

		// render everything
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
		if worm.facingLeft {
			renderTex(
				device, worm.leftTex,
				worm.x, worm.y,
				worm.left.Bounds().Dx(), worm.left.Bounds().Dy(),
			)
		} else {
			renderTex(
				device, worm.rightTex,
				worm.x, worm.y,
				worm.right.Bounds().Dx(), worm.right.Bounds().Dy(),
			)
		}
		if debug {
			renderTex(
				device, worm.hitboxTex,
				worm.x, worm.y,
				worm.left.Bounds().Dx(), worm.left.Bounds().Dy(),
			)
		}

		check(device.SetSamplerState(0, d3d9.SAMP_ADDRESSU, d3d9.TADDRESS_BORDER))
		check(device.SetSamplerState(0, d3d9.SAMP_ADDRESSV, d3d9.TADDRESS_BORDER))
		check(device.SetSamplerState(0, d3d9.SAMP_MINFILTER, d3d9.TEXF_LINEAR))
		check(device.SetSamplerState(0, d3d9.SAMP_MAGFILTER, d3d9.TEXF_LINEAR))
		bb, err := device.GetBackBuffer(0, 0, d3d9.BACKBUFFER_TYPE_MONO)
		check(err)
		defer bb.Release()
		check(device.SetRenderTarget(0, bb))
		scale = 3.5
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

type worm struct {
	hitbox            []point
	hitboxImg         *image.RGBA
	hitboxTex         *d3d9.Texture
	left, right       *image.RGBA
	leftTex, rightTex *d3d9.Texture
	x, y              int
	facingLeft        bool
	onGround          bool
	fallSpeed         int
}

func parseWorm(xcfFile string) worm {
	canvas, err := xcf.LoadFromFile(xcfFile)
	check(err)
	return worm{
		hitbox:    parseHitbox(canvas.GetLayerByName("hitbox")),
		hitboxImg: canvas.GetLayerByName("hitbox").RGBA,
		left:      canvas.GetLayerByName("worm left").RGBA,
		right:     canvas.GetLayerByName("worm right").RGBA,
	}
}

func parseHitbox(img *xcf.Layer) []point {
	outline := make(map[point]bool)

	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y).A > 0 {
				outline[pt(x, y)] = true
				break
			}
		}

		for x := b.Max.X - 1; x >= b.Min.X; x-- {
			if img.RGBAAt(x, y).A > 0 {
				outline[pt(x, y)] = true
				break
			}
		}
	}

	for x := b.Min.X; x < b.Max.X; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if img.RGBAAt(x, y).A > 0 {
				outline[pt(x, y)] = true
				break
			}
		}

		for y := b.Max.Y - 1; y >= b.Min.Y; y-- {
			if img.RGBAAt(x, y).A > 0 {
				outline[pt(x, y)] = true
				break
			}
		}
	}

	points := make([]point, 0, len(outline))
	for p, _ := range outline {
		points = append(points, p)
	}
	sort.Sort(byX(points))
	sort.Stable(byY(points))

	return points
}

func collides(w *worm, level *image.RGBA) bool {
	for _, p := range w.hitbox {
		if level.RGBAAt(w.x+p.x, w.y+p.y).A > 127 {
			return true
		}
	}
	return false
}

func dropWorm(w *worm, level *image.RGBA) {
	for w.y+w.left.Bounds().Dy() > 0 && collides(w, level) {
		w.y--
	}
	for w.y < level.Bounds().Dy() && !collides(w, level) {
		w.y++
	}
	w.y--
	w.onGround = true
}

func moveHor(w *worm, level *image.RGBA, dx int) {
	if !w.onGround || dx == 0 {
		return
	}

	if dx > 0 {
		w.facingLeft = false
	}
	if dx < 0 {
		w.facingLeft = true
	}

	dxSquare := square(dx) // remember this, dx will be changed in the for-loop
	origX, origY := w.x, w.y
	step := dx / abs(dx) // 1 or -1
	for dx != 0 {
		// step one pixel to the side
		dx -= step
		w.x += step
		if collides(w, level) {
			// if we hit something, this might be a wall that cannot be overcome
			// or it is just a one or two pixel step that we can climb
			w.y--
			if !collides(w, level) {
				// fine, move one pixel up
			} else {
				w.y--
				if !collides(w, level) {
					// fine, move twp pixels up
				} else {
					// we hit a wall, do not move up, go back one step and stay
					// there
					w.y += 2
					w.x -= step
					return
				}
			}
		} else {
			// if we hit nothing, we might not stand on the ground anymore
			w.y++
			if collides(w, level) {
				// we are still on the ground, everything is fine, keep moving
				// horizontally
				w.y--
			} else {
				w.y++
				if collides(w, level) {
					// we are still on the ground, everything is fine, keep
					// moving horizontally, this means 2 pixels step-down
					w.y--
				} else {
					// we are not on the ground anymore, we are falling
					w.y -= 2
					w.onGround = false
					w.fallSpeed = 0
				}
			}
		}
		// if we already travelled dx or more, quit here so we do not move
		// insanely fast up-hill or down-hill
		if square(w.x-origX)+square(w.y-origY) >= dxSquare {
			break
		}
	}
}

func moveWorm(w *worm, level *image.RGBA, dx, dy int) {
	if dx < 0 {
		w.facingLeft = true
	}
	if dx > 0 {
		w.facingLeft = false
	}
	x, y := w.x+dx, w.y+dy
	for _, p := range w.hitbox {
		if level.RGBAAt(x+p.x, y+p.y).A > 127 {
			return
		}
	}
	w.x, w.y = x, y
}
