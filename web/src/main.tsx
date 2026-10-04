import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { setNonce } from "get-nonce";
import { TooltipProvider } from "@/components/ui/tooltip";
import { App } from "./App";
import "./index.css";

// Radix injects a <style> element for scroll locking; the page's
// Content-Security-Policy admits it only with this request's nonce.
const nonce = document.querySelector<HTMLMetaElement>('meta[name="taskroll-nonce"]')?.content;
if (nonce && !nonce.startsWith("{{")) setNonce(nonce);

const dark = window.matchMedia("(prefers-color-scheme: dark)");
const applyTheme = () => document.documentElement.classList.toggle("dark", dark.matches);
applyTheme();
dark.addEventListener("change", applyTheme);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <TooltipProvider>
      <App />
    </TooltipProvider>
  </StrictMode>,
);
