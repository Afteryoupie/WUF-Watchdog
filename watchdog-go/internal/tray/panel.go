// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"fmt"
	"time"
)

type panelView struct {
	status, countdown, detail, model, primary, auto, decision                   string
	progress                                                                    float64
	ready, primaryEnabled, modelsEnabled, decisionEnabled, autoEnabled, checked bool
}

func panelState(data *Snapshot, elapsed time.Duration, busy bool, feedback string) panelView {
	v := panelView{status: "未連線", countdown: "—", detail: "無法連線至本機 Watchdog", model: "尚無可用模型", primary: "啟動模型", auto: "閒置時自動休眠", decision: "決策模型 · Laya multilingual · 未連線"}
	if data == nil {
		return v
	}
	v.model = Compact(data.CurrentModel)
	v.primaryEnabled = !busy && data.Status != "starting" && data.ActiveRequests == 0
	v.modelsEnabled = v.primaryEnabled && len(data.Models) > 0
	v.autoEnabled = !busy && data.IdleTimeout > 0
	v.decisionEnabled = !busy && len(data.Decision.Models) > 0
	v.checked = data.AutoSleepEnabled
	v.auto = fmt.Sprintf("閒置 %d 分鐘後自動休眠", data.IdleTimeout/60)
	if data.IdleTimeout <= 0 {
		v.auto = "此模型保持常駐"
	}
	v.countdown = Countdown(data, elapsed)
	switch data.Status {
	case "running":
		v.status, v.ready, v.primary = "準備就緒", true, "立即休眠"
		v.detail = "後自動休眠 · 新請求會重設倒數"
		if data.IdleTimeout > 0 && data.AutoSleepEnabled {
			v.progress = max(0, min(1, float64(data.IdleRemaining-int(elapsed.Seconds()))/float64(data.IdleTimeout)))
		}
		if !data.AutoSleepEnabled || data.IdleTimeout <= 0 {
			v.detail = "已關閉自動休眠 · 模型保持喚醒"
			v.progress = 1
		}
		if data.ActiveRequests > 0 {
			v.status = "處理中"
			v.detail = fmt.Sprintf("目前有 %d 個請求，暫不休眠", data.ActiveRequests)
			v.progress = 1
		}
	case "starting":
		v.status = "正在載入"
		v.detail = "正在載入模型，請稍候…"
	default:
		v.status = "休眠待命"
		v.detail = "下一次請求會自動喚醒"
	}
	decision := "休眠待命"
	switch data.Decision.Status {
	case "starting":
		decision = "載入中…"
	case "running":
		decision = "準備就緒"
		if data.Decision.ActiveRequests > 0 {
			decision = "處理中"
		}
	}
	v.decision = "決策模型 · Laya multilingual · " + decision
	if busy {
		v.status = "正在處理…"
	}
	if feedback != "" && feedback != "操作完成" && feedback != "正在處理…" {
		v.detail = feedback
	}
	return v
}
