// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"runtime"
	"syscall"
	"testing"
	"time"
)

// Native popup handles are exercised without opening a menu or touching the
// clipboard.
func TestNativeMenuUpdatesBusyState(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	root, _, _ := createPopup.Call()
	models, _, _ := createPopup.Call()
	if root == 0 || models == 0 {
		t.Fatal("CreatePopupMenu failed")
	}
	defer destroyMenu.Call(root)
	c := &controller{popup: root, modelMenu: models, updated: time.Now(), actions: map[uint32]menuAction{}, modelIDs: map[uint32]string{1000: "full-model-ID"}}
	c.append(root, 10, "status", false, false)
	c.append(root, 11, "countdown", false, false)
	c.append(root, 12, "feedback", false, false)
	c.append(root, 20, "start", true, false)
	c.append(root, 21, "idle", true, false)
	c.append(models, 1000, "model", true, false)
	appendMenu.Call(root, 0x10, models, pointer(wide("models")))
	c.data = &Snapshot{Status: "running", ActiveRequests: 1, CurrentModelID: "full-model-ID", CurrentModel: "Riva-Translate-4B-Instruct-v2-Q4_K_M", AutoSleepEnabled: true}
	c.updatePopup()
	var label [128]uint16
	getLabel := user32.NewProc("GetMenuStringW")
	getLabel.Call(root, 5, pointer(&label[0]), 128, 0x400)
	if text := syscall.UTF16ToString(label[:]); text != Compact(c.data.CurrentModel) {
		t.Fatal("parent label must show current model", text)
	}
	if submenu, _, _ := user32.NewProc("GetSubMenu").Call(root, 5); submenu != models {
		t.Fatal("parent update detached the model submenu")
	}
	state, _, _ := user32.NewProc("GetMenuState").Call(root, 20, 0)
	if state&3 == 0 {
		t.Fatal("busy translation sleep remained enabled")
	}
	state, _, _ = user32.NewProc("GetMenuState").Call(models, 1000, 0)
	if state&3 == 0 || state&8 == 0 {
		t.Fatal("busy model menu lost disabled/selected state")
	}
	if c.actions[20].path != "sleep" {
		t.Fatal("displayed action does not match operation")
	}
}
