// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"image"
	"image/png"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"
	"wuf-translate/watchdog-go/internal/core"
)

func TestPanelPositionAnchorsToIcon(t *testing.T) {
	for _, tc := range []struct {
		name         string
		icon, work   rect
		wantX, wantY int32
	}{
		{"bottom icon", rect{990, 1050, 1010, 1070}, rect{0, 0, 1920, 1040}, 828, 591},
		{"right edge", rect{1890, 1050, 1910, 1070}, rect{0, 0, 1920, 1040}, 1575, 591},
		{"top taskbar", rect{990, 0, 1010, 20}, rect{0, 40, 1920, 1080}, 828, 40},
		{"negative monitor", rect{-1010, 1050, -990, 1070}, rect{-1920, 0, 0, 1040}, -1172, 591},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y := panelPosition(tc.icon, tc.work, 345, 449, 8)
			if x != tc.wantX || y != tc.wantY {
				t.Fatalf("position (%d,%d), want (%d,%d)", x, y, tc.wantX, tc.wantY)
			}
		})
	}
}

// Paint this application's own view into a memory bitmap, never a desktop capture.
func TestNativePanelOpaquePaintingAndStableRefresh(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	c := &controller{client: NewClient("http://127.0.0.1:8080"), updated: time.Now()}
	c.data = &Snapshot{Snapshot: core.Snapshot{Status: "running", CurrentModel: "Riva-Translate-4B-Instruct-v2-Q4_K_M", IdleTimeout: 180, IdleRemaining: 167, AutoSleepEnabled: true, Models: []core.Model{{ID: "riva"}}}, Decision: core.DecisionSnapshot{Status: "sleeping", Models: []core.Model{{ID: "laya"}}, RuntimeAvailable: true}}
	if err := c.ensurePanel(); err != nil {
		t.Fatal(err)
	}
	p := c.panel
	defer p.close()
	// Hidden windows discard invalidations. Keep our test widget offscreen and
	// unactivated so GetUpdateRect observes real native repaint requests.
	offscreen := int32(-32000)
	user32.NewProc("MoveWindow").Call(p.hwnd, uintptr(offscreen), uintptr(offscreen), uintptr(p.px(392)), uintptr(p.px(510)), 0)
	user32.NewProc("ShowWindow").Call(p.hwnd, 4)
	p.refresh()
	for _, control := range p.controls {
		user32.NewProc("ValidateRect").Call(control.hwnd, 0)
	}
	p.refresh()
	for id, control := range p.controls {
		if pending, _, _ := user32.NewProc("GetUpdateRect").Call(control.hwnd, 0, 0); pending != 0 {
			t.Fatalf("unchanged control %d repainted", id)
		}
	}
	c.data.AutoSleepEnabled = false
	p.refresh()
	if pending, _, _ := user32.NewProc("GetUpdateRect").Call(p.controls[21].hwnd, 0, 0); pending == 0 {
		t.Fatal("auto-sleep checkmark did not repaint after toggling")
	}
	c.data.AutoSleepEnabled = true
	p.refresh()
	width, height := p.px(392), p.px(510)
	p.roundCorners(width, height)
	region, _, _ := gdi32.NewProc("CreateRectRgn").Call(0, 0, 0, 0)
	defer deleteObject.Call(region)
	if kind, _, _ := user32.NewProc("GetWindowRgn").Call(p.hwnd, region); kind == 0 {
		t.Fatal("rounded window region missing")
	}
	if inside, _, _ := gdi32.NewProc("PtInRegion").Call(region, 0, 0); inside != 0 {
		t.Fatal("window corner is not clipped")
	}
	if inside, _, _ := gdi32.NewProc("PtInRegion").Call(region, uintptr(width/2), uintptr(height/2)); inside == 0 {
		t.Fatal("window center was clipped")
	}
	if width >= 360 || height >= 470 {
		t.Fatal("panel is not compact", width, height)
	}
	dc, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(0)
	defer gdi32.NewProc("DeleteDC").Call(dc)
	header := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, ImageSize uint32
		Xppm, Yppm             int32
		Used, Important        uint32
	}{Width: width, Height: -height, Planes: 1, BitCount: 32}
	header.Size = uint32(unsafe.Sizeof(header))
	var bits uintptr
	bitmap, _, err := gdi32.NewProc("CreateDIBSection").Call(dc, pointer(&header), 0, pointer(&bits), 0, 0)
	if bitmap == 0 {
		t.Fatal(err)
	}
	defer deleteObject.Call(bitmap)
	old, _, _ := selectObject.Call(dc, bitmap)
	defer selectObject.Call(dc, old)
	p.background(dc, rect{0, 0, width, height})
	for id, control := range p.controls {
		gdi32.NewProc("SetViewportOrgEx").Call(dc, uintptr(p.px(control.x)), uintptr(p.px(control.y)), 0)
		p.drawControl(&drawItem{ID: id, DC: dc, Rect: rect{0, 0, p.px(control.w), p.px(control.h)}})
	}
	gdi32.NewProc("SetViewportOrgEx").Call(dc, 0, 0, 0)
	gdi32.NewProc("GdiFlush").Call()
	pixels := make([]byte, int(width*height*4))
	kernel32.NewProc("RtlMoveMemory").Call(pointer(&pixels[0]), bits, uintptr(len(pixels)))
	for _, point := range []point{{1, 1}, {width - 2, height - 2}, {width / 2, p.px(150)}} {
		offset := (point.Y*width + point.X) * 4
		if pixels[offset] != 30 || pixels[offset+1] != 28 || pixels[offset+2] != 28 {
			t.Fatal("background does not match #1c1c1e", point)
		}
	}
	if path := os.Getenv("WUF_PANEL_PREVIEW"); path != "" {
		img := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
		for i := 0; i < len(pixels); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], 255
		}
		output, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer output.Close()
		if err := png.Encode(output, img); err != nil {
			t.Fatal(err)
		}
	}
}
