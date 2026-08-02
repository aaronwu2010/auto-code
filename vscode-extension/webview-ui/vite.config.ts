import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// VS Code Webview 限制：所有资源必须通过 vscode-webview:// 协议加载，
// 因此 base 设为相对路径 './'，让 vite 产出可被 Webview 加载的资源 URL。
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // 固定文件名，避免 hash，方便 webviewPanel.ts 引用
        entryFileNames: "assets/index.js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
});
