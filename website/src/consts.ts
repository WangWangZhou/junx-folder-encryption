// 站点全局数据：所有页面通过 import 引用。

export const SITE_VERSION = "v1.1.0";
export const SITE_TITLE = `文件夹隐藏工具 ${SITE_VERSION}`;
export const SITE_DESCRIPTION =
	"文件夹隐藏工具：文件夹隐藏、文件与文件夹 AES-256-GCM 加密、批量任务与锁屏密码，Windows 10 / 11 桌面应用。";

export const REPO_URL = "https://gitee.com/wangwang2020/junx-folder-encryption";
export const DOWNLOAD_URL = `${REPO_URL}/releases`;

// 文档导航：分组与页面一一对应。
export const NAV_GROUPS = [
	{
		title: "指南",
		items: [
			{ title: "文件夹隐藏使用", href: "/guide/folder-hide/" },
			{ title: "文件加密使用", href: "/guide/file-encrypt/" },
			{ title: "文件夹加密使用", href: "/guide/folder-encrypt/" },
		],
	},
	{
		title: "进阶",
		items: [
			{ title: "批量加密", href: "/advanced/batch-encrypt/" },
			{ title: "锁屏密码", href: "/advanced/lock-screen/" },
			{ title: "配置管理", href: "/advanced/config-management/" },
			{ title: "日志管理", href: "/advanced/log-management/" },
		],
	},
	{
		title: "更多",
		items: [
			{ title: "官网", href: "/more/official/" },
			{ title: "下载", href: "/more/download/" },
			{ title: "帮助翻译", href: "/more/translate/" },
			{ title: "更新日志", href: "/more/changelog/" },
		],
	},
] as const;

export const SITE_NAV = [
	{ title: "指南", href: "/guide/folder-hide/" },
	{ title: "进阶", href: "/advanced/batch-encrypt/" },
	{ title: "更多", href: "/more/official/" },
] as const;
