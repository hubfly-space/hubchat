import { lazy, Suspense, type ComponentType } from "react";
import { createBrowserRouter, Navigate, Outlet, type RouteObject } from "react-router-dom";
import { AppShell } from "./shell/AppShell";
import { WorkspaceProvider } from "./workspace-context";
import { AuthLayout } from "../pages/auth/AuthLayout";
import { RouteFallback } from "./RouteFallback";
import { NotFound } from "../pages/NotFound";

/**
 * Route tree.
 *
 * Structure follows the module boundaries in §8.4 rather than the visual
 * navigation, so a route path predicts which Go module owns its data:
 * /inbox → conversation, /channels/widgets → widget, /developers/* →
 * webhook + integrations.
 *
 * Every page is code-split. The inbox is the only route most agents ever load,
 * and it should not pay for the settings tree, the report charts, or the
 * builders it will never open.
 */

function page(loader: () => Promise<{ default: ComponentType }>) {
  const Component = lazy(loader);
  return (
    <Suspense fallback={<RouteFallback />}>
      <Component />
    </Suspense>
  );
}

const appRoutes: RouteObject[] = [
  { index: true, element: <Navigate to="/overview" replace /> },

  // ---------------------------------------------------------------- overview
  { path: "overview", element: page(() => import("../pages/Overview")) },
  { path: "workspaces/new", element: page(() => import("../pages/setup/CreateWorkspace")) },

  // ------------------------------------------------------------------- inbox
  {
    path: "inbox",
    element: page(() => import("../pages/inbox/InboxPage")),
    children: [
      { index: true, element: null },
      { path: ":viewId", element: null },
      { path: ":viewId/:conversationId", element: null },
    ],
  },

  // ------------------------------------------------------------------- tasks
  { path: "tasks", element: page(() => import("../pages/tasks/TaskList")) },

  // ----------------------------------------------------------------- tickets
  { path: "tickets", element: page(() => import("../pages/tickets/TicketList")) },
  { path: "tickets/:ticketId", element: page(() => import("../pages/tickets/TicketDetail")) },

  // --------------------------------------------------------------- customers
  { path: "customers", element: page(() => import("../pages/customers/CustomerList")) },
  { path: "customers/:customerId", element: page(() => import("../pages/customers/CustomerDetail")) },
  { path: "companies", element: page(() => import("../pages/customers/CompanyList")) },
  { path: "companies/:companyId", element: page(() => import("../pages/customers/CompanyDetail")) },

  // ---------------------------------------------------------------- feedback
  { path: "feedback", element: page(() => import("../pages/feedback/BoardList")) },
  { path: "feedback/roadmap", element: page(() => import("../pages/feedback/Roadmap")) },
  { path: "feedback/boards/:boardId", element: page(() => import("../pages/feedback/BoardDetail")) },
  { path: "feedback/items/:itemId", element: page(() => import("../pages/feedback/ItemDetail")) },

  // ---------------------------------------------------------- knowledge base
  { path: "kb", element: page(() => import("../pages/kb/ArticleList")) },
  { path: "kb/collections", element: page(() => import("../pages/kb/CollectionList")) },
  { path: "kb/articles/:articleId", element: page(() => import("../pages/kb/ArticleEditor")) },
  { path: "kb/search-analytics", element: page(() => import("../pages/kb/SearchAnalytics")) },
  { path: "kb/changelog", element: page(() => import("../pages/content/ChangelogList")) },

  // ----------------------------------------------------------------- surveys
  { path: "surveys", element: page(() => import("../pages/surveys/SurveyList")) },
  { path: "surveys/:surveyId", element: page(() => import("../pages/surveys/SurveyDetail")) },

  // ------------------------------------------------------------------- forms
  { path: "forms", element: page(() => import("../pages/forms/FormList")) },
  { path: "forms/:formId", element: page(() => import("../pages/forms/FormBuilder")) },

  // ----------------------------------------------------------------- reports
  { path: "reports", element: page(() => import("../pages/reports/ReportsOverview")) },
  { path: "reports/support", element: page(() => import("../pages/reports/SupportReport")) },
  { path: "reports/experience", element: page(() => import("../pages/reports/ExperienceReport")) },
  { path: "reports/surfaces", element: page(() => import("../pages/reports/SurfacesReport")) },

  // ---------------------------------------------------------------- channels
  { path: "channels/widgets", element: page(() => import("../pages/channels/WidgetList")) },
  { path: "channels/widgets/:widgetId", element: page(() => import("../pages/channels/WidgetBuilder")) },
  { path: "channels/portals", element: page(() => import("../pages/channels/PortalList")) },
  { path: "channels/portals/:portalId", element: page(() => import("../pages/channels/PortalBuilder")) },
  { path: "channels/inboxes", element: page(() => import("../pages/channels/InboxSettings")) },
  { path: "channels/email", element: page(() => import("../pages/channels/EmailChannel")) },

  // -------------------------------------------------------------- automation
  { path: "automation/rules", element: page(() => import("../pages/automation/RuleList")) },
  { path: "automation/rules/:ruleId", element: page(() => import("../pages/automation/RuleBuilder")) },
  { path: "automation/macros", element: page(() => import("../pages/automation/MacroList")) },
  { path: "automation/replies", element: page(() => import("../pages/automation/SavedReplyList")) },
  { path: "automation/executions", element: page(() => import("../pages/automation/ExecutionLog")) },

  // --------------------------------------------------------------------- SLA
  { path: "sla/policies", element: page(() => import("../pages/sla/PolicyList")) },
  { path: "sla/policies/:policyId", element: page(() => import("../pages/sla/PolicyEditor")) },
  { path: "sla/calendars", element: page(() => import("../pages/sla/CalendarList")) },

  // -------------------------------------------------------------- developers
  { path: "developers/keys", element: page(() => import("../pages/developers/ApiKeys")) },
  { path: "developers/webhooks", element: page(() => import("../pages/developers/WebhookList")) },
  { path: "developers/webhooks/:endpointId", element: page(() => import("../pages/developers/WebhookDetail")) },
  { path: "developers/events", element: page(() => import("../pages/developers/EventStream")) },
  { path: "developers/metadata", element: page(() => import("../pages/developers/MetadataSchema")) },
  { path: "developers/sdk", element: page(() => import("../pages/developers/SdkGuide")) },
  { path: "developers/commands", element: page(() => import("../pages/developers/CustomerCommands")) },

  // ---------------------------------------------------------------- settings
  { path: "settings", element: <Navigate to="/settings/general" replace /> },
  { path: "settings/general", element: page(() => import("../pages/settings/General")) },
  { path: "settings/branding", element: page(() => import("../pages/settings/Branding")) },
  { path: "settings/members", element: page(() => import("../pages/settings/Members")) },
  { path: "settings/teams", element: page(() => import("../pages/settings/Teams")) },
  { path: "settings/roles", element: page(() => import("../pages/settings/Roles")) },
  { path: "settings/tags", element: page(() => import("../pages/settings/Tags")) },
  { path: "settings/fields", element: page(() => import("../pages/settings/TicketFields")) },
  { path: "settings/hours", element: page(() => import("../pages/settings/BusinessHours")) },
  { path: "settings/notifications", element: page(() => import("../pages/settings/Notifications")) },
  { path: "settings/security", element: page(() => import("../pages/settings/Security")) },
  { path: "settings/privacy", element: page(() => import("../pages/settings/Privacy")) },
  { path: "settings/data", element: page(() => import("../pages/settings/ImportExport")) },
  { path: "settings/audit", element: page(() => import("../pages/settings/AuditLog")) },
  { path: "settings/jobs", element: page(() => import("../pages/settings/Jobs")) },
  { path: "settings/limits", element: page(() => import("../pages/settings/Limits")) },

  // ----------------------------------------------------------------- account
  { path: "account", element: <Navigate to="/account/profile" replace /> },
  { path: "account/profile", element: page(() => import("../pages/account/Profile")) },
  { path: "account/preferences", element: page(() => import("../pages/account/Preferences")) },
  { path: "account/sessions", element: page(() => import("../pages/account/Sessions")) },

  { path: "*", element: <NotFound /> },
];

export const router = createBrowserRouter(
  [
    // Unauthenticated surfaces sit outside the shell entirely — no nav rail,
    // no workspace context requirement.
    {
      element: <AuthLayout />,
      children: [
        { path: "/login", element: page(() => import("../pages/auth/Login")) },
        { path: "/signup", element: page(() => import("../pages/auth/Signup")) },
        { path: "/forgot-password", element: page(() => import("../pages/auth/ForgotPassword")) },
        { path: "/magic-link", element: page(() => import("../pages/auth/MagicLink")) },
        { path: "/reset-password", element: page(() => import("../pages/auth/ResetPassword")) },
        { path: "/verify", element: page(() => import("../pages/auth/VerifyEmail")) },
        { path: "/two-factor", element: page(() => import("../pages/auth/TwoFactor")) },
        { path: "/invite/:token", element: page(() => import("../pages/auth/AcceptInvite")) },
      ],
    },

    // First-run installation flow (§7.1). Full-screen, its own chrome.
    { path: "/setup", element: page(() => import("../pages/setup/SetupWizard")) },
    // Diagnostic only — talks to the real API, not fixtures. See its own
    // header comment for what that means and why it lives outside AppShell.
    { path: "/dev/live", element: page(() => import("../pages/dev/LiveDemo")) },
    // Post-setup workspace onboarding (§7.2).
    { path: "/onboarding", element: page(() => import("../pages/setup/Onboarding")) },

    // Everything below needs a workspace, so the provider wraps this branch
    // rather than the whole tree. Two reasons it belongs here and not around
    // the RouterProvider:
    //
    //   · /login must not require a workspace. Bootstrapping one before the
    //     user has signed in guarantees a 401 on the page whose entire job is
    //     to fix that.
    //   · the provider redirects (to /login on 401, /onboarding on 403), and
    //     <Navigate> needs router context to do it.
    {
      element: <WorkspaceRoute />,
      children: [{ path: "/", element: <AppShell />, children: appRoutes }],
    },
  ],
  { basename: "/app" },
);

/**
 * Pathless layout route that supplies workspace context to everything nested
 * under it. Renders an Outlet rather than AppShell directly, so the shell
 * stays exactly one route deep and its own children resolve normally.
 */
function WorkspaceRoute() {
  return (
    <WorkspaceProvider>
      <Outlet />
    </WorkspaceProvider>
  );
}
