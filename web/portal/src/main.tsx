import { ThemeProvider, ToastProvider, TooltipProvider } from "@hubchat/shared";
import { lazy, StrictMode, Suspense, type ComponentType } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, Navigate, RouterProvider } from "react-router-dom";
import { PortalShell } from "./components/PortalShell";
import { RouteFallback } from "./components/RouteFallback";
import { PortalFeatureGate, type PortalFeature } from "./portal-context";
import { PortalProvider } from "./portal-context";
import Home from "./pages/Home";
import "./styles.css";

/**
 * Routing.
 *
 * Home is eager because it is the landing page for most visitors and the one
 * route that must paint immediately. Everything else is split: a customer
 * arriving from a search result to read one article should not download the
 * ticket composer, the feedback board, and the account screen alongside it.
 */
function page(loader: () => Promise<{ default: ComponentType }>) {
  const Component = lazy(loader);
  return (
    <Suspense fallback={<RouteFallback />}>
      <Component />
    </Suspense>
  );
}

function featurePage(feature: PortalFeature, loader: () => Promise<{ default: ComponentType }>) {
  return <PortalFeatureGate feature={feature}>{page(loader)}</PortalFeatureGate>;
}

const router = createBrowserRouter(
  [
    { path: "/sign-in", element: page(() => import("./pages/SignIn")) },
    {
      path: "/",
      element: <PortalShell />,
      children: [
        { index: true, element: <Home /> },
        { path: "kb", element: featurePage("knowledge_base", () => import("./pages/Knowledge")) },
        { path: "kb/:collectionSlug", element: featurePage("knowledge_base", () => import("./pages/Knowledge")) },
        { path: "kb/article/:slug", element: featurePage("knowledge_base", () => import("./pages/Article")) },
        { path: "tickets", element: featurePage("tickets", () => import("./pages/Tickets")) },
        { path: "tickets/new", element: featurePage("tickets", () => import("./pages/NewTicket")) },
        { path: "tickets/:number", element: featurePage("tickets", () => import("./pages/TicketDetail")) },
        { path: "feedback", element: featurePage("feedback", () => import("./pages/Feedback")) },
        { path: "feedback/:itemId", element: featurePage("feedback", () => import("./pages/FeedbackItem")) },
        { path: "changelog", element: featurePage("changelog", () => import("./pages/Changelog")) },
        { path: "survey/:workspaceID/:surveyID", element: page(() => import("./pages/Survey")) },
        { path: "account", element: page(() => import("./pages/Account")) },
        { path: "*", element: <Navigate to="/" replace /> },
      ],
    },
  ],
  { basename: "/portal" },
);

const container = document.getElementById("root");
if (!container) throw new Error("#root is missing from index.html");

createRoot(container).render(
  <StrictMode>
    <ThemeProvider storageKey="hubchat.portal" defaultMode="light">
      <TooltipProvider delayDuration={300}>
        <ToastProvider>
          <PortalProvider>
            <RouterProvider router={router} />
          </PortalProvider>
        </ToastProvider>
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>,
);
