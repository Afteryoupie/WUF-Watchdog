// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"runtime"
	"syscall"
	"testing"
	"time"

	"wuf-translate/watchdog-go/internal/core"
)

// Native popup handles are exercised without opening a menu or touching the
// clipboard. This catches routing the Laya labels to the wrong parent HMENU.
func TestNativeMenuUpdatesNestedDecisionAndBusyState(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	root, _, _ := createPopup.Call()
	models, _, _ := createPopup.Call()
	decisions, _, _ := createPopup.Call()
	if root == 0 || models == 0 || decisions == 0 {
		t.Fatal("CreatePopupMenu failed")
	}
	defer destroyMenu.Call(root)
	c := &controller{popup: root, modelMenu: models, decisionMenu: decisions, updated: time.Now(), actions: map[uint32]menuAction{}, modelIDs: map[uint32]string{1000: "full-model-ID"}}
	c.append(root, 10, "status", false, false)
	c.append(root, 11, "countdown", false, false)
	c.append(root, 12, "feedback", false, false)
	c.append(root, 20, "start", true, false)
	c.append(root, 21, "idle", true, false)
	c.append(models, 1000, "model", true, false)
	c.append(decisions, 30, "decision start", true, false)
	c.append(decisions, 31, "decision idle", true, false)
	appendMenu.Call(root, 0x10, models, pointer(wide("models")))
	appendMenu.Call(root, 0x10, decisions, pointer(wide("decision")))
	c.data = &Snapshot{Snapshot: core.Snapshot{Status: "running", ActiveRequests: 1, CurrentModelID: "full-model-ID", CurrentModel: "Riva-Translate-4B-Instruct-v2-Q4_K_M", AutoSleepEnabled: true},
		Decision: core.DecisionSnapshot{Status: "running", Models: []core.Model{{ID: "laya"}}, RuntimeAvailable: true}}
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
	getLabel.Call(decisions, 30, pointer(&label[0]), 128, 0)
	if text := syscall.UTF16ToString(label[:]); text != "決策模型休眠" {
		t.Fatal("wrong nested label", text)
	}
	state, _, _ := user32.NewProc("GetMenuState").Call(root, 20, 0)
	if state&3 == 0 {
		t.Fatal("busy translation sleep remained enabled")
	}
	state, _, _ = user32.NewProc("GetMenuState").Call(models, 1000, 0)
	if state&3 == 0 || state&8 == 0 {
		t.Fatal("busy model menu lost disabled/selected state")
	}
	if c.actions[30].path != "decision/sleep" || c.actions[20].path != "sleep" {
		t.Fatal("displayed action does not match operation")
	}
	c.data.Status = "sleeping"
	c.data.ActiveRequests = 0
	c.data.Decision.Status = "sleeping"
	c.updatePopup()
	getLabel.Call(decisions, 30, pointer(&label[0]), 128, 0)
	if text := syscall.UTF16ToString(label[:]); text != "啟動決策模型" || c.actions[30].path != "decision/wake" {
		t.Fatal("wake label/action diverged", text)
	}
}
