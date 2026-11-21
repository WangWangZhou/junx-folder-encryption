// 轻量 i18n：无框架依赖，供原生 JS 前端使用。
const STORAGE_KEY = "folder-hide-locale";

const dictionaries = {
    "zh-CN": {
        "app.title": "文件夹隐藏工具",
        "tab.a": "方案A",
        "tab.d": "方案D",
        "tab.about": "关于",
        "group.folder": "文件夹设置",
        "field.targetPath": "目标文件夹路径",
        "placeholder.path": "例如 D:\\Private\\我的文件夹",
        "field.newName": "新的文件夹名称",
        "placeholder.newName": "选填：加密前先改为该名称",
        "btn.encrypt": "加密",
        "btn.decrypt": "解密",
        "group.acl": "ACL 权限限制",
        "group.about": "关于",
        "about.title": "文件夹隐藏工具 1.2.0",
        "about.a": "<b>方案A</b>：为目录名追加 <code>..</code> 并设置隐藏属性，资源管理器双击会报“位置不可用”。",
        "about.d": "<b>方案D</b>：备份目录权限后拒绝当前用户列目录；双击无法打开，解密时从目录内 <code>.DS_Store</code> 恢复原权限。",
        "about.warn": "注意：这不是加密保护。管理员或拥有权限的账户仍可修改 ACL。操作前请备份。",
        "about.note": "元数据密钥由内置字符串派生。",
        "group.log": "日志",
        "btn.clearLog": "清空日志",
        "field.language": "显示语言:",
        "hint.a": "方案A：解密请填写带 .. 的完整路径。",
        "hint.d": "方案D：加密后目录保持原名，但无法列目录。",
        "hint.about": "关于：本工具不是加密保护，请勿用于系统目录。",
        "msg.needPath": "请填写目标文件夹路径",
        "msg.needDecryptPath": "请填写要解密的文件夹路径",
        "help.a": "[提示] 方案A：填写路径，可选新名称；加密后追加 .. 并隐藏。",
        "help.d": "[提示] 方案D：加密后目录名不变，但当前用户无法列目录；解密时恢复权限。",
        "log.version": "[信息] 文件夹隐藏工具 {version}",
        "tab.general": "常规",
        "group.general": "常规设置",
        "chk.autoStart": "开机启动",
        "chk.runAsAdmin": "以管理员身份启动",
        "tip.runAsAdmin": "启用后程序启动时将请求管理员权限（Windows 会弹出确认提示）。",
        "hint.general": "常规：设置显示语言与开机启动选项。",
        "log.autoStartOn": "[信息] 已启用开机启动",
        "log.autoStartOff": "[信息] 已关闭开机启动",
        "log.runAsAdminOn": "[信息] 已启用以管理员身份启动",
        "log.runAsAdminOff": "[信息] 已关闭以管理员身份启动",
    },
    en: {
        "app.title": "Folder Hide Tool",
        "tab.a": "Scheme A",
        "tab.d": "Scheme D",
        "tab.about": "About",
        "group.folder": "Folder Settings",
        "field.targetPath": "Target Folder Path",
        "placeholder.path": "e.g. D:\\Private\\MyFolder",
        "field.newName": "New Folder Name",
        "placeholder.newName": "Optional: rename before hiding",
        "btn.encrypt": "Hide",
        "btn.decrypt": "Restore",
        "group.acl": "ACL Permission Restriction",
        "group.about": "About",
        "about.title": "Folder Hide Tool 1.2.0",
        "about.a": "<b>Scheme A</b>: appends <code>..</code> to the folder name and sets the hidden attribute; double-clicking in Explorer reports \"Location is not available\".",
        "about.d": "<b>Scheme D</b>: backs up directory permissions and denies the current user list access; double-clicking fails to open, and restoring reads <code>.DS_Store</code> inside the folder.",
        "about.warn": "Note: this is not encryption. Administrators or privileged accounts can still change ACLs. Back up before use.",
        "about.note": "The metadata key is derived from a built-in string.",
        "group.log": "Log",
        "btn.clearLog": "Clear Log",
        "field.language": "Display language:",
        "hint.a": "Scheme A: enter the full path ending with .. to restore.",
        "hint.d": "Scheme D: the folder keeps its name but cannot be listed after hiding.",
        "hint.about": "About: this is not encryption; do not use on system folders.",
        "msg.needPath": "Please enter the target folder path",
        "msg.needDecryptPath": "Please enter the folder path to restore",
        "help.a": "[Tip] Scheme A: enter a path and an optional new name; hides by appending ..",
        "help.d": "[Tip] Scheme D: the folder name stays the same but the current user cannot list it; restoring brings permissions back.",
        "log.version": "[Info] Folder Hide Tool {version}",
        "tab.general": "General",
        "group.general": "General Settings",
        "chk.autoStart": "Start with Windows",
        "chk.runAsAdmin": "Run as administrator",
        "tip.runAsAdmin": "When enabled, the app requests administrator privileges at startup (Windows will show a confirmation prompt).",
        "hint.general": "General: configure display language and startup options.",
        "log.autoStartOn": "[Info] Start with Windows enabled",
        "log.autoStartOff": "[Info] Start with Windows disabled",
        "log.runAsAdminOn": "[Info] Run as administrator enabled",
        "log.runAsAdminOff": "[Info] Run as administrator disabled",
    },
    ja: {
        "app.title": "フォルダ非表示ツール",
        "tab.a": "方式A",
        "tab.d": "方式D",
        "tab.about": "情報",
        "group.folder": "フォルダ設定",
        "field.targetPath": "対象フォルダのパス",
        "placeholder.path": "例: D:\\Private\\マイフォルダ",
        "field.newName": "新しいフォルダ名",
        "placeholder.newName": "任意：非表示前に名前を変更",
        "btn.encrypt": "非表示にする",
        "btn.decrypt": "元に戻す",
        "group.acl": "ACLアクセス制限",
        "group.about": "情報",
        "about.title": "フォルダ非表示ツール 1.2.0",
        "about.a": "<b>方式A</b>：フォルダ名に <code>..</code> を付け、隠し属性を設定します。エクスプローラーでダブルクリックすると「場所は使用できません」と表示されます。",
        "about.d": "<b>方式D</b>：ディレクトリのアクセス権をバックアップし、現在のユーザーの一覧表示を拒否します。ダブルクリックで開けず、復元時はフォルダ内の <code>.DS_Store</code> から権限を戻します。",
        "about.warn": "注意：これは暗号化ではありません。管理者や権限を持つアカウントはACLを変更できます。操作前にバックアップしてください。",
        "about.note": "メタデータの鍵は組み込み文字列から派生しています。",
        "group.log": "ログ",
        "btn.clearLog": "ログをクリア",
        "field.language": "表示言語:",
        "hint.a": "方式A：復元するには .. 付きの完全パスを入力してください。",
        "hint.d": "方式D：非表示後もフォルダ名は変わりませんが、一覧表示できません。",
        "hint.about": "情報：これは暗号化ではありません。システムフォルダには使用しないでください。",
        "msg.needPath": "対象フォルダのパスを入力してください",
        "msg.needDecryptPath": "復元するフォルダのパスを入力してください",
        "help.a": "[ヒント] 方式A：パスと任意の新しい名前を入力し、.. を付けて非表示にします。",
        "help.d": "[ヒント] 方式D：非表示後もフォルダ名は変わらず、現在のユーザーは一覧表示できません。復元時に権限を戻します。",
        "log.version": "[情報] フォルダ非表示ツール {version}",
        "tab.general": "一般",
        "group.general": "一般設定",
        "chk.autoStart": "Windows起動時に開始",
        "chk.runAsAdmin": "管理者として実行",
        "tip.runAsAdmin": "有効にすると、起動時に管理者権限を要求します（Windows の確認ダイアログが表示されます）。",
        "hint.general": "一般：表示言語と起動オプションを設定します。",
        "log.autoStartOn": "[情報] Windows起動時に開始を有効にしました",
        "log.autoStartOff": "[情報] Windows起動時に開始を無効にしました",
        "log.runAsAdminOn": "[情報] 管理者として実行を有効にしました",
        "log.runAsAdminOff": "[情報] 管理者として実行を無効にしました",
    },
};

export const supportedLocales = Object.keys(dictionaries);

function normalizeLocale(value) {
    if (!value) return null;
    const lower = String(value).toLowerCase();
    if (lower.startsWith("zh")) return "zh-CN";
    if (lower.startsWith("ja")) return "ja";
    if (lower.startsWith("en")) return "en";
    return null;
}

function detectLocale() {
    let saved = null;
    try {
        saved = localStorage.getItem(STORAGE_KEY);
    } catch (_) {
        // WebView 环境下 localStorage 通常可用，失败则回退到系统语言。
    }
    return normalizeLocale(saved) || normalizeLocale(navigator.language) || "zh-CN";
}

let currentLocale = detectLocale();
const listeners = new Set();

export function getLocale() {
    return currentLocale;
}

export function t(key, vars) {
    const dict = dictionaries[currentLocale] || dictionaries["zh-CN"];
    let value = dict[key];
    if (value === undefined) value = dictionaries["zh-CN"][key];
    if (value === undefined) return key;
    if (vars) {
        for (const [name, replacement] of Object.entries(vars)) {
            value = value.replace(new RegExp("\\{" + name + "\\}", "g"), String(replacement));
        }
    }
    return value;
}

export function applyI18n(root = document) {
    root.querySelectorAll("[data-i18n]").forEach((node) => {
        node.textContent = t(node.dataset.i18n);
    });
    root.querySelectorAll("[data-i18n-html]").forEach((node) => {
        node.innerHTML = t(node.dataset.i18nHtml);
    });
    root.querySelectorAll("[data-i18n-placeholder]").forEach((node) => {
        node.placeholder = t(node.dataset.i18nPlaceholder);
    });
    root.querySelectorAll("[data-i18n-title]").forEach((node) => {
        node.title = t(node.dataset.i18nTitle);
    });
    document.title = t("app.title");
    if (document.documentElement) document.documentElement.lang = currentLocale;
}

export function setLocale(locale) {
    const normalized = normalizeLocale(locale);
    if (!normalized || normalized === currentLocale) return;
    currentLocale = normalized;
    try {
        localStorage.setItem(STORAGE_KEY, normalized);
    } catch (_) {
        // 持久化失败不影响本次会话的语言切换。
    }
    applyI18n();
    listeners.forEach((fn) => fn(normalized));
}

export function onLocaleChange(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
}

applyI18n();
