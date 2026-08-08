import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    // 监听所有地址（IPv4 + IPv6），解决 Wails 通过 [::1] 代理连接被强制关闭的问题
    host: true,
    strictPort: true,
    port: 5173,
  },
});
