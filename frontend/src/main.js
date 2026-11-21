import { Events } from "@wailsio/runtime";
import * as FolderService from "../bindings/folderhide/backend/folderservice.js";
import { FolderLog } from "../bindings/events/index.js";
import { t, getLocale, setLocale, onLocaleChange } from "./i18n.js";

const el = (id) => document.getElementById(id);
const logBox = el("logBox");
const hint = el("hint");
const logGroup = el("logGroup");
const langSelect = el("langSelect");
const autoStartChk = el("autoStartChk");
const runAsAdminChk = el("runAsAdminChk");
const runAsAdminLabel = el("runAsAdminLabel");

const hintKeyByTab = {
    tabA: "hint.a",
    tabD: "hint.d",
    tabGeneral: "hint.general",
    tabAbout: "hint.about",
};
let currentTab = "tabA";

function refreshHint() {
    hint.textContent = t(hintKeyByTab[currentTab] || "");
}

function appendLog(text) {
    const line = document.createElement("div");
    line.className = "log-line";
    const value = String(text);
    if (value.includes("[ERROR]")) {
        line.classList.add("error");
    } else if (value.includes("[WARN]")) {
        line.classList.add("warn");
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

document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
        document.querySelectorAll(".tab").forEach((item) => item.classList.remove("active"));
        document.querySelectorAll(".panel").forEach((item) => item.classList.remove("active"));
        tab.classList.add("active");
        el(tab.dataset.tab).classList.add("active");
        currentTab = tab.dataset.tab;
        refreshHint();
        // 仅方案A、方案D显示日志区
        logGroup.classList.toggle("hidden", currentTab === "tabAbout" || currentTab === "tabGeneral");
    });
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

langSelect.addEventListener("change", (event) => {
    setLocale(event.target.value);
});

onLocaleChange(() => {
    refreshHint();
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

async function init() {
    langSelect.value = getLocale();
    refreshHint();
    await loadStartupState();
    try {
        const version = await FolderService.Version();
        appendLog(t("log.version", { version }));
        const logs = await FolderService.RecentLogs();
        logs.forEach(appendLog);
    } catch (err) {
        showError(err);
    }
}

init();
