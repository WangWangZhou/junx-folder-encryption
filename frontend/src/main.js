import { Events, Dialogs, Window } from "@wailsio/runtime";
import * as FolderService from "../bindings/folderhide/backend/folderservice.js";
import { FilesDropped, FolderLog, FolderProgress } from "../bindings/events/index.js";
import { t, getLocale, setLocale, onLocaleChange } from "./i18n.js";

const el = (id) => document.getElementById(id);
const logBox = el("logBox");
const logGroup = el("logGroup");
const langSelect = el("langSelect");
const autoStartChk = el("autoStartChk");
const runAsAdminChk = el("runAsAdminChk");
const runAsAdminLabel = el("runAsAdminLabel");
const pinEnableChk = el("pinEnableChk");
const pinSetupOverlay = el("pinSetupOverlay");
const setupPinBoxes = el("setupPinBoxes");
const confirmPinBoxes = el("confirmPinBoxes");
const pinClearBtn = el("pinClearBtn");
const pinSaveBtn = el("pinSaveBtn");
const pinCancelBtn = el("pinCancelBtn");
const pinSetupError = el("pinSetupError");
const lockOverlay = el("lockOverlay");
const unlockMessage = el("unlockMessage");
const unlockPin = el("unlockPin");
const autoLockChk = el("autoLockChk");
const autoLockLabel = el("autoLockLabel");
const manualLockBtn = el("manualLockBtn");
const pinTopBtn = el("pinTopBtn");
const openConfigBtn = el("openConfigBtn");
const openConfigFolderBtn = el("openConfigFolderBtn");
const changeConfigBtn = el("changeConfigBtn");
const logLevelSelect = el("logLevelSelect");
const openLogFileBtn = el("openLogFileBtn");
const resetConfigBtn = el("resetConfigBtn");
const quitBtn = el("quitBtn");
const clearRecentBtn = el("clearRecentBtn");
const showDesktopIconsBtn = el("showDesktopIconsBtn");
const alignStartLeftBtn = el("alignStartLeftBtn");
const showHiddenFilesBtn = el("showHiddenFilesBtn");
const removeShortcutArrowBtn = el("removeShortcutArrowBtn");

// 打开密码固定为 4 位、无限次尝试。
const PIN_LENGTH = 4;
const PIN_POLICY = "unlimited";

// 这两页内容较高：切到它们时隐藏日志区，并按内容调整窗口高度。
const TALL_TAB_IDS = new Set(["tabGeneral", "tabOther"]);

let currentTab = "tabHide";
let pinState = { Enabled: false, Length: 4, Policy: "unlimited", Locked: false, RemainingSeconds: 0, AttemptsLeft: -1, AutoLock: false, SessionLocked: false };
let unlockCode = "";
let setupCode = "";
let unlocking = false;

// 仅同步自动锁定相关控件，不重建设置区的 PIN 输入框（避免打断用户输入）。
function syncAutoLockControls(state) {
    const canLock = !!state.Enabled;
    autoLockChk.disabled = !canLock;
    autoLockChk.checked = canLock && !!state.AutoLock;
    autoLockLabel.classList.toggle("is-disabled", !canLock);
    manualLockBtn.disabled = !canLock;
}

// 会话锁定状态变化时立即弹出解锁页；由轮询与本地操作共同驱动。
function syncLockOverlay(state) {
    if (!state || !state.Enabled || !state.SessionLocked) return;
    if (!lockOverlay.classList.contains("hidden")) return;
    pinState = state;
    syncAutoLockControls(state);
    showLockOverlay(state);
    setUnlockMessage("");
}

// 日志为空时（例如刚清空）显示与输入框 placeholder 同色的提示文字。
function refreshLogPlaceholder() {
    logBox.dataset.empty = t("log.empty");
}

// initial 为 true 时用 placeholder 灰色显示（如启动时的版本行），
// 真实操作日志统一黑色，错误/警告仍按级别着色。
function appendLog(text, initial = false) {
    const line = document.createElement("div");
    line.className = "log-line";
    const value = String(text);
    if (value.includes("[ERROR]")) {
        line.classList.add("error");
    } else if (value.includes("[WARN]")) {
        line.classList.add("warn");
    } else if (initial) {
        line.classList.add("initial");
    }
    line.textContent = value;
    logBox.appendChild(line);
    logBox.scrollTop = logBox.scrollHeight;
}

function showError(err) {
    const message = err && err.message ? err.message : String(err);
    appendLog("[ERROR] " + message);
}

function hiddenPathFor(path, newName) {
    const clean = path.replace(/[\\/]+$/, "");
    if (!newName) {
        return clean + "..";
    }
    const index = Math.max(clean.lastIndexOf("\\"), clean.lastIndexOf("/"));
    const parent = index >= 0 ? clean.slice(0, index + 1) : "";
    return parent + newName + "..";
}

function restoredPathFor(path) {
    return path.replace(/\.\.$/, "");
}

// 置顶（总在最前）：默认开启，点击图钉切换，选择记在 localStorage。
const PIN_TOP_KEY = "folder-hide-pin-top";

function readPinTop() {
    try {
        const saved = localStorage.getItem(PIN_TOP_KEY);
        return saved === null ? true : saved === "1";
    } catch (_) {
        return true;
    }
}

async function applyPinTop(enabled) {
    pinTopBtn.setAttribute("aria-pressed", enabled ? "true" : "false");
    try {
        await Window.SetAlwaysOnTop(enabled);
    } catch (err) {
        showError(err);
        return;
    }
    try {
        await FolderService.SetAlwaysOnTop(enabled);
    } catch (err) {
        showError(err);
    }
    try {
        localStorage.setItem(PIN_TOP_KEY, enabled ? "1" : "0");
    } catch (_) {
        // 持久化失败只影响下次启动的默认状态，不影响本次切换。
    }
}

pinTopBtn.addEventListener("click", () => {
    applyPinTop(pinTopBtn.getAttribute("aria-pressed") !== "true");
});

// 一级标签切换：面板互斥显示，常规/关于页隐藏日志区腾出空间。
function switchTab(tabId) {
    // 批量运行期间锁定在加密页，避免队列跑在后台却看不到进度。
    if (batch.running && tabId !== "tabEnc") return;
    document.querySelectorAll(".tab").forEach((item) => {
        item.classList.toggle("active", item.dataset.tab === tabId);
    });
    document.querySelectorAll(".panel").forEach((item) => {
        item.classList.toggle("active", item.id === tabId);
    });
    currentTab = tabId;
    refreshLogPlaceholder();
    logGroup.classList.toggle("hidden", tabId === "tabAbout" || TALL_TAB_IDS.has(tabId));

    if (TALL_TAB_IDS.has(tabId) && !batch.open) {
        // 面板显示后测量一次；命中高度缓存时直接到位，避免多次 SetSize 造成卡顿。
        requestAnimationFrame(() => resizePanelWindow().catch(showError));
    } else if (!batch.open && lastPanelHeight !== SINGLE_SIZE.height) {
        lastPanelHeight = SINGLE_SIZE.height;
        setWindowSize(SINGLE_SIZE).catch(showError);
    }

    // 离开批量任务所属页面时收起面板，避免跨页操作队列。
    if (batch.open && tabId !== "tabEnc") closeBatch();
}

document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => switchTab(tab.dataset.tab));
});

// 二级标签切换：仅切换「文件夹隐藏」下的方案A / 方案B。
function switchSub(subId) {
    document.querySelectorAll(".subtab").forEach((item) => {
        item.classList.toggle("active", item.dataset.sub === subId);
    });
    document.querySelectorAll(".subpanel").forEach((item) => {
        item.classList.toggle("active", item.id === subId);
    });
}

document.querySelectorAll(".subtab").forEach((tab) => {
    tab.addEventListener("click", () => switchSub(tab.dataset.sub));
});

el("encryptBtnA").addEventListener("click", async () => {
    const path = el("targetPathA").value.trim();
    const newName = el("newName").value.trim();
    if (!path) {
        showError(t("msg.needPath"));
        return;
    }
    try {
        await FolderService.EncryptFolder(path, newName);
        el("targetPathA").value = hiddenPathFor(path, newName);
        el("newName").value = "";
    } catch (err) {
        showError(err);
    }
});

el("decryptBtnA").addEventListener("click", async () => {
    const path = el("targetPathA").value.trim();
    if (!path) {
        showError(t("msg.needDecryptPath"));
        return;
    }
    try {
        await FolderService.DecryptFolder(path);
        el("targetPathA").value = restoredPathFor(path);
    } catch (err) {
        showError(err);
    }
});

el("encryptBtnD").addEventListener("click", async () => {
    const path = el("targetPathD").value.trim();
    if (!path) {
        showError(t("msg.needPath"));
        return;
    }
    try {
        await FolderService.EncryptFolderByACL(path);
    } catch (err) {
        showError(err);
    }
});

el("decryptBtnD").addEventListener("click", async () => {
    const path = el("targetPathD").value.trim();
    if (!path) {
        showError(t("msg.needDecryptPath"));
        return;
    }
    try {
        await FolderService.DecryptFolderByACL(path);
    } catch (err) {
        showError(err);
    }
});

// 加密页配置：文件与文件夹合并为同一页，类型由后端自动识别。
const encPage = {
    src: el("encSrc"),
    out: el("encOut"),
    del: el("encDelete"),
    fill: el("encFill"),
    text: el("encText"),
    start: el("encStart"),
    decrypt: el("encDecrypt"),
    pick: el("encPick"),
    batch: el("encBatch"),
    needMsg: "msg.needSource",
};

// ================= 批量任务 =================
// 队列只在前端维护：单线程串行调用现有单任务接口，暂停/删除仅作用于排队中的任务。
// 批量态只向右扩宽，高度与常规态一致，避免鼠标下的按钮上下跳动。
const BATCH_SIZE = { width: 576, height: 400 };
const SINGLE_SIZE = { width: 414, height: 400 };

const batchPanel = el("batchPanel");
const batchTasksBox = el("batchTasks");
const batchEmpty = el("batchEmpty");
const batchModeLabel = el("batchMode");
const batchGridBtn = el("batchGridBtn");
const batchListBtn = el("batchListBtn");

const batch = {
    open: false,
    view: "grid",       // grid | list
    mode: "",           // "" | encrypt | decrypt，由第一个任务决定
    tasks: [],           // 文件与文件夹共用同一队列
    running: false,
    total: 0,           // 启动时冻结，暂停/删除时递减
    done: 0,
    currentId: "",
};

function batchList() {
    return batch.tasks;
}

function readBatchSettings() {
    // 批量入口固定开启；迁移旧配置时同步写入两个兼容字段。
    return { file: true, folder: true };
}

function baseName(path) {
    const clean = String(path).replace(/[\\/]+$/, "");
    const index = Math.max(clean.lastIndexOf("\\"), clean.lastIndexOf("/"));
    return index >= 0 ? clean.slice(index + 1) : clean;
}

function sameSource(a, b) {
    return a.replace(/[\\/]+$/, "").toLowerCase() === b.replace(/[\\/]+$/, "").toLowerCase();
}

function isSubPath(parent, child) {
    const p = String(parent).replace(/[\\/]+$/, "").toLowerCase();
    const c = String(child).replace(/[\\/]+$/, "").toLowerCase();
    return c.startsWith(p + "\\") || c.startsWith(p + "/");
}

function taskTitle(task) {
    const percent = task.state === "running" ? " · " + task.percent + "%" : "";
    return task.name + percent + (task.error ? " · " + task.error : "");
}

// 第一个任务决定批量性质；之后类型不符直接拒绝。
async function checkBatchPath(probe, list) {
    if (!probe.Exists) {
        return { ok: false, reason: t("batch.errMissing", { name: baseName(probe.Path) }) };
    }
    if (probe.Kind === "other") {
        return { ok: false, reason: t("batch.errOther", { name: baseName(probe.Path) }) };
    }

    const mode = probe.Kind === "junx" ? "decrypt" : "encrypt";
    if (batch.mode && batch.mode !== mode) {
        return {
            ok: false,
            reason: t(batch.mode === "encrypt" ? "batch.errModeEncrypt" : "batch.errModeDecrypt", {
                name: baseName(probe.Path),
            }),
        };
    }

    if (list.some((task) => sameSource(task.source, probe.Path))) {
        return { ok: false, reason: t("batch.errDuplicate", { name: baseName(probe.Path) }) };
    }

    if (mode === "encrypt" && probe.OutputExists) {
        return { ok: false, reason: t("batch.errOutputExists", { name: probe.OutputPath }) };
    }

    if (mode === "encrypt") {
        // 新路径落在已有任务内部时直接拒绝，避免父子路径被重复打包。
        if (list.some((task) => isSubPath(task.source, probe.Path))) {
            return { ok: false, reason: t("batch.errNested", { name: baseName(probe.Path) }) };
        }
        // 新路径包含已有任务时询问是否移除子任务，只打包父路径。
        const parents = list.filter((task) => isSubPath(probe.Path, task.source));
        if (parents.length) {
            const merge = await askMerge(probe.Path, parents.length);
            if (!merge) {
                return { ok: false, reason: t("batch.errMergeCanceled", { name: baseName(probe.Path) }) };
            }
            parents.forEach((task) => removeTaskSilently(task.id));
        }
    }
    return { ok: true, mode };
}

async function askMerge(path, count) {
    try {
        const yes = t("btn.merge");
        const answer = await Dialogs.Question({
            Title: t("batch.mergeTitle"),
            Message: t("batch.mergeMessage", { name: baseName(path), count }),
            Buttons: [
                { Label: yes, IsDefault: true },
                { Label: t("btn.cancel"), IsCancel: true },
            ],
        });
        return answer === yes;
    } catch (_) {
        return false;
    }
}

async function addBatchPaths(paths) {
    if (batch.running) {
        showError(t("batch.errRunning"));
        return;
    }
    let probes = [];
    try {
        probes = await FolderService.ProbePaths(paths);
    } catch (err) {
        showError(err);
        return;
    }

    const list = batchList();
    for (const probe of probes) {
        const verdict = await checkBatchPath(probe, list);
        if (!verdict.ok) {
            appendLog("[WARN] " + verdict.reason);
            continue;
        }
        list.push({
            id: "t" + Date.now().toString(36) + Math.random().toString(36).slice(2, 6),
            source: probe.Path,
            name: baseName(probe.Path),
            state: "pending",
            percent: 0,
            phase: "",
            error: "",
        });
        if (!batch.mode) batch.mode = verdict.mode;
    }
    renderBatch();
}

function buildTaskButtons(task) {
    const box = document.createElement("span");
    box.className = "task-buttons";

    const pause = document.createElement("button");
    pause.type = "button";
    pause.className = "task-pause" + (task.state === "paused" ? " resume" : "");
    pause.title = t(task.state === "paused" ? "batch.resume" : "batch.pause");
    pause.addEventListener("click", (event) => {
        event.stopPropagation();
        togglePause(task);
    });

    const del = document.createElement("button");
    del.type = "button";
    del.className = "task-del";
    del.textContent = "×";
    del.title = t("batch.remove");
    del.addEventListener("click", (event) => {
        event.stopPropagation();
        removeTask(task.id);
    });

    box.append(pause, del);
    return box;
}

function renderTile(task) {
    const tile = document.createElement("div");
    tile.className = "task-tile " + task.state;
    tile.dataset.id = task.id;
    tile.title = taskTitle(task);

    const fill = document.createElement("div");
    fill.className = "task-fill";
    fill.style.height = task.percent + "%";
    tile.appendChild(fill);

    if (task.state !== "running") {
        const hover = document.createElement("div");
        hover.className = "task-hover";
        hover.appendChild(buildTaskButtons(task));
        tile.appendChild(hover);
    }
    return tile;
}

function renderRow(task) {
    const row = document.createElement("div");
    row.className = "task-row " + task.state;
    row.dataset.id = task.id;
    row.title = taskTitle(task);

    const name = document.createElement("span");
    name.className = "task-name";
    name.textContent = task.name;
    row.appendChild(name);

    const bar = document.createElement("span");
    bar.className = "task-bar";
    const fill = document.createElement("span");
    fill.className = "task-bar-fill";
    fill.style.width = task.percent + "%";
    bar.appendChild(fill);
    row.appendChild(bar);

    if (task.state !== "running") {
        const actions = buildTaskButtons(task);
        actions.classList.add("task-actions");
        row.appendChild(actions);
    }
    return row;
}

function renderBatch() {
    const list = batchList();
    batchTasksBox.className = "batch-tasks " + batch.view;
    batchTasksBox.replaceChildren();
    list.forEach((task) => {
        batchTasksBox.appendChild(batch.view === "grid" ? renderTile(task) : renderRow(task));
    });
    batchEmpty.classList.toggle("hidden", list.length > 0);

    if (!list.length) {
        batchModeLabel.textContent = t("batch.modeEmpty");
    } else {
        batchModeLabel.textContent = t(batch.mode === "decrypt" ? "batch.modeDecrypt" : "batch.modeEncrypt", {
            count: list.length,
        });
    }
    syncBatchControls();
}

function syncBatchControls() {
    batchGridBtn.classList.toggle("active", batch.view === "grid");
    batchListBtn.classList.toggle("active", batch.view === "list");
    if (!batch.open) return;

    const page = encPage;
    const list = batchList();
    if (batch.running) return;
    // 批量性质确定后，另一个操作按钮不可用。
    page.start.disabled = list.length > 0 && batch.mode === "decrypt";
    page.decrypt.disabled = list.length > 0 && batch.mode === "encrypt";
}

function updateTaskVisual(task) {
    const node = batchTasksBox.querySelector('[data-id="' + task.id + '"]');
    if (!node) return;
    const fill = node.querySelector(".task-fill, .task-bar-fill");
    if (fill) {
        if (batch.view === "grid") {
            fill.style.height = task.percent + "%";
        } else {
            fill.style.width = task.percent + "%";
        }
    }
    node.title = taskTitle(task);
}

function togglePause(task) {
    if (task.state === "running") return;
    if (task.state === "paused") {
        task.state = "pending";
        if (batch.running) batch.total += 1;
    } else {
        task.state = "paused";
        if (batch.running) batch.total -= 1;
    }
    renderBatch();
    refreshBatchProgress();
}

function removeTask(id) {
    const list = batchList();
    const index = list.findIndex((task) => task.id === id);
    if (index < 0) return;
    const [task] = list.splice(index, 1);
    if (batch.running && task.state === "pending") batch.total -= 1;
    if (!list.length) batch.mode = "";
    renderBatch();
    refreshBatchProgress();
}

function removeTaskSilently(id) {
    const list = batchList();
    const index = list.findIndex((task) => task.id === id);
    if (index >= 0) list.splice(index, 1);
}

// 总进度：完成数 + 当前任务进度，分母是启动时冻结的总数（暂停/删除会从分母扣除）。
function refreshBatchProgress() {
    if (!batch.running) return;
    const page = encPage;
    const current = batchList().find((task) => task.id === batch.currentId);
    const percent = current ? current.percent : 0;
    const value = batch.total > 0 ? Math.min(100, Math.round(((batch.done + percent / 100) / batch.total) * 100)) : 100;
    page.fill.style.width = value + "%";
    page.text.textContent = value + "% · " + batch.done + "/" + batch.total;
}

async function runBatch(kind) {
    if (batch.running) return;
    const page = encPage;
    const list = batchList();
    if (!list.length) {
        showError(t("batch.errEmpty"));
        return;
    }
    if (batch.mode && batch.mode !== kind) {
        showError(t(batch.mode === "encrypt" ? "batch.errRunEncrypt" : "batch.errRunDecrypt"));
        return;
    }

    batch.running = true;
    batch.done = 0;
    // 只统计本次真正会执行的排队任务：暂停与上一轮失败的任务都不计入。
    batch.total = list.filter((task) => task.state === "pending").length;
    batch.currentId = "";
    if (batch.total === 0) {
        batch.running = false;
        showError(t("batch.errNoPending"));
        return;
    }

    setBusy(page, true);
    page.batch.disabled = true;
    page.fill.style.width = "0%";
    page.text.textContent = "0% · 0/" + batch.total;

    try {
        while (true) {
            const task = batchList().find((item) => item.state === "pending");
            if (!task) break;
            task.state = "running";
            task.percent = 0;
            batch.currentId = task.id;
            renderBatch();
            try {
                if (kind === "encrypt") {
                    // 统一入口：容器类型由后端探测源文件/文件夹后决定。
                    await FolderService.EncryptPath(task.source, "", false, getLocale());
                } else {
                    // 通用解密：容器里是文件还是文件夹由清单决定。
                    await FolderService.DecryptJunx(task.source);
                }
                removeTaskSilently(task.id);
                batch.done += 1;
            } catch (err) {
                task.state = "error";
                task.percent = 100;
                task.error = err && err.message ? err.message : String(err);
                batch.done += 1;
                appendLog("[ERROR] " + task.error);
            }
            batch.currentId = "";
            if (!batchList().length) batch.mode = "";
            renderBatch();
            refreshBatchProgress();
        }
    } finally {
        batch.running = false;
        batch.currentId = "";
        setBusy(page, false);
        applyBatchUI();
        page.batch.disabled = false;
        renderBatch();
        const value = batch.total > 0 ? 100 : 0;
        page.fill.style.width = value + "%";
        page.text.textContent = value + "% · " + batch.done + "/" + batch.total;
    }
}

// 批量态：源输入框变成三倍高的拖入区，浏览/输出名称/删除原件全部禁用。
function applyBatchUI() {
    const open = batch.open;
    batchPanel.classList.toggle("hidden", !open);
    document.body.classList.toggle("batch-mode", open);

    encPage.src.classList.toggle("batch-drop", open);
    encPage.src.readOnly = open;
    encPage.out.disabled = open;
    encPage.del.disabled = open;
    encPage.pick.disabled = open;
    encPage.batch.classList.toggle("active", open);
    encPage.src.placeholder = open ? t("placeholder.batchDrop") : t("placeholder.sourcePath");
}

async function setWindowSize(size) {
    try {
        await Window.SetSize(size.width, size.height);
    } catch (_) {
        // 窗口尺寸调整失败不影响功能。
    }
}

// 内容较高的页面按内容增高：高度只测量一次并缓存，后续切换直接到位。
let lastPanelHeight = SINGLE_SIZE.height;
let panelResizeTimer = 0;
const panelHeightCache = new Map();

async function resizePanelWindow() {
    if (!TALL_TAB_IDS.has(currentTab) || batch.open) return;
    const cached = panelHeightCache.get(currentTab);
    if (cached !== undefined) {
        if (lastPanelHeight !== cached) {
            lastPanelHeight = cached;
            await setWindowSize({ width: SINGLE_SIZE.width, height: cached });
        }
        return;
    }
    const panel = el(currentTab);
    // 删除底部提示行后，只需测量面板底部是否越过窗口边界。
    const neededBottom = panel.getBoundingClientRect().bottom + 8;
    const delta = Math.ceil(neededBottom - window.innerHeight);
    const target = Math.max(SINGLE_SIZE.height, lastPanelHeight + delta);
    panelHeightCache.set(currentTab, target);
    if (target === lastPanelHeight) return;
    lastPanelHeight = target;
    await setWindowSize({ width: SINGLE_SIZE.width, height: target });
}

function schedulePanelResize() {
    if (!TALL_TAB_IDS.has(currentTab) || batch.open) return;
    clearTimeout(panelResizeTimer);
    panelResizeTimer = setTimeout(() => {
        resizePanelWindow().catch(showError);
    }, 80);
}

if (typeof ResizeObserver !== "undefined") {
    TALL_TAB_IDS.forEach((id) => {
        const panel = el(id);
        if (panel) new ResizeObserver(schedulePanelResize).observe(panel);
    });
}

async function openBatch() {
    if (batch.open) return;
    if (batch.running) return;
    if (activeProgress) {
        showError(t("msg.busy"));
        return;
    }
    batch.open = true;
    switchTab("tabEnc");
    applyBatchUI();
    renderBatch();
    resetProgress(encPage);
    await setWindowSize(BATCH_SIZE);
}

async function closeBatch() {
    if (!batch.open || batch.running) return;
    batch.open = false;
    applyBatchUI();
    renderBatch();
    await setWindowSize(SINGLE_SIZE);
}

// 同一时间只允许一个加解密任务，进度事件据此决定更新哪一页。
let activeProgress = null;

function resetProgress(page) {
    page.fill.style.width = "0%";
    page.text.textContent = t("progress.idle");
    page.text.title = "";
}

function updateProgress(page, percent, phase) {
    const value = Math.max(0, Math.min(100, Math.round(percent)));
    page.fill.style.width = value + "%";
    if (value >= 100) {
        page.text.textContent = "100% · " + t("progress.done");
        page.text.title = "";
        return;
    }
    const label = phase ? t("progress." + phase) : "";
    page.text.textContent = label ? value + "% · " + label : value + "%";
    page.text.title = label;
}

function setBusy(page, busy) {
    page.start.disabled = busy;
    page.decrypt.disabled = busy;
    page.pick.disabled = busy;
    page.src.readOnly = busy;
    page.out.readOnly = busy;
    page.del.disabled = busy;
}

async function pickPath() {
    const selected = await Dialogs.OpenFile({
        CanChooseFiles: true,
        CanChooseDirectories: true,
        AllowsMultipleSelection: false,
        Title: t("field.sourcePath"),
    });
    return typeof selected === "string" ? selected : "";
}

async function pickJunxFile() {
    const selected = await Dialogs.OpenFile({
        CanChooseFiles: true,
        CanChooseDirectories: false,
        AllowsMultipleSelection: false,
        Filters: [{ DisplayName: ".junx", Pattern: "*.junx" }],
        Title: t("msg.needJunx"),
    });
    return typeof selected === "string" ? selected : "";
}

// 个人文件夹允许加密但不允许自动删除：检测后取消勾选并禁用，提示用户手动删除。
let deleteRiskChecked = false;

async function showDeleteRiskDialog() {
    const yes = t("btn.yes");
    try {
        await Dialogs.Question({
            Title: t("msg.deleteRiskTitle"),
            Message: t("msg.deleteRisk"),
            Buttons: [{ Label: yes, IsDefault: true }],
        });
    } catch (_) {
        // 弹窗失败不影响主流程，后端仍有兜底保护。
    }
}

async function applyDeleteRisk(path, notify = false) {
    if (!path) return false;
    let risk = null;
    try {
        risk = await FolderService.CheckDeleteRisk(path);
    } catch (err) {
        // 检测失败时不改变用户选择，后端仍会兜底拒绝删除。
        return false;
    }
    const high = !!(risk && (risk.highRisk || risk.HighRisk));
    deleteRiskChecked = high;
    encPage.del.disabled = high;
    encPage.del.title = high ? t("tip.deleteRiskDisabled") : "";
    if (high && encPage.del.checked) {
        encPage.del.checked = false;
        if (notify) await showDeleteRiskDialog();
    }
    return high;
}

function setEncSource(path) {
    encPage.src.value = path;
    resetProgress(encPage);
    applyDeleteRisk(path, true).catch(showError);
}

async function runEncrypt(page) {
    if (activeProgress) {
        showError(t("msg.busy"));
        return;
    }
    const src = page.src.value.trim();
    if (!src) {
        showError(t(page.needMsg));
        return;
    }
    // 二次校验：用户可能在选路径后又勾回删除选项。
    await applyDeleteRisk(src, true);
    const outName = page.out.value.trim();
    const deleteOriginal = page.del.checked && !deleteRiskChecked;

    activeProgress = page;
    resetProgress(page);
    setBusy(page, true);
    try {
        // 类型由后端探测，文件与文件夹共用同一入口。
        await FolderService.EncryptPath(src, outName, deleteOriginal, getLocale());
    } catch (err) {
        showError(err);
    } finally {
        setBusy(page, false);
        activeProgress = null;
    }
}

async function runDecrypt(page) {
    if (activeProgress) {
        showError(t("msg.busy"));
        return;
    }
    let junxPath = page.src.value.trim();
    // 输入框里不是 .junx 时，直接弹原生选择框，避免用户手输错误后缀。
    if (!junxPath.toLowerCase().endsWith(".junx")) {
        try {
            junxPath = await pickJunxFile();
        } catch (err) {
            showError(err);
            return;
        }
        if (!junxPath) return;
        page.src.value = junxPath;
    }

    activeProgress = page;
    resetProgress(page);
    setBusy(page, true);
    try {
        // 通用解密：任何 .junx 容器都由此入口还原，类型由清单决定。
        await FolderService.DecryptJunx(junxPath);
    } catch (err) {
        showError(err);
    } finally {
        setBusy(page, false);
        activeProgress = null;
    }
}

encPage.pick.addEventListener("click", async () => {
    try {
        const path = await pickPath();
        if (path) {
            // 换目标时重置进度，并重新判定删除风险。
            setEncSource(path);
        }
    } catch (err) {
        showError(err);
    }
});
encPage.src.addEventListener("change", () => {
    applyDeleteRisk(encPage.src.value.trim(), true).catch(showError);
});

encPage.start.addEventListener("click", () => {
    if (batch.open) {
        runBatch("encrypt");
        return;
    }
    runEncrypt(encPage);
});
encPage.decrypt.addEventListener("click", () => {
    if (batch.open) {
        runBatch("decrypt");
        return;
    }
    runDecrypt(encPage);
});
encPage.batch.addEventListener("click", () => {
    if (batch.open) {
        closeBatch();
    } else {
        openBatch();
    }
});

function applyStartupState(state) {
    autoStartChk.checked = state.AutoStart;
    runAsAdminChk.checked = state.RunAsAdmin && state.AutoStart;
    runAsAdminChk.disabled = !state.AutoStart;
    runAsAdminLabel.classList.toggle("is-disabled", !state.AutoStart);
}

async function loadStartupState() {
    try {
        applyStartupState(await FolderService.GetStartupState());
    } catch (err) {
        showError(err);
    }
}

autoStartChk.addEventListener("change", async () => {
    const enabled = autoStartChk.checked;
    try {
        await FolderService.SetAutoStart(enabled);
        if (!enabled) runAsAdminChk.checked = false;
        applyStartupState({ AutoStart: enabled, RunAsAdmin: runAsAdminChk.checked });
        appendLog(t(enabled ? "log.autoStartOn" : "log.autoStartOff"));
    } catch (err) {
        autoStartChk.checked = !enabled;
        showError(err);
    }
});

runAsAdminChk.addEventListener("change", async () => {
    const enabled = runAsAdminChk.checked;
    try {
        await FolderService.SetRunAsAdmin(enabled);
        appendLog(t(enabled ? "log.runAsAdminOn" : "log.runAsAdminOff"));
    } catch (err) {
        runAsAdminChk.checked = !enabled;
        showError(err);
    }
});

function createPinBoxes(container, count, onComplete) {
    container.replaceChildren();
    const boxes = [];
    const checkComplete = () => {
        if (onComplete && collectPin(container).length === count) {
            onComplete();
        }
    };
    for (let i = 0; i < count; i += 1) {
        const box = document.createElement("input");
        // 密码默认不可见：用 password 类型，输入时只显示掩码。
        box.type = "password";
        box.className = "pin-digit";
        box.maxLength = count;
        box.inputMode = "numeric";
        box.autocomplete = "off";
        box.pattern = "[0-9]*";
        box.setAttribute("aria-label", t("pin.digit", { index: i + 1 }));

        box.addEventListener("input", () => {
            const digits = box.value.replace(/\D/g, "");
            if (digits.length > 1) {
                distributePinDigits(container, i, digits);
            } else {
                box.value = digits;
                if (digits && i < count - 1) {
                    boxes[i + 1].focus();
                }
            }
            checkComplete();
        });

        box.addEventListener("keydown", (event) => {
            if (event.key === "Backspace" && !box.value && i > 0) {
                boxes[i - 1].value = "";
                boxes[i - 1].focus();
                event.preventDefault();
            } else if (event.key === "ArrowLeft" && i > 0) {
                boxes[i - 1].focus();
                event.preventDefault();
            } else if (event.key === "ArrowRight" && i < count - 1) {
                boxes[i + 1].focus();
                event.preventDefault();
            }
        });

        box.addEventListener("paste", (event) => {
            const text = (event.clipboardData || window.clipboardData).getData("text");
            const digits = text.replace(/\D/g, "");
            if (digits) {
                event.preventDefault();
                distributePinDigits(container, i, digits);
                checkComplete();
            }
        });

        boxes.push(box);
        container.appendChild(box);
    }
    return boxes;
}

function distributePinDigits(container, start, digits) {
    const boxes = Array.from(container.querySelectorAll(".pin-digit"));
    for (let i = 0; start + i < boxes.length && i < digits.length; i += 1) {
        boxes[start + i].value = digits[i];
    }
    const next = Math.min(start + digits.length, boxes.length - 1);
    boxes[next].focus();
}

function collectPin(container) {
    return Array.from(container.querySelectorAll(".pin-digit"))
        .map((box) => box.value.replace(/\D/g, ""))
        .join("");
}

function clearPinBoxes(container) {
    container.querySelectorAll(".pin-digit").forEach((box) => {
        box.value = "";
    });
}

// 第一行输满后自动跳到确认行的第一个框。
function focusConfirmFirst() {
    const first = confirmPinBoxes.querySelector(".pin-digit");
    if (first) first.focus();
}

function pinMessage(code, state) {
    if (code === "locked" || (state && state.Locked)) {
        const seconds = state && state.RemainingSeconds ? state.RemainingSeconds : 0;
        return t("pin.locked", { seconds });
    }
    let message = t("pin.err." + code);
    if (message === "pin.err." + code) {
        message = t("pin.err.error");
    }
    if (code === "mismatch" && state && state.AttemptsLeft >= 0) {
        message += "，" + t("pin.attempts", { count: state.AttemptsLeft });
    }
    return message;
}

function setUnlockMessage(code, state) {
    unlockCode = code;
    if (!code) {
        unlockMessage.textContent = "";
        return;
    }
    if (code === "verify") {
        unlockMessage.textContent = t("pin.verifyHint");
        return;
    }
    unlockMessage.textContent = pinMessage(code, state);
}

function setPinSetupError(code) {
    setupCode = code;
    if (!code) {
        pinSetupError.textContent = "";
        return;
    }
    const message = t("pin.err." + code);
    pinSetupError.textContent = message === "pin.err." + code ? t("pin.err.error") : message;
}

// 设置弹层打开期间输入框始终可编辑；关闭时统一清理，不保留半成品输入。
function syncPinSetupEditable() {
    pinClearBtn.disabled = false;
    pinSaveBtn.disabled = false;
    setupPinBoxes.querySelectorAll(".pin-digit").forEach((box) => {
        box.disabled = false;
    });
    confirmPinBoxes.querySelectorAll(".pin-digit").forEach((box) => {
        box.disabled = false;
    });
}

function openPinSetup() {
    clearPinBoxes(setupPinBoxes);
    clearPinBoxes(confirmPinBoxes);
    setPinSetupError("");
    createPinBoxes(setupPinBoxes, PIN_LENGTH, focusConfirmFirst);
    createPinBoxes(confirmPinBoxes, PIN_LENGTH);
    syncPinSetupEditable();
    pinSetupOverlay.classList.remove("hidden");
    const first = setupPinBoxes.querySelector(".pin-digit");
    if (first) first.focus();
}

function closePinSetup() {
    pinSetupOverlay.classList.add("hidden");
    clearPinBoxes(setupPinBoxes);
    clearPinBoxes(confirmPinBoxes);
    setPinSetupError("");
    if (!pinState.Enabled) pinEnableChk.checked = false;
}

function applyPinState(state) {
    pinState = state || { Enabled: false, Length: PIN_LENGTH, Policy: PIN_POLICY, Locked: false, RemainingSeconds: 0, AttemptsLeft: -1, AutoLock: false, SessionLocked: false };
    pinEnableChk.checked = pinState.Enabled;
    // 只在弹层打开时重建输入框，避免状态轮询造成常驻页面反复重排。
    if (!pinSetupOverlay.classList.contains("hidden")) {
        createPinBoxes(setupPinBoxes, PIN_LENGTH, focusConfirmFirst);
        createPinBoxes(confirmPinBoxes, PIN_LENGTH);
        syncPinSetupEditable();
    }
    syncAutoLockControls(pinState);
}

function showLockOverlay(state) {
    createPinBoxes(unlockPin, PIN_LENGTH, doUnlock);
    setUnlockMessage(state.Locked ? "locked" : "", state);
    lockOverlay.classList.remove("hidden");
    const first = unlockPin.querySelector(".pin-digit");
    if (first) first.focus();
}

async function doUnlock() {
    if (unlocking) return;
    const pin = collectPin(unlockPin);
    if (pin.length !== PIN_LENGTH) {
        setUnlockMessage("format", pinState);
        return;
    }
    unlocking = true;
    try {
        const result = await FolderService.Unlock(pin);
        applyPinState(result.State);
        if (result.OK) {
            lockOverlay.classList.add("hidden");
            clearPinBoxes(unlockPin);
            setUnlockMessage("");
            appendLog(t("log.pinUnlocked"));
            return;
        }
        setUnlockMessage(result.Code, result.State);
        clearPinBoxes(unlockPin);
        const first = unlockPin.querySelector(".pin-digit");
        if (first) first.focus();
    } catch (err) {
        setUnlockMessage("error", pinState);
        showError(err);
    } finally {
        unlocking = false;
    }
}

el("unlockBtn").addEventListener("click", doUnlock);

autoLockChk.addEventListener("change", async () => {
    const enabled = autoLockChk.checked;
    try {
        const result = await FolderService.SetAutoLock(enabled, getLocale());
        applyPinState(result.State);
        if (!result.OK) {
            setPinSetupError(result.Code);
            return;
        }
        if (result.State.SessionLocked) {
            showLockOverlay(result.State);
            setUnlockMessage("");
        }
    } catch (err) {
        autoLockChk.checked = !enabled;
        showError(err);
    }
});

manualLockBtn.addEventListener("click", async () => {
    if (!pinState.Enabled) return;
    try {
        const result = await FolderService.Lock();
        applyPinState(result.State);
        if (!result.OK) return;
        showLockOverlay(result.State);
        setUnlockMessage("");
    } catch (err) {
        showError(err);
    }
});

pinEnableChk.addEventListener("change", async () => {
    const enabled = pinEnableChk.checked;
    if (enabled) {
        // 已启用时必须先取消，避免绕过旧密码直接重设。
        if (pinState.Enabled) {
            pinEnableChk.checked = true;
            appendLog(t("pin.alreadyEnabled"));
            return;
        }
        openPinSetup();
        return;
    }
    // 取消勾选：关闭已生效的密码。
    clearPinBoxes(setupPinBoxes);
    clearPinBoxes(confirmPinBoxes);
    setPinSetupError("");
    if (pinState.Enabled) {
        try {
            const result = await FolderService.DisablePin(getLocale());
            applyPinState(result.State);
            if (!result.OK) {
                pinEnableChk.checked = true;
                showError(t("pin.err." + result.Code));
                return;
            }
            appendLog(t("log.pinDisabled"));
        } catch (err) {
            pinEnableChk.checked = true;
            applyPinState(pinState);
            showError(err);
        }
    }
});

pinClearBtn.addEventListener("click", () => {
    clearPinBoxes(setupPinBoxes);
    clearPinBoxes(confirmPinBoxes);
    setPinSetupError("");
    const first = setupPinBoxes.querySelector(".pin-digit");
    if (first) first.focus();
});

pinSaveBtn.addEventListener("click", async () => {
    const pin = collectPin(setupPinBoxes);
    const confirm = collectPin(confirmPinBoxes);
    if (pin.length !== PIN_LENGTH || confirm.length !== PIN_LENGTH) {
        setPinSetupError("format");
        return;
    }
    if (pin !== confirm) {
        setPinSetupError("confirmMismatch");
        return;
    }
    try {
        const result = await FolderService.ConfigurePin(pin, PIN_LENGTH, PIN_POLICY);
        applyPinState(result.State);
        if (!result.OK) {
            setPinSetupError(result.Code);
            return;
        }
        pinSetupOverlay.classList.add("hidden");
        clearPinBoxes(setupPinBoxes);
        clearPinBoxes(confirmPinBoxes);
        setPinSetupError("");
        appendLog(t("log.pinEnabled"));
        showLockOverlay(result.State);
        setUnlockMessage("verify");
        const first = unlockPin.querySelector(".pin-digit");
        if (first) first.focus();
    } catch (err) {
        setPinSetupError("error");
        showError(err);
    }
});

pinCancelBtn.addEventListener("click", closePinSetup);

langSelect.addEventListener("change", async (event) => {
    const value = event.target.value;
    setLocale(value);
    try {
        await FolderService.SetLanguage(value);
    } catch (err) {
        showError(err);
    }
});

onLocaleChange(() => {
    refreshLogPlaceholder();
    // 语言变化可能改变文案长度，重新测量高页面的窗口高度。
    panelHeightCache.clear();
    if (TALL_TAB_IDS.has(currentTab)) schedulePanelResize();
    setUnlockMessage(unlockCode, pinState);
    setPinSetupError(setupCode);
    createPinBoxes(unlockPin, PIN_LENGTH, doUnlock);
    if (!pinSetupOverlay.classList.contains("hidden")) {
        createPinBoxes(setupPinBoxes, PIN_LENGTH, focusConfirmFirst);
        createPinBoxes(confirmPinBoxes, PIN_LENGTH);
        syncPinSetupEditable();
    }
    if (!activeProgress && !batch.running) {
        resetProgress(encPage);
    }
    if (batch.open) {
        applyBatchUI();
        renderBatch();
    }
});

// 常规页按钮与日志区右上角微型按钮共用同一清空逻辑：
// 只清界面日志与内存日志，不删除磁盘日志文件。
async function clearLogs() {
    logBox.replaceChildren();
    try {
        await FolderService.ClearLogs();
    } catch (err) {
        showError(err);
    }
}

el("clearLogBtn").addEventListener("click", clearLogs);
el("clearLogMiniBtn").addEventListener("click", clearLogs);

// Windows 优化会重启资源管理器，先统一确认再调用后端。
async function runTweak(invoke) {
    const yes = t("btn.yes");
    let answer = "";
    try {
        answer = await Dialogs.Question({
            Title: t("tip.restartExplorerTitle"),
            Message: t("tip.restartExplorer"),
            Buttons: [
                { Label: yes, IsDefault: true },
                { Label: t("btn.cancel"), IsCancel: true },
            ],
        });
    } catch (_) {
        return;
    }
    if (answer !== yes) return;

    try {
        await invoke();
    } catch (err) {
        const message = err && err.message ? err.message : String(err);
        if (message.includes("管理员")) {
            showError(t("err.needAdmin"));
        } else {
            showError(err);
        }
    }
}

clearRecentBtn.addEventListener("click", () => runTweak(FolderService.ClearRecentFiles));
showDesktopIconsBtn.addEventListener("click", () => runTweak(FolderService.ShowDesktopIcons));
alignStartLeftBtn.addEventListener("click", () => runTweak(FolderService.AlignTaskbarLeft));
showHiddenFilesBtn.addEventListener("click", () => runTweak(FolderService.ShowHiddenFiles));
removeShortcutArrowBtn.addEventListener("click", () => runTweak(FolderService.RemoveShortcutArrow));

openConfigBtn.addEventListener("click", async () => {
    try {
        await FolderService.OpenConfigFile();
    } catch (err) {
        showError(err);
    }
});

openConfigFolderBtn.addEventListener("click", async () => {
    try {
        await FolderService.OpenConfigFolder();
    } catch (err) {
        showError(err);
    }
});

changeConfigBtn.addEventListener("click", async () => {
    let selected = "";
    try {
        selected = await Dialogs.OpenFile({
            CanChooseFiles: false,
            CanChooseDirectories: true,
            AllowsMultipleSelection: false,
            Title: t("tip.changeConfig"),
        });
    } catch (err) {
        showError(err);
        return;
    }
    if (typeof selected !== "string" || !selected) return;
    try {
        await FolderService.ChangeConfigPath(selected);
        appendLog(t("log.configPathChanged"));
    } catch (err) {
        showError(err);
    }
});

openLogFileBtn.addEventListener("click", async () => {
    try {
        await FolderService.OpenLogFile();
    } catch (err) {
        showError(err);
    }
});

quitBtn.addEventListener("click", () => {
    FolderService.Quit();
});

resetConfigBtn.addEventListener("click", async () => {
    const yes = t("btn.yes");
    let answer = "";
    try {
        answer = await Dialogs.Question({
            Title: t("tip.resetConfigTitle"),
            Message: t("tip.resetConfigMessage"),
            Buttons: [
                { Label: yes, IsDefault: true },
                { Label: t("btn.cancel"), IsCancel: true },
            ],
        });
    } catch (_) {
        return;
    }
    if (answer !== yes) return;

    try {
        await FolderService.ResetConfig();
        const info = await FolderService.GetConfigInfo();
        setLocale(info.language);
        langSelect.value = getLocale();
        const level = ["off", "trace", "debug", "normal"].includes(info.logLevel) ? info.logLevel : "normal";
        logLevelSelect.value = level;
        previousLogLevel = level;
        // 批量入口固定显示，旧配置里的关闭状态在这里顺带纠正。
        encPage.batch.classList.remove("hidden");
        if (!info.batchFile || !info.batchFolder) {
            await FolderService.SetBatchSettings(true, true);
        }
        await applyPinTop(typeof info.alwaysOnTop === "boolean" ? info.alwaysOnTop : readPinTop());
        const state = await FolderService.GetPinState();
        applyPinState(state);
        if (!state.Enabled || !state.SessionLocked) {
            lockOverlay.classList.add("hidden");
        }
        appendLog(t("log.configReset"));
        schedulePanelResize();
    } catch (err) {
        showError(err);
    }
});

let previousLogLevel = "normal";

logLevelSelect.addEventListener("change", async () => {
    const value = logLevelSelect.value;
    const wasOff = previousLogLevel === "off";
    previousLogLevel = value;
    try {
        await FolderService.SetLogLevel(value);
        appendLog(t("log.logLevelChanged", { level: t("logLevel." + value) }));
    } catch (err) {
        showError(err);
    }
    if (value !== "off" || wasOff) return;
    let answer = "";
    try {
        answer = await Dialogs.Question({
            Title: t("group.log"),
            Message: t("log.offOpenFolder"),
            Buttons: [
                { Label: t("btn.yes"), IsDefault: true },
                { Label: t("btn.no"), IsCancel: true },
            ],
        });
    } catch (_) {
        return;
    }
    if (answer !== t("btn.yes")) return;
    try {
        await FolderService.OpenLogFolder();
    } catch (err) {
        showError(err);
    }
});

Events.On(FolderLog, (event) => appendLog(event.data));

// 后端推送的进度事件：Percent 0-100，Phase 为 scan/build/encrypt/...
Events.On(FolderProgress, (event) => {
    const data = event.data || {};
    const percent = Number(data.Percent ?? data.percent ?? 0);
    const phase = data.Phase || data.phase || "";

    // 批量运行中：事件属于当前子任务，同时刷新子任务与总进度。
    if (batch.running && batch.currentId) {
        const task = batchList().find((item) => item.id === batch.currentId);
        if (task) {
            task.percent = Math.max(0, Math.min(100, Math.round(percent)));
            task.phase = phase;
            updateTaskVisual(task);
        }
        refreshBatchProgress();
        return;
    }

    if (!activeProgress) return;
    updateProgress(activeProgress, percent, phase);
});

// 拖放文件到窗口：按落点元素决定填哪个输入框；批量面板展开时全部转成任务。
Events.On(FilesDropped, async (event) => {
    const data = event.data || {};
    const paths = data.Paths || data.paths || [];
    if (!Array.isArray(paths) || paths.length === 0) return;
    const target = data.Target || data.target || "";

    if (target !== "encDrop" && currentTab !== "tabEnc") return;

    if (batch.open) {
        // 批量面板展开时拖入的内容全部进入当前队列。
        await addBatchPaths(paths);
        return;
    }
    if (activeProgress) return;

    // 拖入新路径：复位进度并重新判定删除风险。
    setEncSource(paths[0]);
    switchTab("tabEnc");
});

// 批量入口固定开启，界面不再提供关闭开关。
encPage.batch.classList.remove("hidden");

batchGridBtn.addEventListener("click", () => {
    batch.view = "grid";
    renderBatch();
});
batchListBtn.addEventListener("click", () => {
    batch.view = "list";
    renderBatch();
});

async function init() {
    let info = null;
    try {
        info = await FolderService.GetConfigInfo();
    } catch (err) {
        showError(err);
    }
    if (info && info.needsMigration) {
        const legacy = readBatchSettings();
        try {
            await FolderService.MigrateLegacyConfig(getLocale(), readPinTop(), legacy.file, legacy.folder);
            info = await FolderService.GetConfigInfo();
        } catch (err) {
            showError(err);
        }
    }
    if (info) {
        setLocale(info.language);
        langSelect.value = getLocale();
        const level = ["off", "trace", "debug", "normal"].includes(info.logLevel) ? info.logLevel : "normal";
        logLevelSelect.value = level;
        previousLogLevel = level;
        // 批量入口固定显示，旧配置里的关闭状态在这里顺带纠正。
        encPage.batch.classList.remove("hidden");
        if (!info.batchFile || !info.batchFolder) {
            await FolderService.SetBatchSettings(true, true);
        }
        await applyPinTop(typeof info.alwaysOnTop === "boolean" ? info.alwaysOnTop : readPinTop());
    } else {
        langSelect.value = getLocale();
        await applyPinTop(readPinTop());
    }
    refreshLogPlaceholder();
    await loadStartupState();
    try {
        const state = await FolderService.GetPinState();
        applyPinState(state);
        if (state.Enabled && state.SessionLocked) {
            showLockOverlay(state);
        }
        const version = await FolderService.Version();
        appendLog(t("log.version", { version }), true);
        const logs = await FolderService.RecentLogs();
        logs.forEach((line) => appendLog(line));
    } catch (err) {
        showError(err);
    }
}

// 每 3 秒轮询会话状态：空闲 15 分钟到期后由后端惰性锁定，此处负责弹出解锁页。
let pollingLock = false;
setInterval(async () => {
    if (pollingLock || !lockOverlay.classList.contains("hidden")) return;
    pollingLock = true;
    try {
        syncLockOverlay(await FolderService.GetPinState());
    } catch (_) {
        // 轮询失败不影响本轮，下次重试。
    } finally {
        pollingLock = false;
    }
}, 3000);

init();
