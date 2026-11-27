import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "SUDOKU_");
  return {
    plugins: [react()],
    build: {
      outDir: "../internal/webui/dist",
      emptyOutDir: true,
    },
    server: {
      port: 5173,
      proxy: {
        "/api": {
          target: env.SUDOKU_API ?? "http://127.0.0.1:7777",
          changeOrigin: true,
          ws: true,
        },
      },
    },
  };
});
