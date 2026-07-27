import { ThemeProvider, ToastProvider, TooltipProvider } from "@hubchat/shared";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { router } from "./app/router";
import { WorkspaceProvider } from "./app/workspace-context";
import "./styles.css";

const container = document.getElementById("root");
if (!container) throw new Error("#root is missing from index.html");

createRoot(container).render(
  <StrictMode>
    <ThemeProvider storageKey="hubchat.appearance">
      <TooltipProvider delayDuration={300} skipDelayDuration={200}>
        <ToastProvider>
          <WorkspaceProvider>
            <RouterProvider router={router} />
          </WorkspaceProvider>
        </ToastProvider>
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>,
);
