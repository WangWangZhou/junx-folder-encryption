import { Events, Dialogs, Window } from "@wailsio/runtime";
import * as FolderService from "../bindings/folderhide/backend/folderservice.js";
import { FilesDropped, FolderLog, FolderProgress } from "../bindings/events/index.js";
import { t, getLocale, setLocale, onLocaleChange } from "./i18n.js";

const el = (id) => document.getElementById(id);
const logBox = el("logBox");
const hint = el("hint");
const logGroup = el("logGroup");
const langSelect = el("langSelect");
const autoStartChk = el("autoStartChk");
const runAsAdminChk = el("runAsAdminChk");
const runAsAdminLabel = el("runAsAdminLabel");
const pinEnableChk = el("pinEnableChk");
const pinSetupRow = el("pinSetupRow");
const setupPinBoxes = el("setupPinBoxes");
const confirmPinBoxes = el("confirmPinBoxes");
const pinClearBtn = el("pinClearBtn");
const pinSaveBtn = el("pinSaveBtn");
const pinSetupError = el("pinSetupError");
const lockOverlay = el("lockOverlay");
const unlockMessage = el("unlockMessage");
const unlockPin = el("unlockPin");
const autoLockChk = el("autoLockChk");
const autoLockLabel = el("autoLockLabel");
const manualLockBtn = el("manualLockBtn");
const pinTopBtn = el("pinTopBtn");

// 打开密码固定为 4 位、无限次尝试。
const PIN_LENGTH = 4;
const PIN_POLICY = "unlimited";

const hintKeyByTab = {
    tabHide: "hint.hide",
    tabFileEnc: "hint.fileEnc",
    tabFolderEnc: "hint.folderEnc",
    tabGeneral: "hint.general",
    tabAbout: "hint.about",
};

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

function refreshHint() {
    hint.textContent = t(hintKeyByTab[currentTab] || "");
    refreshLogPlaceholder();
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
    document.querySelectorAll(".tab").forEach((item) => {
        item.classList.toggle("active", item.dataset.tab === tabId);
    });
    document.querySelectorAll(".panel").forEach((item) => {
        item.classList.toggle("active", item.id === tabId);
    });
    currentTab = tabId;
    refreshHint();
    logGroup.classList.toggle("hidden", tabId === "tabAbout" || tabId === "tabGeneral");
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

// 加密页配置：文件与文件夹两页结构一致，只有后端方法和提示语不同。
const encPages = {
    file: {
        kind: "file",
        src: el("fileEncSrc"),
        out: el("fileEncOut"),
        del: el("fileEncDelete"),
        fill: el("fileEncFill"),
        text: el("fileEncText"),
        start: el("fileEncStart"),
        decrypt: el("fileEncDecrypt"),
        pick: el("fileEncPick"),
        needMsg: "msg.needSource",
    },
    folder: {
        kind: "folder",
        src: el("folderEncSrc"),
        out: el("folderEncOut"),
        del: el("folderEncDelete"),
        fill: el("folderEncFill"),
        text: el("folderEncText"),
        start: el("folderEncStart"),
        decrypt: el("folderEncDecrypt"),
        pick: el("folderEncPick"),
        needMsg: "msg.needSourceFolder",
    },
};

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

async function pickPath(kind) {
    const selected = await Dialogs.OpenFile({
        CanChooseFiles: kind === "file",
        CanChooseDirectories: kind === "folder",
        AllowsMultipleSelection: false,
        Title: t(kind === "file" ? "field.sourceFile" : "field.sourceFolder"),
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
    const outName = page.out.value.trim();
    const deleteOriginal = page.del.checked;

    activeProgress = page;
    resetProgress(page);
    setBusy(page, true);
    try {
        if (page.kind === "file") {
            await FolderService.EncryptFileToJunx(src, outName, deleteOriginal, getLocale());
        } else {
            await FolderService.EncryptFolderToJunx(src, outName, deleteOriginal, getLocale());
        }
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
        if (page.kind === "file") {
            await FolderService.DecryptFileFromJunx(junxPath);
        } else {
            await FolderService.DecryptFolderFromJunx(junxPath);
        }
    } catch (err) {
        showError(err);
    } finally {
        setBusy(page, false);
        activeProgress = null;
    }
}

Object.values(encPages).forEach((page) => {
    page.pick.addEventListener("click", async () => {
        try {
            const path = await pickPath(page.kind);
            if (path) {
                page.src.value = path;
                // 换了新的目标后，上一轮进度条不应继续显示 100%
                resetProgress(page);
            }
        } catch (err) {
            showError(err);
        }
    });
    page.start.addEventListener("click", () => runEncrypt(page));
    page.decrypt.addEventListener("click", () => runDecrypt(page));
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

// 设置区输入框仅在“勾选打开密码且尚未设置密码”时可编辑。
// 一旦密码已启用，8 个输入框与清除/保存按钮全部只读，防止直接改写已生效的密码。
function syncPinSetupEditable() {
    const editable = pinEnableChk.checked && !pinState.Enabled;
    pinSetupRow.classList.toggle("is-disabled", !editable);
    pinClearBtn.disabled = !editable;
    pinSaveBtn.disabled = !editable;
    setupPinBoxes.querySelectorAll(".pin-digit").forEach((box) => {
        box.disabled = !editable;
    });
    confirmPinBoxes.querySelectorAll(".pin-digit").forEach((box) => {
        box.disabled = !editable;
    });
}

function applyPinState(state) {
    pinState = state || { Enabled: false, Length: PIN_LENGTH, Policy: PIN_POLICY, Locked: false, RemainingSeconds: 0, AttemptsLeft: -1, AutoLock: false, SessionLocked: false };
    pinEnableChk.checked = pinState.Enabled;
    createPinBoxes(setupPinBoxes, PIN_LENGTH, focusConfirmFirst);
    createPinBoxes(confirmPinBoxes, PIN_LENGTH);
    syncPinSetupEditable();
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
    syncPinSetupEditable();
    if (enabled) {
        // 已启用密码时复选框保持勾选但输入框只读；仅首次设置（尚未有密码）时聚焦输入。
        if (!pinState.Enabled) {
            setPinSetupError("");
            const first = setupPinBoxes.querySelector(".pin-digit");
            if (first) first.focus();
        }
        return;
    }
    // 取消勾选时清空两行已输入内容，避免残留数字。
    clearPinBoxes(setupPinBoxes);
    clearPinBoxes(confirmPinBoxes);
    setPinSetupError("");
    if (pinState.Enabled) {
        try {
            const result = await FolderService.DisablePin(getLocale());
            applyPinState(result.State);
            if (!result.OK) {
                pinEnableChk.checked = true;
                syncPinSetupEditable();
                setPinSetupError(result.Code);
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

langSelect.addEventListener("change", (event) => {
    setLocale(event.target.value);
});

onLocaleChange(() => {
    refreshHint();
    setUnlockMessage(unlockCode, pinState);
    setPinSetupError(setupCode);
    createPinBoxes(unlockPin, PIN_LENGTH, doUnlock);
    createPinBoxes(setupPinBoxes, PIN_LENGTH, focusConfirmFirst);
    createPinBoxes(confirmPinBoxes, PIN_LENGTH);
    syncPinSetupEditable();
    if (!activeProgress) {
        Object.values(encPages).forEach(resetProgress);
    }
});

el("clearLogBtn").addEventListener("click", async () => {
    logBox.replaceChildren();
    try {
        await FolderService.ClearLogs();
    } catch (err) {
        showError(err);
    }
});

Events.On(FolderLog, (event) => appendLog(event.data));

// 后端推送的进度事件：Percent 0-100，Phase 为 scan/build/encrypt/...
Events.On(FolderProgress, (event) => {
    if (!activeProgress) return;
    const data = event.data || {};
    const percent = Number(data.Percent ?? data.percent ?? 0);
    const phase = data.Phase || data.phase || "";
    updateProgress(activeProgress, percent, phase);
});

// 拖放文件到窗口：按落点元素（data-file-drop-target）决定填哪个输入框。
Events.On(FilesDropped, (event) => {
    const data = event.data || {};
    const paths = data.Paths || data.paths || [];
    if (!Array.isArray(paths) || paths.length === 0) return;
    const target = data.Target || data.target || "";

    let page = null;
    if (target === "file") {
        page = encPages.file;
    } else if (target === "folder") {
        page = encPages.folder;
    } else if (currentTab === "tabFileEnc") {
        page = encPages.file;
    } else if (currentTab === "tabFolderEnc") {
        page = encPages.folder;
    }
    if (!page || activeProgress) return;

    page.src.value = paths[0];
    // 拖入新的文件/文件夹后，进度条复位成初始的“就绪”状态
    resetProgress(page);
    switchTab(page.kind === "file" ? "tabFileEnc" : "tabFolderEnc");
});

async function init() {
    langSelect.value = getLocale();
    refreshHint();
    // 先恢复上次的置顶选择，再拉取其它状态。
    await applyPinTop(readPinTop());
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
