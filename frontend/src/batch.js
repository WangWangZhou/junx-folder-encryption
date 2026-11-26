import { Events, Dialogs } from "@wailsio/runtime";
import * as FolderService from "../bindings/folderhide/backend/folderservice.js";
import { BatchFilesDropped, FolderProgress } from "../bindings/events/index.js";
import { t, getLocale, onLocaleChange } from "./i18n.js";

const el = (id) => document.getElementById(id);

const queueList = el("queueList");
const pendingTab = el("queuePendingTab");
const runningTab = el("queueRunningTab");
const doneTab = el("queueDoneTab");
const pendingCount = el("queuePendingCount");
const runningCount = el("queueRunningCount");
const doneCount = el("queueDoneCount");
const btnSweep = el("queueSweepBtn");
const btnPin = el("batchPinBtn");
const btnEncryptMode = el("batchEncryptMode");
const btnDecryptMode = el("batchDecryptMode");
const pathInput = el("batchPath");
const outDirInput = el("batchOutDir");
const deleteChk = el("batchDelete");
const addHint = el("batchAddHint");
const btnRun = el("batchRun");
const btnBack = el("batchBack");

const PIN_TOP_KEY = "folder-hide-batch-pin-top";

// 六个队列：encrypt/decrypt × pending/running/done。
// mode 只表示当前查看和运行哪个模式；任务自身携带 mode，拖入混合类型时自动分流。
const state = {
    mode: "encrypt",
    view: "pending",
    tasks: [],
    running: { encrypt: false, decrypt: false },
    currentId: { encrypt: "", decrypt: "" },
};

function readPinTop() {
    try {
        const saved = localStorage.getItem(PIN_TOP_KEY);
        return saved === null ? true : saved === "1";
    } catch (_) {
        return true;
    }
}

async function applyPinTop(enabled) {
    btnPin.setAttribute("aria-pressed", enabled ? "true" : "false");
    try {
        await FolderService.SetBatchAlwaysOnTop(enabled);
    } catch (err) {
        setHint(err && err.message ? err.message : String(err));
    }
    try {
        localStorage.setItem(PIN_TOP_KEY, enabled ? "1" : "0");
    } catch (_) {
        // 持久化失败只影响下次启动的默认状态。
    }
}

btnPin.addEventListener("click", () => {
    applyPinTop(btnPin.getAttribute("aria-pressed") !== "true");
});

function setHint(message) {
    addHint.textContent = message || "";
}

function isDialogCancelled(err) {
    const message = String(err && err.message ? err.message : err || "").toLowerCase();
    return message.includes("cancel");
}

function baseName(path) {
    return String(path).split(/[\\/]/).pop() || String(path);
}

function sameSource(a, b) {
    return String(a).replace(/[\\/]+$/, "").toLowerCase() === String(b).replace(/[\\/]+$/, "").toLowerCase();
}

function isSubPath(parent, child) {
    const p = String(parent).replace(/[\\/]+$/, "").toLowerCase();
    const c = String(child).replace(/[\\/]+$/, "").toLowerCase();
    return c.startsWith(p + "\\") || c.startsWith(p + "/");
}

function statusOf(task) {
    if (task.finishedAt) return "done";
    if (task.state === "running") return "running";
    return "pending";
}

function tasksOf(mode, status) {
    return state.tasks.filter((task) => task.mode === mode && statusOf(task) === status);
}

function runnableOf(mode) {
    return tasksOf(mode, "pending").filter((task) => task.state === "pending");
}

// 后端进度事件带 TaskID，两个模式同时运行时也能准确落到对应任务卡。
Events.On(FolderProgress, (event) => {
    const data = event.data || {};
    const taskID = data.TaskID || data.taskID || data.taskId || "";
    if (!taskID) return;
    const task = state.tasks.find((item) => item.id === taskID);
    if (!task) return;
    const percent = Number(data.Percent ?? data.percent ?? 0);
    const phase = data.Phase ?? data.phase ?? "";
    task.percent = phase === "done" ? 100 : percent;
    updateTaskVisual(task);
});

function stateLabel(task) {
    if (task.finishedAt) {
        return task.state === "error" ? t("queue.stateFailed") : t("queue.stateDone");
    }
    if (task.state === "running") return t("queue.stateRunning", { percent: task.percent });
    if (task.state === "paused") return t("queue.statePaused");
    return t("queue.statePending");
}

function buildTaskButtons(task) {
    const box = document.createElement("span");
    box.className = "queue-actions";

    if (!task.finishedAt) {
        const toggle = document.createElement("button");
        toggle.type = "button";
        toggle.textContent = task.state === "paused" ? "▶" : "⏸";
        toggle.title = t(task.state === "paused" ? "batch.resume" : "batch.pause");
        toggle.disabled = task.state === "running";
        toggle.addEventListener("click", () => togglePause(task));
        box.appendChild(toggle);
    }

    const remove = document.createElement("button");
    remove.type = "button";
    remove.textContent = "×";
    remove.title = t("batch.remove");
    remove.disabled = task.state === "running";
    remove.addEventListener("click", () => removeTask(task.id));
    box.appendChild(remove);
    return box;
}

function renderTask(task) {
    const item = document.createElement("div");
    item.className = "queue-item " + (task.finishedAt ? (task.state === "error" ? "error" : "done") : task.state);
    item.dataset.id = task.id;

    const top = document.createElement("div");
    top.className = "queue-item-top";
    const name = document.createElement("span");
    name.className = "queue-name";
    name.textContent = task.name;
    name.title = task.source;
    const stateNode = document.createElement("span");
    stateNode.className = "queue-state";
    stateNode.textContent = stateLabel(task);
    top.append(name, stateNode);
    item.appendChild(top);

    const bottom = document.createElement("div");
    bottom.className = "queue-item-bottom";
    const bar = document.createElement("span");
    bar.className = "queue-bar";
    const fill = document.createElement("span");
    fill.className = "queue-bar-fill";
    fill.style.width = (task.finishedAt ? 100 : task.percent) + "%";
    bar.appendChild(fill);
    bottom.appendChild(bar);
    bottom.appendChild(buildTaskButtons(task));
    item.appendChild(bottom);

    // 右侧输出名保留，且仅加密任务使用；解密任务仍按容器内原名还原。
    const outRow = document.createElement("div");
    outRow.className = "queue-out";
    const outLabel = document.createElement("span");
    outLabel.textContent = t("batch.outNameShort");
    const out = document.createElement("input");
    out.type = "text";
    out.value = task.outName || "";
    out.placeholder = t("batch.outPlaceholder");
    out.disabled = !!task.finishedAt || state.running[task.mode];
    out.addEventListener("change", () => {
        task.outName = out.value.trim();
    });
    outRow.append(outLabel, out);
    item.appendChild(outRow);
    return item;
}

function updateTaskVisual(task) {
    const node = queueList.querySelector('[data-id="' + task.id + '"]');
    if (!node) return;
    const fill = node.querySelector(".queue-bar-fill");
    if (fill) fill.style.width = task.percent + "%";
    const stateNode = node.querySelector(".queue-state");
    if (stateNode) stateNode.textContent = stateLabel(task);
}

function renderQueue() {
    const pending = tasksOf(state.mode, "pending");
    const running = tasksOf(state.mode, "running");
    const done = tasksOf(state.mode, "done");
    const list = state.view === "running" ? running : state.view === "done" ? done : pending;

    queueList.replaceChildren();
    list.forEach((task) => queueList.appendChild(renderTask(task)));

    pendingCount.textContent = String(pending.length);
    runningCount.textContent = String(running.length);
    doneCount.textContent = String(done.length);

    pendingTab.classList.toggle("active", state.view === "pending");
    runningTab.classList.toggle("active", state.view === "running");
    doneTab.classList.toggle("active", state.view === "done");

    btnEncryptMode.classList.toggle("active", state.mode === "encrypt");
    btnDecryptMode.classList.toggle("active", state.mode === "decrypt");
    btnRun.textContent = t(state.mode === "encrypt" ? "batch.runEncrypt" : "batch.runDecrypt");
    btnRun.disabled = state.running[state.mode] || runnableOf(state.mode).length === 0;

    // 清扫只在全局无运行任务、且当前标签为未开始/已完成时可用；范围是当前模式、当前标签。
    const globalIdle = !state.running.encrypt && !state.running.decrypt;
    const sweepCount = state.view === "pending" ? pending.length : state.view === "done" ? done.length : 0;
    btnSweep.disabled = !globalIdle || state.view === "running" || sweepCount === 0;
    const sweepTipKey = !globalIdle
        ? "batch.sweepRunningTip"
        : state.view === "pending"
            ? "batch.sweepPendingTip"
            : state.view === "done"
                ? "batch.sweepDoneTip"
                : "batch.sweepRunningTip";
    btnSweep.title = t(sweepTipKey);
    btnSweep.setAttribute("aria-label", t(sweepTipKey));
}

function togglePause(task) {
    if (task.state === "running" || task.finishedAt) return;
    task.state = task.state === "paused" ? "pending" : "paused";
    renderQueue();
}

function removeTask(id) {
    const index = state.tasks.findIndex((task) => task.id === id);
    if (index < 0) return;
    if (state.tasks[index].state === "running") return;
    state.tasks.splice(index, 1);
    renderQueue();
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

async function addPaths(paths) {
    if (!Array.isArray(paths) || paths.length === 0) return;
    const outputDir = outDirInput.value.trim();
    let probes = [];
    try {
        probes = await FolderService.ProbePaths(paths, outputDir);
    } catch (err) {
        setHint(err && err.message ? err.message : String(err));
        return;
    }

    const added = [];
    for (const probe of probes) {
        if (!probe.Exists || probe.Kind === "other") {
            setHint(t("batch.errOther", { name: baseName(probe.Path) }));
            continue;
        }
        const mode = probe.Kind === "junx" ? "decrypt" : "encrypt";
        if (state.tasks.some((task) => task.mode === mode && sameSource(task.source, probe.Path))) {
            setHint(t("batch.errDuplicate", { name: baseName(probe.Path) }));
            continue;
        }
        if (mode === "encrypt") {
            if (probe.OutputExists) {
                setHint(t("batch.errOutputExists", { name: probe.OutputPath }));
                continue;
            }
            const list = tasksOf("encrypt", "pending").concat(tasksOf("encrypt", "running"));
            if (list.some((task) => isSubPath(task.source, probe.Path))) {
                setHint(t("batch.errNested", { name: baseName(probe.Path) }));
                continue;
            }
            const parents = list.filter((task) => isSubPath(probe.Path, task.source));
            if (parents.length) {
                const merge = await askMerge(probe.Path, parents.length);
                if (!merge) continue;
                parents.forEach((task) => removeTask(task.id));
            }
        }
        const task = {
            id: "t" + Date.now().toString(36) + Math.random().toString(36).slice(2, 6),
            mode,
            source: probe.Path,
            name: baseName(probe.Path),
            outName: "",
            outDir: outputDir,
            state: "pending",
            percent: 0,
            error: "",
            finishedAt: 0,
        };
        state.tasks.push(task);
        added.push(task);
    }

    state.view = "pending";
    const addedModes = new Set(added.map((task) => task.mode));
    if (addedModes.size === 1 && !addedModes.has(state.mode)) {
        state.mode = [...addedModes][0];
    }
    renderQueue();
    setHint("");
}

async function addFromInput() {
    const path = pathInput.value.trim();
    if (!path) {
        setHint(t("msg.needSource"));
        return;
    }
    const before = new Set(state.tasks.map((task) => task.id));
    await addPaths([path]);
    const added = state.tasks.find((task) => !before.has(task.id));
    if (added) {
        added.outDir = outDirInput.value.trim();
    }
    pathInput.value = "";
    renderQueue();
}

async function runMode(mode) {
    if (state.running[mode]) return;
    const list = runnableOf(mode);
    if (!list.length) {
        setHint(t("batch.errNoPending"));
        return;
    }
    state.running[mode] = true;
    state.view = "running";
    renderQueue();

    try {
        for (const task of list) {
            if (task.state !== "pending") continue;
            task.state = "running";
            task.percent = 0;
            state.currentId[mode] = task.id;
            renderQueue();
            try {
                if (mode === "encrypt") {
                    await FolderService.EncryptPathEx(
                        task.id,
                        task.source,
                        task.outName,
                        task.outDir,
                        deleteChk.checked,
                        getLocale(),
                    );
                } else {
                    await FolderService.DecryptJunxToDir(task.id, task.source, task.outDir);
                }
                task.state = "done";
            } catch (err) {
                task.state = "error";
                task.error = err && err.message ? err.message : String(err);
            }
            task.finishedAt = Date.now();
            task.percent = 100;
            state.currentId[mode] = "";
            renderQueue();
        }
    } finally {
        state.running[mode] = false;
        state.currentId[mode] = "";
        renderQueue();
    }
}

el("batchAdd").addEventListener("click", () => {
    addFromInput().catch((err) => setHint(err && err.message ? err.message : String(err)));
});
btnRun.addEventListener("click", () => {
    runMode(state.mode).catch((err) => setHint(err && err.message ? err.message : String(err)));
});
btnBack.addEventListener("click", async () => {
    try {
        await FolderService.RestoreMainWindow();
    } catch (err) {
        setHint(err && err.message ? err.message : String(err));
    }
});

btnEncryptMode.addEventListener("click", () => switchMode("encrypt"));
btnDecryptMode.addEventListener("click", () => switchMode("decrypt"));
function switchMode(mode) {
    state.mode = mode;
    state.view = "pending";
    renderQueue();
}

[pendingTab, runningTab, doneTab].forEach((tab) => {
    tab.addEventListener("click", () => {
        state.view = tab.dataset.status || "pending";
        renderQueue();
    });
});
btnSweep.addEventListener("click", () => {
    if (btnSweep.disabled) return;
    const mode = state.mode;
    if (state.view === "pending") {
        state.tasks = state.tasks.filter((task) => !(task.mode === mode && statusOf(task) === "pending"));
    } else if (state.view === "done") {
        state.tasks = state.tasks.filter((task) => !(task.mode === mode && task.finishedAt));
    }
    renderQueue();
});

el("batchBrowse").addEventListener("click", async () => {
    try {
        const selected = await Dialogs.OpenFile({
            CanChooseFiles: true,
            CanChooseDirectories: true,
            AllowsMultipleSelection: false,
            Title: t("batch.source"),
        });
        const path = typeof selected === "string" ? selected : "";
        if (path) {
            pathInput.value = path;
            setHint("");
        }
    } catch (err) {
        if (!isDialogCancelled(err)) setHint(err && err.message ? err.message : String(err));
    }
});

el("batchOutDirBrowse").addEventListener("click", async () => {
    try {
        const selected = await Dialogs.OpenFile({
            CanChooseFiles: false,
            CanChooseDirectories: true,
            AllowsMultipleSelection: false,
            Title: t("batch.outDirBrowse"),
        });
        const path = typeof selected === "string" ? selected : "";
        if (path) {
            outDirInput.value = path;
            setHint("");
        }
    } catch (err) {
        if (!isDialogCancelled(err)) setHint(err && err.message ? err.message : String(err));
    }
});

// 拖放：Wails 主窗口与批量窗口各自注册事件，这里只接收批量窗口的拖放载荷。
Events.On(BatchFilesDropped, async (event) => {
    const data = event.data || {};
    const paths = data.Paths || data.paths || [];
    if (!Array.isArray(paths) || paths.length === 0) return;
    if (data.Target && data.Target !== "batchDrop") return;
    await addPaths(paths);
});

onLocaleChange(() => {
    renderQueue();
});

async function init() {
    applyPinTop(readPinTop());
    renderQueue();
    try {
        const seed = await FolderService.TakeBatchSeed();
        if (seed && seed.length) await addPaths(seed);
    } catch (err) {
        setHint(err && err.message ? err.message : String(err));
    }
}

init();
