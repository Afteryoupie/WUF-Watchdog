import Cocoa
import Foundation

/// A non-interactive status panel above native, keyboard-accessible menu commands.
final class GuardianStatusView: NSView {
    let status = NSTextField(labelWithString: "正在連線")
    let countdown = NSTextField(labelWithString: "—")
    let detail = NSTextField(labelWithString: "")
    let progress = NSProgressIndicator()

    init() {
        super.init(frame: NSRect(x: 0, y: 0, width: 300, height: 100))
        let title = NSTextField(labelWithString: "WUF Watchdog")
        title.font = .systemFont(ofSize: 13, weight: .semibold)
        title.frame = NSRect(x: 18, y: 72, width: 128, height: 22)
        status.font = .systemFont(ofSize: 11, weight: .medium)
        status.alignment = .right
        status.frame = NSRect(x: 130, y: 73, width: 152, height: 20)
        countdown.font = .monospacedDigitSystemFont(ofSize: 17, weight: .medium)
        countdown.frame = NSRect(x: 18, y: 43, width: 264, height: 25)
        detail.font = .systemFont(ofSize: 11)
        detail.textColor = .secondaryLabelColor
        detail.frame = NSRect(x: 18, y: 24, width: 264, height: 18)
        progress.isIndeterminate = false
        progress.minValue = 0; progress.maxValue = 1
        progress.style = .bar
        progress.frame = NSRect(x: 18, y: 10, width: 264, height: 5)
        for view in [title, status, countdown, detail, progress] { addSubview(view) }
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
}

class WUFMenuBarController: NSObject, NSMenuDelegate {
    private var statusItem: NSStatusItem!
    private let menu = NSMenu()
    private let panel = GuardianStatusView()
    private let modelsMenu = NSMenu()
    private var primaryItem: NSMenuItem!
    private var autoItem: NSMenuItem!
    private var modelsItem: NSMenuItem!
    private var decisionItem: NSMenuItem!
    private var decisionToggle: NSMenuItem!
    private var decisionAuto: NSMenuItem!
    private var decisionInfo: NSMenuItem!
    private var decisionSnapshot: [String: Any] = [:]
    private var quitItem: NSMenuItem!
    private var refreshItem: NSMenuItem!
    private var timer: Timer?
    private var nextPoll: TimeInterval = 0
    private var polling = false
    private var isMenuOpen = false
    private var busy = false
    private var feedback: String?
    private var currentStatus = "sleeping"
    private var currentModel = "尚未取得模型"
    private var currentModelId = ""
    private var idleDeadline: TimeInterval = 0
    private var idleTimeout = 0
    private var activeRequests = 0
    private var autoSleepEnabled = true
    private var availableModels: [[String: Any]] = []
    private var isConnected = false

    init(connect: Bool = true) {
        super.init()
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.menu = menu
        menu.delegate = self
        menu.autoenablesItems = false
        modelsMenu.autoenablesItems = false
        buildMenu()
        refreshPresentation()
        if connect {
            startPolling()
            fetchStatus()
        }
    }

    @discardableResult
    private func item(_ title: String, _ symbol: String, _ action: Selector, key: String = "") -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: key)
        item.target = self
        item.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)
        menu.addItem(item)
        return item
    }

    private func buildMenu() {
        let header = NSMenuItem()
        header.view = panel
        menu.addItem(header)
        modelsItem = NSMenuItem(title: "切換模型", action: nil, keyEquivalent: "")
        modelsItem.image = NSImage(systemSymbolName: "square.stack.3d.up", accessibilityDescription: nil)
        modelsItem.submenu = modelsMenu
        menu.addItem(modelsItem)
        menu.addItem(.separator())
        primaryItem = item("啟動模型", "power", #selector(toggleEngine))
        autoItem = item("閒置時自動休眠", "timer", #selector(toggleAutoSleep))
        menu.addItem(.separator())
        let decisionMenu = NSMenu()
        decisionMenu.autoenablesItems = false
        decisionItem = NSMenuItem(title: "決策模型 · Laya multilingual", action: nil, keyEquivalent: "")
        decisionItem.image = NSImage(systemSymbolName: "arrow.triangle.branch", accessibilityDescription: nil)
        decisionItem.submenu = decisionMenu
        decisionInfo = NSMenuItem(title: "休眠待命", action: nil, keyEquivalent: "")
        decisionInfo.isEnabled = false
        decisionMenu.addItem(decisionInfo)
        decisionToggle = NSMenuItem(title: "啟動決策模型", action: #selector(toggleDecision), keyEquivalent: "")
        decisionToggle.target = self
        decisionMenu.addItem(decisionToggle)
        decisionAuto = NSMenuItem(title: "閒置 3 分鐘後自動休眠", action: #selector(toggleDecisionAuto), keyEquivalent: "")
        decisionAuto.target = self
        decisionMenu.addItem(decisionAuto)
        menu.addItem(decisionItem)
        menu.addItem(.separator())
        item("開啟控制台", "rectangle.grid.2x2", #selector(openWebDashboard), key: "d")
        item("複製 API 網址", "doc.on.doc", #selector(copyApiUrl), key: "c")
        refreshItem = item("重新連線", "arrow.clockwise", #selector(reconnect))
        menu.addItem(.separator())
        quitItem = item("退出 WUF Watchdog…", "power", #selector(quitApp), key: "q")
    }

    private func startPolling() {
        let timer = Timer(timeInterval: 1, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            self.refreshPresentation()
            if ProcessInfo.processInfo.systemUptime >= self.nextPoll {
                self.nextPoll = ProcessInfo.processInfo.systemUptime + 2
                self.fetchStatus()
            }
        }
        self.timer = timer
        // Explicit tracking mode keeps ticking inside an open NSMenu/submenu.
        RunLoop.main.add(timer, forMode: .common)
        RunLoop.main.add(timer, forMode: .eventTracking)
    }

    private func fetchStatus() {
        guard !polling else { return }
        polling = true
        var request = URLRequest(url: URL(string: "http://127.0.0.1:8080/api/status")!)
        request.timeoutInterval = 1.5
        URLSession.shared.dataTask(with: request) { [weak self] data, response, error in
            // Dispatch onto the modes used by both the ordinary and menu loops.
            RunLoop.main.perform(inModes: [.default, .eventTracking]) {
                guard let self = self else { return }
                self.polling = false
                guard error == nil, (response as? HTTPURLResponse)?.statusCode == 200,
                      let data = data,
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let status = json["status"] as? String else {
                    self.isConnected = false
                    self.refreshPresentation()
                    return
                }
                self.isConnected = true
                self.currentStatus = status
                self.currentModel = json["current_model"] as? String ?? "尚未選擇模型"
                self.currentModelId = json["current_model_id"] as? String ?? ""
                self.idleTimeout = json["idle_timeout"] as? Int ?? 0
                self.idleDeadline = ProcessInfo.processInfo.systemUptime + Double(json["idle_remaining"] as? Int ?? 0)
                self.activeRequests = json["active_requests"] as? Int ?? 0
                self.autoSleepEnabled = json["auto_sleep_enabled"] as? Bool ?? true
                self.availableModels = json["models"] as? [[String: Any]] ?? []
                self.decisionSnapshot = json["decision"] as? [String: Any] ?? [:]
                if !self.isMenuOpen { self.rebuildModels() }
                self.refreshPresentation()
            }
        }.resume()
    }

    private func refreshPresentation() {
        let running = currentStatus == "running"
        let starting = currentStatus == "starting"
        let remaining = max(0, Int(ceil(idleDeadline - ProcessInfo.processInfo.systemUptime)))
        modelsItem.title = currentModel
        modelsItem.toolTip = "點選切換模型"
        panel.status.stringValue = !isConnected ? "未連線" : starting ? "正在載入" : running ? (activeRequests > 0 ? "處理中" : "準備就緒") : "休眠待命"
        panel.status.textColor = isConnected && running ? .systemGreen : .secondaryLabelColor
        panel.progress.isHidden = true
        if !isConnected {
            panel.countdown.stringValue = "尚未連線"
            panel.detail.stringValue = "正在重試；請確認 WUF Watchdog 已啟動"
        } else if busy || starting {
            panel.countdown.stringValue = starting ? "載入中" : "處理中"
            panel.detail.stringValue = "操作完成後會自動更新"
        } else if !running {
            panel.countdown.stringValue = "休眠中"
            panel.detail.stringValue = "需要時自動喚醒，或按下啟動模型"
        } else if activeRequests > 0 {
            panel.countdown.stringValue = "處理中"
            panel.detail.stringValue = "目前有 \(activeRequests) 個請求，暫不休眠"
        } else if !autoSleepEnabled || idleTimeout <= 0 {
            panel.countdown.stringValue = "保持喚醒"
            panel.detail.stringValue = "模型常駐，隨時回應請求"
        } else {
            panel.countdown.stringValue = String(format: "%02d:%02d", remaining / 60, remaining % 60)
            panel.detail.stringValue = remaining == 0 ? "等待服務進入休眠" : "後自動休眠 · 新請求會重設倒數"
            panel.progress.isHidden = false
            panel.progress.doubleValue = min(1, Double(remaining) / Double(idleTimeout))
        }
        if let feedback = feedback { panel.detail.stringValue = feedback }
        primaryItem.title = busy ? "正在處理…" : starting ? "模型載入中…" : running ? "立即休眠" : "啟動模型"
        primaryItem.image = NSImage(systemSymbolName: running ? "moon" : "power", accessibilityDescription: nil)
        primaryItem.isEnabled = isConnected && !busy && !starting
        autoItem.title = idleTimeout > 0 ? "閒置 \(idleTimeout / 60) 分鐘後自動休眠" : "此模型保持常駐"
        autoItem.state = autoSleepEnabled && idleTimeout > 0 ? .on : .off
        autoItem.isEnabled = isConnected && !busy && idleTimeout > 0
        modelsItem.isEnabled = isConnected && !busy && !starting
        for item in modelsMenu.items {
            let selected = item.representedObject as? String == currentModelId
            item.state = selected ? .on : .off
            item.isEnabled = isConnected && !busy && !starting && !(selected && running) && item.action != nil
        }
        refreshItem.isHidden = isConnected
        quitItem.isEnabled = !busy
        let decisionStatus = decisionSnapshot["status"] as? String ?? "sleeping"
        let decisionActive = decisionSnapshot["active_requests"] as? Int ?? 0
        let hasDecision = !(decisionSnapshot["models"] as? [[String: Any]] ?? []).isEmpty
        let decisionRunning = decisionStatus == "running"
        decisionInfo.title = decisionStatus == "starting" ? "載入中…" : decisionRunning ? (decisionActive > 0 ? "處理中" : "準備就緒 · \(String(describing: decisionSnapshot["device"] ?? "local").uppercased())") : "休眠待命"
        decisionItem.title = "決策模型 · Laya multilingual · " + decisionInfo.title
        decisionItem.isEnabled = isConnected && hasDecision
        decisionToggle.title = decisionStatus == "starting" ? "模型載入中…" : decisionRunning ? "決策模型休眠" : "啟動決策模型"
        decisionToggle.isEnabled = isConnected && hasDecision && !busy && decisionStatus != "starting" && decisionActive == 0 && decisionSnapshot["runtime_available"] as? Bool != false
        decisionAuto.state = decisionSnapshot["auto_sleep_enabled"] as? Bool == false ? .off : .on
        decisionAuto.isEnabled = isConnected && hasDecision && !busy
        updateIcon(status: isConnected ? (running || starting ? currentStatus : decisionStatus) : "disconnected")
    }

    private func rebuildModels() {
        modelsMenu.removeAllItems()
        for model in availableModels {
            guard let name = model["name"] as? String, let id = model["id"] as? String else { continue }
            let item = NSMenuItem(title: name, action: #selector(modelSelected(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = id
            item.toolTip = "點選即載入 · \(String(describing: model["type"] ?? "local").uppercased())"
            modelsMenu.addItem(item)
        }
        if modelsMenu.items.isEmpty {
            let empty = NSMenuItem(title: "尚無可用模型", action: nil, keyEquivalent: "")
            empty.isEnabled = false
            modelsMenu.addItem(empty)
        }
    }

    // Use the same quadratic contours as the Windows icons and dashboard.
    // Loading once keeps artwork and state changes independent of file I/O.
    private lazy var iconContours: [String: [[[Double]]]] = {
        let file = URL(fileURLWithPath: CommandLine.arguments[0])
            .deletingLastPathComponent().appendingPathComponent("watchdog-vectors.json")
        guard let data = try? Data(contentsOf: file),
              let shapes = try? JSONDecoder().decode([String: [[[Double]]]].self, from: data) else { return [:] }
        return shapes
    }()

    private func guardianIcon(status: String) -> NSImage {
        let awake = status == "running" || status == "starting"
        let speaking = status == "running" && (activeRequests > 0 || (decisionSnapshot["active_requests"] as? Int ?? 0) > 0)
        let state = !awake ? "sleeping" : speaking ? "running" : "ready"
        guard let contours = iconContours[state], !contours.isEmpty else {
            let fallback = NSImage(systemSymbolName: "dog.fill", accessibilityDescription: "WUF Watchdog") ?? NSImage(size: NSSize(width: 22, height: 22))
            fallback.isTemplate = true
            return fallback
        }
        let image = NSImage(size: NSSize(width: 22, height: 22), flipped: false) { _ in
            guard let c = NSGraphicsContext.current?.cgContext else { return false }
            c.saveGState()
            defer { c.restoreGState() }
            c.translateBy(x: 0, y: 22)
            c.scaleBy(x: 22.0 / 64, y: -22.0 / 64)
            c.setFillColor(NSColor.black.cgColor)
            for loop in contours where loop.count >= 3 && loop.allSatisfy({ $0.count == 2 }) {
                guard let first = loop.first, let last = loop.last else { continue }
                c.move(to: CGPoint(x: (last[0] + first[0]) / 2, y: (last[1] + first[1]) / 2))
                for (i, point) in loop.enumerated() {
                    let next = loop[(i + 1) % loop.count]
                    c.addQuadCurve(to: CGPoint(x: (point[0] + next[0]) / 2, y: (point[1] + next[1]) / 2),
                                   control: CGPoint(x: point[0], y: point[1]))
                }
                c.closePath()
            }
            c.drawPath(using: .eoFill)
            return true
        }
        image.isTemplate = true
        return image
    }

    private func updateIcon(status: String) {
        guard let button = statusItem.button else { return }
        button.title = ""
        button.image = guardianIcon(status: status)
        let labels = ["running": activeRequests > 0 || (decisionSnapshot["active_requests"] as? Int ?? 0) > 0 ? "運行中 · 處理請求" : "準備就緒", "starting": "正在載入", "sleeping": "休眠待命", "disconnected": "尚未連線"]
        let label = "WUF Watchdog · \(labels[status] ?? "狀態未知") · \(currentModel)"
        button.toolTip = label
        button.setAccessibilityLabel(label)
    }


    func menuWillOpen(_ menu: NSMenu) {
        rebuildModels()
        isMenuOpen = true
        refreshPresentation()
        fetchStatus()
    }
    func menuDidClose(_ menu: NSMenu) { isMenuOpen = false }

    private func perform(_ path: String, query: [URLQueryItem] = [], completion: (() -> Void)? = nil) {
        guard !busy else { return }
        busy = true; feedback = nil
        refreshPresentation()
        var components = URLComponents(string: "http://127.0.0.1:8080/api/" + path)!
        if !query.isEmpty { components.queryItems = query }
        var request = URLRequest(url: components.url!)
        request.httpMethod = "POST"
        request.timeoutInterval = 180
        URLSession.shared.dataTask(with: request) { [weak self] data, response, error in
            RunLoop.main.perform(inModes: [.default, .eventTracking]) {
                guard let self = self else { return }
                self.busy = false
                let json = data.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }
                let status = json?["status"] as? String
                if error == nil, (response as? HTTPURLResponse)?.statusCode == 200, status == "ok" || status == "stopped" {
                    completion?()
                } else {
                    self.feedback = "操作未完成：" + (json?["message"] as? String ?? error?.localizedDescription ?? "請稍後重試")
                }
                self.refreshPresentation()
                self.fetchStatus()
            }
        }.resume()
    }
    @objc private func modelSelected(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String else { return }
        perform("switch", query: [URLQueryItem(name: "model", value: id)])
    }
    @objc private func toggleEngine() { perform(currentStatus == "running" ? "sleep" : "wake") }
    @objc private func toggleAutoSleep() { perform("toggle_auto_sleep") }
    @objc private func toggleDecision() { perform(decisionSnapshot["status"] as? String == "running" ? "decision/sleep" : "decision/wake") }
    @objc private func toggleDecisionAuto() { perform("decision/toggle_auto_sleep") }
    @objc private func reconnect() { feedback = nil; fetchStatus() }
    @objc private func copyApiUrl() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString("http://127.0.0.1:8080/v1", forType: .string)
        feedback = "API 網址已複製"
        refreshPresentation()
    }
    @objc private func openWebDashboard() {
        NSWorkspace.shared.open(URL(string: "http://127.0.0.1:8080/status")!)
    }
    @objc private func quitApp() {
        let alert = NSAlert()
        alert.messageText = "結束 WUF Watchdog？"
        alert.informativeText = "將停止模型與本機 API 服務。需要時可重新開啟 WUF Watchdog app。"
        alert.addButton(withTitle: "取消")
        alert.addButton(withTitle: "結束 WUF Watchdog")
        NSApp.activate(ignoringOtherApps: true)
        guard alert.runModal() == .alertSecondButtonReturn else { return }
        if !isConnected { NSApp.terminate(nil); return }
        perform("stop") { NSApp.terminate(nil) }
    }
}

// 主程式入口
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let controller = WUFMenuBarController()
app.run()
