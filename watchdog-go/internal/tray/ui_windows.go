// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var registerClass = user32.NewProc("RegisterClassExW")
var createWindow = user32.NewProc("CreateWindowExW")
var defaultProc = user32.NewProc("DefWindowProcW")
var destroyWindow = user32.NewProc("DestroyWindow")
var postMessage = user32.NewProc("PostMessageW")
var postQuit = user32.NewProc("PostQuitMessage")
var getMessage = user32.NewProc("GetMessageW")
var translateMessage = user32.NewProc("TranslateMessage")
var dispatchMessage = user32.NewProc("DispatchMessageW")
var getCursor = user32.NewProc("GetCursorPos")
var foreground = user32.NewProc("SetForegroundWindow")
var createPopup = user32.NewProc("CreatePopupMenu")
var appendMenu = user32.NewProc("AppendMenuW")
var modifyMenu = user32.NewProc("ModifyMenuW")
var enableMenu = user32.NewProc("EnableMenuItem")
var checkMenu = user32.NewProc("CheckMenuItem")
var destroyMenu = user32.NewProc("DestroyMenu")
var trackMenu = user32.NewProc("TrackPopupMenu")
var loadImage = user32.NewProc("LoadImageW")
var loadIcon = user32.NewProc("LoadIconW")
var destroyIcon = user32.NewProc("DestroyIcon")
var notifyIcon = shell32.NewProc("Shell_NotifyIconW")
var shellExecute = shell32.NewProc("ShellExecuteW")

const updateMessage = 0x8001
const trayMessage = 0x8002

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	ID             uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Callback                           uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type notification struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	BalloonIcon         uintptr
}
type menuAction struct {
	path  string
	query url.Values
}

func wide(value string) *uint16       { result, _ := syscall.UTF16PtrFromString(value); return result }
func pointer[T any](value *T) uintptr { return uintptr(unsafe.Pointer(value)) }

type controller struct {
	ctx                     context.Context
	cancel                  context.CancelFunc
	client                  *Client
	hwnd                    uintptr
	mu                      sync.Mutex
	data                    *Snapshot
	updated                 time.Time
	feedback                string
	busy, quitting          bool
	wake                    chan struct{}
	wg                      sync.WaitGroup
	icons                   map[string]uintptr
	ownedIcons              []uintptr
	popup                   uintptr
	modelMenu, decisionMenu uintptr
	actions                 map[uint32]menuAction
	modelIDs                map[uint32]string
	taskbarMessage          uint32
	panel                   *nativePanel
}

func Run(ctx context.Context, client *Client, assets, swift string, ownerPID int, showPanel bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if dpi := user32.NewProc("SetProcessDpiAwarenessContext"); dpi.Find() == nil {
		dpi.Call(^uintptr(3))
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &controller{ctx: ctx, cancel: cancel, client: client, wake: make(chan struct{}, 1), icons: map[string]uintptr{}}
	defer cancel()
	for _, state := range []string{"sleeping", "ready", "running", "starting", "offline"} {
		icon, _, _ := loadImage.Call(0, pointer(wide(filepath.Join(assets, "watchdog-"+state+".ico"))), 1, 16, 16, 0x10)
		if icon == 0 {
			icon, _, _ = loadIcon.Call(0, 32512)
		} else {
			c.ownedIcons = append(c.ownedIcons, icon)
		}
		c.icons[state] = icon
	}
	defer func() {
		for _, icon := range c.ownedIcons {
			destroyIcon.Call(icon)
		}
	}()
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	className := wide("WUF.Translate.Tray")
	class := windowClass{Style: 8, Callback: syscall.NewCallback(c.windowProc), Instance: instance, ClassName: className}
	class.Size = uint32(unsafe.Sizeof(class))
	if value, _, err := registerClass.Call(pointer(&class)); value == 0 {
		return fmt.Errorf("register tray window: %w", err)
	}
	hwnd, _, err := createWindow.Call(0, pointer(className), pointer(wide("WUF Translate Watchdog")), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %w", err)
	}
	c.hwnd = hwnd
	defer destroyWindow.Call(hwnd)
	defer func() {
		if c.panel != nil {
			c.panel.close()
		}
	}()
	taskbar, _, _ := user32.NewProc("RegisterWindowMessageW").Call(pointer(wide("TaskbarCreated")))
	c.taskbarMessage = uint32(taskbar)
	if !c.refreshIcon(0) {
		return errors.New("Windows notification area rejected the tray icon")
	}
	defer func() {
		info := notification{Window: hwnd, ID: 1}
		info.Size = uint32(unsafe.Sizeof(info))
		notifyIcon.Call(2, pointer(&info))
	}()
	c.wg.Add(1)
	go func() { defer c.wg.Done(); c.poll(ownerPID) }()
	defer c.wg.Wait()
	defer cancel()
	if showPanel {
		c.showPanel()
		if c.panel != nil {
			log.Printf("Panel preview visible: %t", c.panel.visible())
		}
	}
	var msg message
	for {
		result, _, err := getMessage.Call(pointer(&msg), 0, 0, 0)
		if int32(result) == -1 {
			return err
		}
		if result == 0 {
			return nil
		}
		if c.panel != nil && c.panel.visible() {
			if c.panel.key(&msg) {
				continue
			}
			if handled, _, _ := user32.NewProc("IsDialogMessageW").Call(c.panel.hwnd, pointer(&msg)); handled != 0 {
				continue
			}
		}
		translateMessage.Call(pointer(&msg))
		dispatchMessage.Call(pointer(&msg))
	}
}

func (c *controller) windowProc(hwnd uintptr, id uint32, wparam, lparam uintptr) uintptr {
	switch id {
	case trayMessage:
		event := uint32(lparam) & 0xffff
		if event == 0x400 || event == 0x401 {
			c.showPanel()
		} else if event == 0x7b || event == 0x205 {
			if c.panel != nil {
				c.panel.hide()
			}
			c.showMenu()
		}
		return 0
	case updateMessage:
		c.refreshIcon(1)
		c.updatePopup()
		if c.panel != nil && c.panel.visible() {
			c.panel.refresh()
		}
		return 0
	case 0x10:
		c.cancel()
		destroyWindow.Call(hwnd)
		return 0
	case 0x2:
		postQuit.Call(0)
		return 0
	}
	if c.taskbarMessage != 0 && id == c.taskbarMessage {
		c.refreshIcon(0)
		return 0
	}
	result, _, _ := defaultProc.Call(hwnd, uintptr(id), wparam, lparam)
	return result
}

func (c *controller) refreshIcon(operation uintptr) bool {
	c.mu.Lock()
	data := c.data
	tip := c.title() + " · " + IconState(data)
	if data != nil {
		tip += " · " + data.CurrentModel
	}
	c.mu.Unlock()
	info := notification{Window: c.hwnd, ID: 1, Flags: 1 | 2 | 4 | 0x80, Callback: trayMessage, Icon: c.icons[IconState(data)]}
	info.Size = uint32(unsafe.Sizeof(info))
	characters := syscall.StringToUTF16(tip)
	copy(info.Tip[:127], characters)
	result, _, _ := notifyIcon.Call(operation, pointer(&info))
	if operation == 0 {
		info.Version = 4
		notifyIcon.Call(4, pointer(&info))
	}
	return result != 0
}

func (c *controller) title() string {
	if c.client.BaseURL != "http://127.0.0.1:8080" {
		if origin, err := url.Parse(c.client.BaseURL); err == nil {
			return "LLM watchdog · " + origin.Port()
		}
	}
	return "LLM watchdog"
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	handle, _, _ := kernel32.NewProc("OpenProcess").Call(0x100000, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer kernel32.NewProc("CloseHandle").Call(handle)
	result, _, _ := kernel32.NewProc("WaitForSingleObject").Call(handle, 0)
	return result == 0x102
}

func (c *controller) poll(ownerPID int) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		state, err := c.client.Status(c.ctx)
		c.mu.Lock()
		c.data = state
		c.updated = time.Now()
		if err != nil {
			c.data = nil
		}
		c.mu.Unlock()
		postMessage.Call(c.hwnd, updateMessage, 0, 0)
		if err != nil && !processAlive(ownerPID) {
			postMessage.Call(c.hwnd, 0x10, 0, 0)
			return
		}
		select {
		case <-c.ctx.Done():
			postMessage.Call(c.hwnd, 0x10, 0, 0)
			return
		case <-ticker.C:
		case <-c.wake:
		}
	}
}

func (c *controller) append(menu uintptr, id uint32, text string, enabled, checked bool) {
	flags := uintptr(0)
	if !enabled {
		flags |= 1
	}
	if checked {
		flags |= 8
	}
	appendMenu.Call(menu, flags, uintptr(id), pointer(wide(strings.ReplaceAll(text, "&", "&&"))))
}
func separator(menu uintptr) { appendMenu.Call(menu, 0x800, 0, 0) }

func (c *controller) showMenu() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
	menu, _, _ := createPopup.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	c.popup = menu
	defer func() { c.popup = 0 }()
	c.actions = map[uint32]menuAction{}
	c.modelIDs = map[uint32]string{}
	c.mu.Lock()
	data, busy, feedback := c.data, c.busy, c.feedback
	c.mu.Unlock()
	connected := data != nil
	c.append(menu, 1, c.title()+strings.Repeat("\u2007", 28), false, false)
	c.append(menu, 10, "狀態", false, false)
	c.append(menu, 11, "休眠倒數", false, false)
	c.append(menu, 12, Compact(feedback), false, false)
	separator(menu)
	models, _, _ := createPopup.Call()
	c.modelMenu = models
	if data != nil {
		for index, model := range data.Models {
			id := uint32(1000 + index)
			c.append(models, id, Compact(model.Name), !busy && data.Status != "starting" && data.ActiveRequests == 0, model.ID == data.CurrentModelID)
			c.actions[id] = menuAction{"switch", url.Values{"model": {model.ID}}}
			c.modelIDs[id] = model.ID
		}
	}
	if data == nil || len(data.Models) == 0 {
		c.append(models, 2, "尚無可用模型", false, false)
	}
	modelLabel := "選擇模型"
	if data != nil && data.CurrentModel != "" {
		modelLabel = Compact(data.CurrentModel)
	}
	appendMenu.Call(menu, 0x10, models, pointer(wide(modelLabel)))
	c.append(menu, 20, "啟動模型", connected && !busy, false)
	c.actions[20] = menuAction{"wake", nil}
	c.append(menu, 21, "閒置時自動休眠", connected && !busy, false)
	c.actions[21] = menuAction{"toggle_auto_sleep", nil}
	separator(menu)
	decisions, _, _ := createPopup.Call()
	c.decisionMenu = decisions
	c.append(decisions, 30, "啟動決策模型", connected && !busy, false)
	c.actions[30] = menuAction{"decision/wake", nil}
	c.append(decisions, 31, "閒置 3 分鐘後自動休眠", connected && !busy, false)
	c.actions[31] = menuAction{"decision/toggle_auto_sleep", nil}
	appendMenu.Call(menu, 0x10, decisions, pointer(wide("決策模型 · Laya multilingual")))
	separator(menu)
	c.append(menu, 40, "開啟網頁控制台", true, false)
	c.append(menu, 41, "複製 API 網址", true, false)
	c.append(menu, 42, "重新整理狀態", true, false)
	separator(menu)
	c.append(menu, 50, "退出 LLM watchdog", true, false)
	c.updatePopup()
	var cursor point
	getCursor.Call(pointer(&cursor))
	foreground.Call(c.hwnd)
	selected, _, _ := trackMenu.Call(menu, 0x100|0x80|2, uintptr(cursor.X), uintptr(cursor.Y), 0, c.hwnd, 0)
	postMessage.Call(c.hwnd, 0, 0, 0)
	id := uint32(selected)
	switch id {
	case 40:
		shellExecute.Call(c.hwnd, pointer(wide("open")), pointer(wide(c.client.BaseURL+"/status")), 0, 0, 1)
	case 41:
		if err := copyText(c.hwnd, c.client.BaseURL+"/v1"); err != nil {
			c.mu.Lock()
			c.feedback = err.Error()
			c.mu.Unlock()
		}
	case 42:
		select {
		case c.wake <- struct{}{}:
		default:
		}
	case 50:
		c.action(menuAction{"stop", nil})
	default:
		if action, ok := c.actions[id]; ok {
			c.action(action)
		}
	}
}

func (c *controller) updatePopup() {
	if c.popup == 0 {
		return
	}
	c.mu.Lock()
	data, busy, feedback, updated := c.data, c.busy, c.feedback, c.updated
	c.mu.Unlock()
	state := "未連線"
	primary := "啟動模型"
	decisionPrimary := "啟動決策模型"
	if data != nil {
		switch data.Status {
		case "starting":
			state = "模型載入中…"
		case "running":
			state = "準備就緒"
			if data.ActiveRequests > 0 {
				state = "推論中"
			}
			primary = "立即休眠"
		default:
			state = "休眠待命"
		}
		if data.Decision.Status == "running" {
			decisionPrimary = "決策模型休眠"
		}
		if feedback == "" {
			feedback = Compact(data.CurrentModel)
		}
	}
	if busy {
		state = "正在處理…"
	}
	for id, text := range map[uint32]string{10: state, 11: "休眠倒數：" + Countdown(data, time.Since(updated)), 12: Compact(feedback), 20: primary, 30: decisionPrimary} {
		flags := uintptr(0)
		if id == 10 || id == 11 || id == 12 {
			flags = 1
		}
		target := c.popup
		if id == 30 {
			target = c.decisionMenu
		}
		modifyMenu.Call(target, uintptr(id), flags, uintptr(id), pointer(wide(strings.ReplaceAll(text, "&", "&&"))))
	}
	if data == nil {
		for _, id := range []uint32{20, 21, 30, 31} {
			target := c.popup
			if id >= 30 {
				target = c.decisionMenu
			}
			enableMenu.Call(target, uintptr(id), 1)
		}
		return
	}
	// Refresh the parent label while the popup is open, retaining its submenu.
	count, _, _ := user32.NewProc("GetMenuItemCount").Call(c.popup)
	for position := uintptr(0); int32(count) >= 0 && position < count; position++ {
		submenu, _, _ := user32.NewProc("GetSubMenu").Call(c.popup, position)
		if submenu == c.modelMenu {
			modifyMenu.Call(c.popup, position, 0x400|0x10, c.modelMenu, pointer(wide(strings.ReplaceAll(Compact(data.CurrentModel), "&", "&&"))))
			break
		}
	}
	primaryPath := "wake"
	if data.Status == "running" {
		primaryPath = "sleep"
	}
	c.actions[20] = menuAction{primaryPath, nil}
	decisionPath := "decision/wake"
	if data.Decision.Status == "running" {
		decisionPath = "decision/sleep"
	}
	c.actions[30] = menuAction{decisionPath, nil}
	for id, on := range map[uint32]bool{21: data.AutoSleepEnabled, 31: data.Decision.AutoSleepEnabled} {
		flag := uintptr(0)
		if on {
			flag = 8
		}
		target := c.popup
		if id == 31 {
			target = c.decisionMenu
		}
		checkMenu.Call(target, uintptr(id), flag)
	}
	for id, enabled := range map[uint32]bool{20: !busy && data.Status != "starting" && data.ActiveRequests == 0, 21: !busy,
		30: !busy && len(data.Decision.Models) > 0 && data.Decision.RuntimeAvailable && data.Decision.Status != "starting" && data.Decision.ActiveRequests == 0,
		31: !busy && len(data.Decision.Models) > 0} {
		flag := uintptr(0)
		if !enabled {
			flag = 1
		}
		target := c.popup
		if id >= 30 {
			target = c.decisionMenu
		}
		enableMenu.Call(target, uintptr(id), flag)
	}
	for id, model := range c.modelIDs {
		checked := uintptr(0)
		if model == data.CurrentModelID {
			checked = 8
		}
		checkMenu.Call(c.modelMenu, uintptr(id), checked)
		flag := uintptr(0)
		if busy || data.Status == "starting" || data.ActiveRequests > 0 || (model == data.CurrentModelID && data.Status == "running") {
			flag = 1
		}
		enableMenu.Call(c.modelMenu, uintptr(id), flag)
	}
}

func (c *controller) action(action menuAction) {
	c.mu.Lock()
	if c.quitting {
		c.mu.Unlock()
		return
	}
	if action.path == "stop" {
		c.quitting = true
	}
	c.busy = true
	c.feedback = "正在處理…"
	c.mu.Unlock()
	postMessage.Call(c.hwnd, updateMessage, 0, 0)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		err := c.client.Action(c.ctx, action.path, action.query)
		c.mu.Lock()
		c.busy = false
		if err != nil {
			c.feedback = err.Error()
			c.quitting = false
		} else {
			c.feedback = "操作完成"
		}
		c.mu.Unlock()
		if action.path == "stop" && err == nil {
			postMessage.Call(c.hwnd, 0x10, 0, 0)
			return
		}
		select {
		case c.wake <- struct{}{}:
		default:
		}
		postMessage.Call(c.hwnd, updateMessage, 0, 0)
	}()
}

func copyText(hwnd uintptr, text string) error {
	opened, _, err := user32.NewProc("OpenClipboard").Call(hwnd)
	if opened == 0 {
		return err
	}
	defer user32.NewProc("CloseClipboard").Call()
	characters := syscall.StringToUTF16(text)
	memory, _, err := kernel32.NewProc("GlobalAlloc").Call(0x42, uintptr(len(characters)*2))
	if memory == 0 {
		return err
	}
	address, _, err := kernel32.NewProc("GlobalLock").Call(memory)
	if address == 0 {
		kernel32.NewProc("GlobalFree").Call(memory)
		return err
	}
	kernel32.NewProc("RtlMoveMemory").Call(address, pointer(&characters[0]), uintptr(len(characters)*2))
	kernel32.NewProc("GlobalUnlock").Call(memory)
	user32.NewProc("EmptyClipboard").Call()
	if value, _, _ := user32.NewProc("SetClipboardData").Call(13, memory); value == 0 {
		kernel32.NewProc("GlobalFree").Call(memory)
		return errors.New("cannot copy API URL")
	}
	return nil
}
