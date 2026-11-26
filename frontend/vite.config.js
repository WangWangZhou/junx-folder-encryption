import { defineConfig } from "vite";
import wails from "@wailsio/runtime/plugins/vite";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const rootDir = dirname(fileURLToPath(import.meta.url));

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [wails("./bindings")],
  build: {
    rollupOptions: {
      // 主窗口与批量窗口各自使用独立 HTML 入口。
      input: {
        main: resolve(rootDir, "index.html"),
        batch: resolve(rootDir, "batch.html"),
      },
    },
  },
});
