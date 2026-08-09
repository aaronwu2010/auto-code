import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    // 强制 IPv4 loopback，避免 Wails 在 Windows 上通过 [::1] 代理时
    // 出现 "wsarecv: An existing connection was forcibly closed" 错误。
    // host:true 会让 vite 输出 http://localhost:5173/，Wails auto 解析
    // localhost 时 Windows 优先解析为 IPv6 [::1]，代理连接被 vite 重置。
    // 用 127.0.0.1 让 vite 输出 http://127.0.0.1:5173/，Wails 直接走 IPv4。
    host: "127.0.0.1",
    strictPort: true,
    port: 5173,
    // Wails 的 ExternalAssetHandler 用 httputil.NewSingleHostReverseProxy 代理，
    // 该代理不能正确转发 vite 的 HMR WebSocket 升级，在 Windows 上会触发
    // "wsarecv: An existing connection was forcibly closed"。
    // 桌面应用下禁用 HMR，改前端代码后刷新 webview 即可看到更新。
    hmr: false,
  },
});
