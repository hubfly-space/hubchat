import {
  Avatar,
  Button,
  Menu,
  MenuContent,
  MenuItem,
  MenuRadioGroup,
  MenuRadioItem,
  MenuSeparator,
  MenuTrigger,
  api,
  useMutation,
  cn,
  useTheme,
  type ThemeMode,
} from "@hubchat/shared";
import { LogOut, Monitor, Moon, Sun, TicketCheck, UserRound } from "lucide-react";
import { useEffect } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import {
  portalAccent,
  portalAssetURL,
  portalDirection,
  portalErrorMessage,
  portalFeatureEnabled,
  portalLanguage,
  portalNavigationItems,
  usePortal,
} from "../portal-context";
import { portalText } from "../i18n";

/**
 * Portal chrome.
 *
 * Branding lives entirely in CSS variables set on the root element, so a
 * workspace's accent re-tones the whole surface without a rebuild — the same
 * `[data-branded]` mechanism the widget uses.
 */
export function PortalShell() {
  const { data, isLoading, error, refetch } = usePortal();
  const logout = useMutation(() => api.post("/portal/auth/logout"));
  const { pathname } = useLocation();
  const { mode, setMode } = useTheme();
  const onHome = pathname === "/";
  const language = portalLanguage(data);
  const t = (key: string, fallback: string, values?: Record<string, string | number>) => portalText(data, key, fallback, values);

  useEffect(() => {
    const root = document.documentElement;
    const previousLanguage = root.getAttribute("lang");
    const previousDirection = root.getAttribute("dir");
    root.lang = language;
    root.dir = portalDirection(language);
    return () => {
      if (previousLanguage === null) root.removeAttribute("lang");
      else root.lang = previousLanguage;
      if (previousDirection === null) root.removeAttribute("dir");
      else root.dir = previousDirection;
    };
  }, [language]);

  if (isLoading) return <div className="grid min-h-dvh place-items-center bg-canvas text-sm text-fg-muted">{t("loading_portal", "Loading portal…")}</div>;
  if (error || !data) {
    return <div className="grid min-h-dvh place-items-center bg-canvas px-4 text-center text-sm text-fg-muted"><div><p>{portalErrorMessage(error)}</p><Button className="mt-4" variant="secondary" size="sm" onClick={refetch}>{t("try_again", "Try again")}</Button></div></div>;
  }

  const { portal, viewer } = data;
  const accent = portalAccent(portal);
  const portalMark = portal.name.trim().charAt(0).toUpperCase() || "H";
  const logoURL = portalAssetURL(portal.theme.logo_url);
  const navigation = portalNavigationItems(portal);
  const currentYear = new Date().getFullYear();

  return (
    <div
      data-branded
      lang={language}
      dir={portalDirection(language)}
      style={{ ["--hc-accent-brand" as string]: accent }}
      className="flex min-h-dvh flex-col bg-canvas text-fg"
    >
      <a
        href="#content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:bg-accent focus:px-3 focus:py-2 focus:text-sm focus:text-accent-fg"
      >
        {t("skip_to_content", "Skip to content")}
      </a>

      <header className="sticky top-0 z-[var(--z-nav)] border-b border-line bg-surface/85 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-6 px-4 sm:px-6">
          <NavLink to="/" className="flex shrink-0 items-center gap-2">
            {logoURL ? <img src={logoURL} alt="" className="size-6 rounded-md object-contain" /> : <span aria-hidden="true" className="grid size-6 place-items-center rounded-md text-[11px] font-bold text-white" style={{ backgroundColor: accent }}>{portalMark}</span>}
            <span className="text-sm font-semibold tracking-tight">{portal.name}</span>
          </NavLink>

          <nav aria-label={t("portal_navigation", "Portal")} className="hc-no-scrollbar hidden flex-1 gap-1 overflow-x-auto sm:flex">
            {navigation.map((item) => {
              const external = item.external || /^https?:\/\//i.test(item.href);
              const className = "rounded-md px-2.5 py-1.5 text-sm text-fg-secondary transition-colors hover:bg-fill hover:text-fg";
              if (external) {
                const href = portalAssetURL(item.href);
                return href ? <a key={`${item.href}-${item.label}`} href={href} target="_blank" rel="noopener noreferrer" className={className}>{item.label}</a> : null;
              }
              return <NavLink key={`${item.href}-${item.label}`} to={item.href} className={({ isActive }) => cn(className, isActive && "bg-accent-subtle font-medium text-accent-text")}>{item.label}</NavLink>;
            })}
          </nav>

          <div className="ml-auto flex shrink-0 items-center gap-2">
            <Menu>
              <MenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  aria-label={t("appearance", "Appearance")}
                  leading={mode === "dark" ? <Moon /> : mode === "light" ? <Sun /> : <Monitor />}
                />
              </MenuTrigger>
              <MenuContent align="end">
                <MenuRadioGroup value={mode} onValueChange={(value) => setMode(value as ThemeMode)}>
                  <MenuRadioItem value="light">{t("light", "Light")}</MenuRadioItem>
                  <MenuRadioItem value="dark">{t("dark", "Dark")}</MenuRadioItem>
                  <MenuRadioItem value="system">{t("match_system", "Match system")}</MenuRadioItem>
                </MenuRadioGroup>
              </MenuContent>
            </Menu>

            {viewer ? (
              <Menu>
                <MenuTrigger asChild>
                  <button
                    type="button"
                    aria-label={`Account: ${viewer.name}`}
                    className="rounded-full transition-transform hover:scale-105"
                  >
                    <Avatar name={viewer.name} seed={viewer.email} size="sm" />
                  </button>
                </MenuTrigger>
                <MenuContent align="end" className="w-56">
                  <div className="px-2 py-2">
                    <p className="truncate text-sm font-medium text-fg">{viewer.name}</p>
                    <p className="truncate text-xs text-fg-muted">{viewer.email}</p>
                  </div>
                  <MenuSeparator />
                  {portalFeatureEnabled(portal, "tickets") && <MenuItem icon={<TicketCheck />}><NavLink to="/tickets">{t("my_requests", "My requests")}</NavLink></MenuItem>}
                  <MenuItem icon={<UserRound />}>
                    <NavLink to="/account">{t("profile_preferences", "Profile & preferences")}</NavLink>
                  </MenuItem>
                  <MenuSeparator />
                  <MenuItem icon={<LogOut />} destructive onSelect={() => { void logout.mutate(undefined); window.location.assign("/portal/sign-in"); }}>
                    {t("sign_out", "Sign out")}
                  </MenuItem>
                </MenuContent>
              </Menu>
            ) : (
              <Button variant="secondary" size="sm" asChild>
                <Link to={`/sign-in?portal=${encodeURIComponent(portal.id)}&next=${encodeURIComponent(pathname)}`}>{t("sign_in", "Sign in")}</Link>
              </Button>
            )}
          </div>
        </div>
      </header>

      <main id="content" className={cn("flex-1", !onHome && "mx-auto w-full max-w-5xl px-4 py-8 sm:px-6")}>
        <Outlet />
      </main>

      <footer className="border-t border-line bg-surface">
        <div className="mx-auto flex max-w-5xl flex-col gap-3 px-4 py-6 text-xs text-fg-muted sm:flex-row sm:items-center sm:px-6">
          <p>© {currentYear} {portal.name}</p>
          <nav aria-label={t("footer", "Footer")} className="flex gap-4 sm:ml-auto">
            {Array.isArray(portal.theme.footer_links) &&
              portal.theme.footer_links.map((link) => {
                if (!link || typeof link !== "object") return null;
                const item = link as { url?: unknown; href?: unknown; label?: unknown };
                const href = typeof item.url === "string" ? item.url : item.href;
                if (typeof href !== "string" || typeof item.label !== "string") return null;
                const safeHref = portalAssetURL(href);
                if (!safeHref) return null;
                return <a key={`${safeHref}-${item.label}`} href={safeHref} target="_blank" rel="noopener noreferrer" className="transition-colors hover:text-fg">{item.label}</a>;
              })}
          </nav>
        </div>
      </footer>
    </div>
  );
}
