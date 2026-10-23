// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var gdi32 = syscall.NewLazyDLL("gdi32.dll")
var selectObject = gdi32.NewProc("SelectObject")
var deleteObject = gdi32.NewProc("DeleteObject")
var fillRect = user32.NewProc("FillRect")
var invalidate = user32.NewProc("InvalidateRect")

const panelBackground uintptr = 0x1e1c1c // #1c1c1e, opaque COLORREF

type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	DC              uintptr
	Erase           int32
	Paint           rect
	Restore, Update int32
	Reserved        [32]byte
}
type drawItem struct {
	Type, ID, ItemID, Action, State uint32
	Window, DC                      uintptr
	Rect                            rect
	Data                            uintptr
}
type monitorInfo struct {
	Size          uint32
	Monitor, Work rect
	Flags         uint32
}
type panelControl struct {
	hwnd       uintptr
	x, y, w, h int32
	text       string
	enabled    bool
}
type nativePanel struct {
	owner       *controller
	hwnd        uintptr
	brush       uintptr
	dpi         uint32
	controls    map[uint32]*panelControl
	fonts       map[string]uintptr
	view        panelView
	tracking    bool
	hiding      bool
	hover       uint32
	icon        rect
	iconDismiss time.Time
}

func (p *nativePanel) px(n int32) int32 { return (n*int32(p.dpi)*88 + 4800) / 9600 }
func color(r, g, b byte) uintptr        { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }
func paintColor(dc uintptr, area rect, value uintptr) {
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(value)
	fillRect.Call(dc, pointer(&area), brush)
	deleteObject.Call(brush)
}
func roundColor(dc uintptr, area rect, radius int32, value uintptr) {
	brush, _, _ := gdi32.NewProc("CreateSolidBrush").Call(value)
	oldBrush, _, _ := selectObject.Call(dc, brush)
	pen, _, _ := gdi32.NewProc("GetStockObject").Call(8) // NULL_PEN
	oldPen, _, _ := selectObject.Call(dc, pen)
	gdi32.NewProc("RoundRect").Call(dc, uintptr(area.Left), uintptr(area.Top), uintptr(area.Right), uintptr(area.Bottom), uintptr(radius), uintptr(radius))
	selectObject.Call(dc, oldPen)
	selectObject.Call(dc, oldBrush)
	deleteObject.Call(brush)
}
func (p *nativePanel) text(dc uintptr, value string, area rect, font string, ink uintptr, flags uintptr) {
	old, _, _ := selectObject.Call(dc, p.fonts[font])
	defer selectObject.Call(dc, old)
	gdi32.NewProc("SetBkMode").Call(dc, 1)
	gdi32.NewProc("SetTextColor").Call(dc, ink)
	user32.NewProc("DrawTextW").Call(dc, pointer(wide(value)), ^uintptr(0), pointer(&area), 0x20|0x800|0x8000|flags)
}
func (p *nativePanel) createFonts() {
	for _, font := range p.fonts {
		deleteObject.Call(font)
	}
	p.fonts = map[string]uintptr{}
	for _, spec := range []struct {
		name         string
		size, weight int32
	}{{"title", 18, 600}, {"count", 36, 600}, {"body", 16, 400}, {"small", 14, 400}, {"status", 14, 600}, {"icon", 16, 400}} {
		face := "Segoe UI"
		if spec.name == "icon" {
			face = "Segoe MDL2 Assets"
		}
		font, _, _ := gdi32.NewProc("CreateFontW").Call(uintptr(-p.px(spec.size)), 0, 0, 0, uintptr(spec.weight), 0, 0, 0, 1, 0, 0, 5, 0, pointer(wide(face)))
		p.fonts[spec.name] = font
	}
}
func (c *controller) ensurePanel() error {
	if c.panel != nil {
		return nil
	}
	p := &nativePanel{owner: c, dpi: 96, controls: map[uint32]*panelControl{}, fonts: map[string]uintptr{}}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	name := wide("WUF.Translate.Panel")
	class := windowClass{Style: 0x20008, Callback: syscall.NewCallback(p.windowProc), Instance: instance, ClassName: name}
	class.Size = uint32(unsafe.Sizeof(class))
	if value, _, err := registerClass.Call(pointer(&class)); value == 0 {
		return fmt.Errorf("register panel: %w", err)
	}
	// An owned popup stays out of the taskbar without WS_EX_TOOLWINDOW, which
	// also lets accessibility tools discover its controls.
	hwnd, _, err := createWindow.Call(0x8, pointer(name), pointer(wide("LLM watchdog")), 0x80000000|0x2000000, 0, 0, 392, 510, c.hwnd, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("create panel: %w", err)
	}
	p.hwnd = hwnd
	p.brush, _, _ = gdi32.NewProc("CreateSolidBrush").Call(panelBackground)
	c.panel = p
	// Windows 11 rounds the native popup; older Windows keeps the same layout.
	corners := uint32(2)
	dark := uint32(1)
	dwm := syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	dwm.Call(hwnd, 33, pointer(&corners), 4)
	dwm.Call(hwnd, 20, pointer(&dark), 4)
	for _, spec := range []struct {
		id         uint32
		x, y, w, h int32
	}{{1, 24, 17, 218, 30}, {2, 240, 20, 128, 25}, {3, 24, 53, 344, 47}, {4, 24, 103, 344, 24}, {100, 12, 150, 368, 49}, {20, 12, 208, 368, 40}, {21, 12, 248, 368, 40}, {130, 12, 301, 368, 49}, {40, 12, 365, 368, 38}, {41, 12, 403, 368, 38}, {50, 12, 459, 368, 38}} {
		kind := wide("BUTTON")
		style := uintptr(0x5001000b)
		if spec.id < 10 {
			kind = wide("STATIC")
			style = 0x5000000d
		}
		child, _, err := createWindow.Call(0, pointer(kind), pointer(wide("")), style, uintptr(spec.x), uintptr(spec.y), uintptr(spec.w), uintptr(spec.h), hwnd, uintptr(spec.id), instance, 0)
		if child == 0 {
			destroyWindow.Call(hwnd)
			c.panel = nil
			return fmt.Errorf("create panel control: %w", err)
		}
		p.controls[spec.id] = &panelControl{hwnd: child, x: spec.x, y: spec.y, w: spec.w, h: spec.h, enabled: true}
	}
	p.createFonts()
	return nil
}
func (p *nativePanel) visible() bool {
	v, _, _ := user32.NewProc("IsWindowVisible").Call(p.hwnd)
	return v != 0
}
func (p *nativePanel) hide() {
	if p.hiding {
		return
	}
	p.hiding = true
	defer func() { p.hiding = false }()
	user32.NewProc("KillTimer").Call(p.hwnd, 1)
	user32.NewProc("ShowWindow").Call(p.hwnd, 0)
}
func (c *controller) showPanel() {
	if err := c.ensurePanel(); err != nil {
		log.Printf("Open tray panel: %v", err)
		c.mu.Lock()
		c.feedback = err.Error()
		c.mu.Unlock()
		return
	}
	p := c.panel
	if p.visible() {
		p.hide()
		return
	}
	// Clicking the tray first deactivates the popup, then delivers NIN_SELECT.
	// Consume that same click instead of reopening what it just dismissed.
	if !p.iconDismiss.IsZero() && time.Since(p.iconDismiss) < 500*time.Millisecond {
		p.iconDismiss = time.Time{}
		return
	}
	var cursor point
	getCursor.Call(pointer(&cursor))
	p.icon = rect{cursor.X - p.px(16), cursor.Y - p.px(16), cursor.X + p.px(16), cursor.Y + p.px(16)}
	identifier := struct {
		Size   uint32
		Window uintptr
		ID     uint32
		GUID   [16]byte
	}{Window: c.hwnd, ID: 1}
	identifier.Size = uint32(unsafe.Sizeof(identifier))
	var icon rect
	if result, _, _ := shell32.NewProc("Shell_NotifyIconGetRect").Call(pointer(&identifier), pointer(&icon)); result == 0 {
		p.icon = icon
	}
	anchor := point{p.icon.Left + (p.icon.Right-p.icon.Left)/2, p.icon.Top + (p.icon.Bottom-p.icon.Top)/2}
	monitor, _, _ := user32.NewProc("MonitorFromPoint").Call(uintptr(uint32(anchor.X))|uintptr(uint32(anchor.Y))<<32, 2)
	info := monitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	user32.NewProc("GetMonitorInfoW").Call(monitor, pointer(&info))
	dpi := uint32(96)
	var unused uint32
	syscall.NewLazyDLL("shcore.dll").NewProc("GetDpiForMonitor").Call(monitor, 0, pointer(&dpi), pointer(&unused))
	if dpi < 96 {
		dpi = 96
	}
	p.dpi = dpi
	p.createFonts()
	width, height := p.px(392), p.px(510)
	x, y := panelPosition(p.icon, info.Work, width, height, p.px(8))
	for _, control := range p.controls {
		user32.NewProc("MoveWindow").Call(control.hwnd, uintptr(p.px(control.x)), uintptr(p.px(control.y)), uintptr(p.px(control.w)), uintptr(p.px(control.h)), 1)
	}
	p.refresh()
	user32.NewProc("SetWindowPos").Call(p.hwnd, ^uintptr(0), uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0)
	p.roundCorners(width, height)
	// Mouse-opened menus keep focus for keyboard use without drawing its cue.
	user32.NewProc("SendMessageW").Call(p.hwnd, 0x127, 0x10001, 0) // UIS_SET, UISF_HIDEFOCUS
	user32.NewProc("ShowWindow").Call(p.hwnd, 5)
	foreground.Call(p.hwnd)
	user32.NewProc("SetFocus").Call(p.controls[100].hwnd)
	user32.NewProc("SetTimer").Call(p.hwnd, 1, 100, 0)
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
func panelPosition(icon, work rect, width, height, gap int32) (int32, int32) {
	x := icon.Left + (icon.Right-icon.Left-width)/2
	y := icon.Top - height - gap
	if y < work.Top {
		y = icon.Bottom + gap
	}
	return max(work.Left, min(x, work.Right-width)), max(work.Top, min(y, work.Bottom-height))
}
func (p *nativePanel) roundCorners(width, height int32) {
	// Explicit clipping also rounds borderless popups when DWM declines to do so.
	region, _, _ := gdi32.NewProc("CreateRoundRectRgn").Call(0, 0, uintptr(width+1), uintptr(height+1), uintptr(p.px(24)), uintptr(p.px(24)))
	if region != 0 {
		if ok, _, _ := user32.NewProc("SetWindowRgn").Call(p.hwnd, region, 1); ok == 0 {
			deleteObject.Call(region)
		}
		// Windows owns the region after a successful SetWindowRgn.
	}
}
func (p *nativePanel) refresh() {
	c := p.owner
	c.mu.Lock()
	data, busy, feedback, updated := c.data, c.busy, c.feedback, c.updated
	c.mu.Unlock()
	previous := p.view
	p.view = panelState(data, time.Since(updated), busy, feedback)
	title := c.title()
	labels := map[uint32]string{1: title, 2: p.view.status, 3: p.view.countdown, 4: p.view.detail, 100: p.view.model, 20: p.view.primary, 21: p.view.auto, 130: p.view.decision, 40: "開啟控制台\tCtrl+D", 41: "複製 API 網址\tCtrl+C", 50: "退出 LLM watchdog\tCtrl+Q"}
	for id, label := range labels {
		if p.controls[id].text != label {
			p.controls[id].text = label
			user32.NewProc("SetWindowTextW").Call(p.controls[id].hwnd, pointer(wide(label)))
		}
	}
	for id, on := range map[uint32]bool{100: p.view.modelsEnabled, 20: p.view.primaryEnabled, 21: p.view.autoEnabled, 130: p.view.decisionEnabled} {
		if p.controls[id].enabled == on {
			continue
		}
		p.controls[id].enabled = on
		value := uintptr(0)
		if on {
			value = 1
		}
		user32.NewProc("EnableWindow").Call(p.controls[id].hwnd, value)
	}
	if previous.progress != p.view.progress {
		invalidate.Call(p.hwnd, 0, 0)
	}
	if previous.checked != p.view.checked {
		invalidate.Call(p.controls[21].hwnd, 0, 0)
	}
	var cursor point
	getCursor.Call(pointer(&cursor))
	user32.NewProc("ScreenToClient").Call(p.hwnd, pointer(&cursor))
	hover := uint32(0)
	for id, control := range p.controls {
		if id >= 10 && control.enabled && cursor.X >= p.px(control.x) && cursor.X < p.px(control.x+control.w) && cursor.Y >= p.px(control.y) && cursor.Y < p.px(control.y+control.h) {
			hover = id
			break
		}
	}
	if hover != p.hover {
		for _, id := range []uint32{p.hover, hover} {
			if control := p.controls[id]; control != nil {
				invalidate.Call(control.hwnd, 0, 0)
			}
		}
		p.hover = hover
	}
}
func buffered(dc uintptr, area rect, draw func(uintptr)) {
	memory, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	if memory == 0 {
		draw(dc)
		return
	}
	defer gdi32.NewProc("DeleteDC").Call(memory)
	bitmap, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(area.Right), uintptr(area.Bottom))
	if bitmap == 0 {
		draw(dc)
		return
	}
	defer deleteObject.Call(bitmap)
	old, _, _ := selectObject.Call(memory, bitmap)
	defer selectObject.Call(memory, old)
	draw(memory)
	gdi32.NewProc("BitBlt").Call(dc, 0, 0, uintptr(area.Right), uintptr(area.Bottom), memory, 0, 0, 0xcc0020)
}
func (p *nativePanel) paint() {
	var paint paintStruct
	dc, _, _ := user32.NewProc("BeginPaint").Call(p.hwnd, pointer(&paint))
	defer user32.NewProc("EndPaint").Call(p.hwnd, pointer(&paint))
	var area rect
	user32.NewProc("GetClientRect").Call(p.hwnd, pointer(&area))
	buffered(dc, area, func(dc uintptr) {
		p.background(dc, area)
	})
}
func (p *nativePanel) background(dc uintptr, area rect) {
	paintColor(dc, area, panelBackground)
	// A restrained outline and spacing define groups without lifting the black surface.
	border := color(46, 48, 53)
	paintColor(dc, rect{0, 0, area.Right, 1}, border)
	paintColor(dc, rect{0, area.Bottom - 1, area.Right, area.Bottom}, border)
	paintColor(dc, rect{0, 0, 1, area.Bottom}, border)
	paintColor(dc, rect{area.Right - 1, 0, area.Right, area.Bottom}, border)
	for _, y := range []int32{201, 294, 356, 449} {
		paintColor(dc, rect{p.px(24), p.px(y), area.Right - p.px(24), p.px(y) + 1}, color(39, 42, 47))
	}
	track := rect{p.px(24), p.px(135), area.Right - p.px(24), p.px(142)}
	roundColor(dc, track, p.px(7), color(48, 50, 55))
	track.Right = track.Left + int32(float64(track.Right-track.Left)*p.view.progress)
	if track.Right > track.Left {
		roundColor(dc, track, p.px(7), color(0, 132, 255))
	}
}
func (p *nativePanel) draw(item *drawItem) {
	copy := *item
	buffered(item.DC, item.Rect, func(dc uintptr) { copy.DC = dc; p.drawControl(&copy) })
}
func (p *nativePanel) drawControl(item *drawItem) {
	control := p.controls[item.ID]
	if control == nil {
		return
	}
	paintColor(item.DC, item.Rect, panelBackground)
	area := item.Rect
	ink := color(237, 238, 241)
	font := "body"
	flags := uintptr(0x4)
	if item.ID < 10 {
		switch item.ID {
		case 1:
			font = "title"
		case 2:
			font = "status"
			flags |= 2
			ink = color(150, 155, 164)
			if p.view.ready {
				ink = color(53, 211, 106)
			}
			dot := rect{area.Left + p.px(27), (area.Bottom - p.px(6)) / 2, area.Left + p.px(33), (area.Bottom + p.px(6)) / 2}
			roundColor(item.DC, dot, p.px(6), ink)
		case 3:
			font = "count"
		case 4:
			font = "small"
			ink = color(165, 171, 182)
		}
	} else {
		enabled, _, _ := user32.NewProc("IsWindowEnabled").Call(control.hwnd)
		hot := p.hover == item.ID
		if enabled != 0 && (hot || item.State&1 != 0) {
			roundColor(item.DC, area, p.px(9), color(43, 45, 50))
		}
		if enabled == 0 {
			ink = color(124, 132, 145)
		}
		area.Left += p.px(12)
		area.Right -= p.px(12)
		if item.State&0x210 == 0x10 { // ODS_FOCUS without ODS_NOFOCUSRECT
			focus := item.Rect
			focus.Left += 2
			focus.Right -= 2
			focus.Top += 2
			focus.Bottom -= 2
			user32.NewProc("DrawFocusRect").Call(item.DC, pointer(&focus))
		}
	}
	if item.ID == 100 || item.ID == 130 {
		caption, label := "目前模型", control.text
		if item.ID == 130 {
			caption = "決策模型"
			label = strings.TrimPrefix(label, "決策模型 · ")
			if name, state, ok := strings.Cut(label, " · "); ok {
				label, caption = name, caption+" · "+state
			}
		}
		chevron := area
		chevron.Left = chevron.Right - p.px(16)
		p.text(item.DC, "\ue76c", chevron, "icon", color(142, 151, 165), 0x4|2)
		area.Right = chevron.Left - p.px(8)
		captionArea := area
		captionArea.Top += p.px(3)
		captionArea.Bottom = captionArea.Top + p.px(19)
		p.text(item.DC, caption, captionArea, "small", color(155, 164, 179), 0x4)
		area.Top = captionArea.Bottom
		p.text(item.DC, label, area, "body", ink, 0x4)
		return
	}
	if item.ID >= 10 {
		glyph := map[uint32]string{20: "\ue708", 21: "\ue73e", 40: "\ue8a7", 41: "\ue8c8", 50: "\ue7e8"}[item.ID]
		if item.ID == 20 && p.view.primary != "立即休眠" {
			glyph = "\ue768"
		}
		iconArea := area
		iconArea.Right = iconArea.Left + p.px(18)
		iconInk := color(142, 151, 165)
		if item.ID == 21 {
			if !p.view.checked {
				glyph = "\ue739"
			} else {
				iconInk = color(0, 145, 255)
			}
		}
		p.text(item.DC, glyph, iconArea, "icon", iconInk, 0x4)
		area.Left += p.px(28)
	}
	label, hint, split := strings.Cut(control.text, "\t")
	if split {
		hintArea := area
		hintArea.Left = area.Right - p.px(68)
		p.text(item.DC, hint, hintArea, "small", color(155, 164, 179), 0x4|2)
		area.Right = hintArea.Left - p.px(6)
	}
	p.text(item.DC, label, area, font, ink, flags)
}
func (p *nativePanel) command(id uint32) {
	c := p.owner
	switch id {
	case 100:
		p.models()
		return
	case 130:
		p.decisions()
		return
	case 40:
		shellExecute.Call(p.hwnd, pointer(wide("open")), pointer(wide(c.client.BaseURL+"/status")), 0, 0, 1)
	case 41:
		if err := copyText(p.hwnd, c.client.BaseURL+"/v1"); err != nil {
			c.mu.Lock()
			c.feedback = err.Error()
			c.mu.Unlock()
		}
	case 50:
		p.hide()
		c.action(menuAction{"stop", nil})
		return
	default:
		path := "toggle_auto_sleep"
		if id == 20 {
			path = "wake"
			if p.view.primary == "立即休眠" {
				path = "sleep"
			}
		}
		if id != 20 && id != 21 {
			return
		}
		c.action(menuAction{path, nil})
		p.refresh()
		return
	}
	p.hide()
}
func (p *nativePanel) choose(menu uintptr, control uint32) uint32 {
	defer destroyMenu.Call(menu)
	var anchor rect
	user32.NewProc("GetWindowRect").Call(p.controls[control].hwnd, pointer(&anchor))
	p.tracking = true
	selected, _, _ := trackMenu.Call(menu, 0x100|0x80|2, uintptr(anchor.Right), uintptr(anchor.Top), 0, p.hwnd, 0)
	p.tracking = false
	foreground.Call(p.hwnd)
	return uint32(selected)
}
func (p *nativePanel) models() {
	c := p.owner
	c.mu.Lock()
	data := c.data
	c.mu.Unlock()
	if data == nil {
		return
	}
	menu, _, _ := createPopup.Call()
	if menu == 0 {
		return
	}
	choices := map[uint32]string{}
	for i, model := range data.Models {
		id := uint32(1000 + i)
		c.append(menu, id, model.Name, model.ID != data.CurrentModelID || data.Status != "running", model.ID == data.CurrentModelID)
		choices[id] = model.ID
	}
	if model, ok := choices[p.choose(menu, 100)]; ok {
		c.action(menuAction{"switch", url.Values{"model": {model}}})
		p.refresh()
	}
}
func (p *nativePanel) decisions() {
	c := p.owner
	c.mu.Lock()
	data, busy := c.data, c.busy
	c.mu.Unlock()
	if data == nil {
		return
	}
	menu, _, _ := createPopup.Call()
	if menu == 0 {
		return
	}
	status := "休眠待命"
	path, label := "decision/wake", "啟動決策模型"
	if data.Decision.Status == "running" {
		status = "準備就緒 · " + data.Decision.Device
		path, label = "decision/sleep", "決策模型休眠"
	}
	if data.Decision.Status == "starting" {
		status = "載入中…"
	}
	if data.Decision.ActiveRequests > 0 {
		status = "處理中"
	}
	c.append(menu, 1, status, false, false)
	separator(menu)
	c.append(menu, 30, label, !busy && data.Decision.RuntimeAvailable && data.Decision.Status != "starting" && data.Decision.ActiveRequests == 0, false)
	c.append(menu, 31, "閒置 3 分鐘後自動休眠", !busy, data.Decision.AutoSleepEnabled)
	switch p.choose(menu, 130) {
	case 30:
		c.action(menuAction{path, nil})
	case 31:
		c.action(menuAction{"decision/toggle_auto_sleep", nil})
	}
	p.refresh()
}
func (p *nativePanel) windowProc(hwnd uintptr, id uint32, wparam, lparam uintptr) uintptr {
	switch id {
	case 0x0f:
		if p.hwnd != 0 {
			p.paint()
		}
		return 0
	case 0x14:
		return 1
	case 0x135, 0x138:
		gdi32.NewProc("SetBkColor").Call(wparam, panelBackground)
		return p.brush
	case 0x2b:
		var item drawItem
		kernel32.NewProc("RtlMoveMemory").Call(pointer(&item), lparam, unsafe.Sizeof(item))
		p.draw(&item)
		return 1
	case 0x111:
		if wparam>>16 == 0 {
			p.command(uint32(wparam & 0xffff))
		}
		return 0
	case 0x113:
		p.refresh()
		return 0
	case 0x6:
		if wparam&0xffff == 0 && !p.tracking && !p.hiding {
			var cursor point
			getCursor.Call(pointer(&cursor))
			if cursor.X >= p.icon.Left && cursor.X < p.icon.Right && cursor.Y >= p.icon.Top && cursor.Y < p.icon.Bottom {
				p.iconDismiss = time.Now()
			}
			p.hide()
		}
		return 0
	case 0x10:
		p.hide()
		return 0
	}
	result, _, _ := defaultProc.Call(hwnd, uintptr(id), wparam, lparam)
	return result
}
func (p *nativePanel) key(msg *message) bool {
	if !p.visible() || msg.ID != 0x100 {
		return false
	}
	if msg.WParam == 9 {
		user32.NewProc("SendMessageW").Call(p.hwnd, 0x127, 0x10002, 0) // UIS_CLEAR, UISF_HIDEFOCUS
	}
	if msg.WParam == 27 {
		p.hide()
		return true
	}
	ctrl, _, _ := user32.NewProc("GetKeyState").Call(17)
	if ctrl&0x8000 != 0 {
		for key, id := range map[uintptr]uint32{'D': 40, 'C': 41, 'Q': 50} {
			if msg.WParam == key {
				p.command(id)
				return true
			}
		}
	}
	return false
}
func (p *nativePanel) close() {
	p.hide()
	destroyWindow.Call(p.hwnd)
	deleteObject.Call(p.brush)
	for _, font := range p.fonts {
		deleteObject.Call(font)
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	user32.NewProc("UnregisterClassW").Call(pointer(wide("WUF.Translate.Panel")), instance)
}
