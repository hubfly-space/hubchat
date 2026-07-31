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
import { NavLink, Outlet, useLocation } from "react-router-dom";
import { portalAccent, usePortal } from "../portal-context";
import { portalErrorMessage } from "../portal-context";

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

  if (isLoading) return <div className="grid min-h-dvh place-items-center bg-canvas text-sm text-fg-muted">Loading portal…</div>;
  if (error || !data) {
    return <div className="grid min-h-dvh place-items-center bg-canvas px-4 text-center text-sm text-fg-muted"><div><p>{portalErrorMessage(error)}</p><Button className="mt-4" variant="secondary" size="sm" onClick={refetch}>Try again</Button></div></div>;
  }

  const { portal, viewer } = data;
  const accent = portalAccent(portal);

  return (
    <div
      data-branded
      style={{ ["--hc-accent-brand" as string]: accent }}
      className="flex min-h-dvh flex-col bg-canvas text-fg"
    >
      <a
        href="#content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:bg-accent focus:px-3 focus:py-2 focus:text-sm focus:text-accent-fg"
      >
        Skip to content
      </a>

      <header className="sticky top-0 z-[var(--z-nav)] border-b border-line bg-surface/85 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-6 px-4 sm:px-6">
          <NavLink to="/" className="flex shrink-0 items-center gap-2">
            <span
              aria-hidden="true"
              className="grid size-6 place-items-center rounded-md text-[11px] font-bold text-white"
                style={{ backgroundColor: accent }}
            >
              N
            </span>
            <span className="text-sm font-semibold tracking-tight">{portal.name}</span>
          </NavLink>

          <nav aria-label="Portal" className="hc-no-scrollbar hidden flex-1 gap-1 overflow-x-auto sm:flex">
            {portal.navigation.map((item) => (
              <NavLink
                key={item.href}
                to={item.href}
                className={({ isActive }) =>
                  cn(
                    "rounded-md px-2.5 py-1.5 text-sm transition-colors",
                    isActive
                      ? "bg-accent-subtle font-medium text-accent-text"
                      : "text-fg-secondary hover:bg-fill hover:text-fg",
                  )
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="ml-auto flex shrink-0 items-center gap-2">
            <Menu>
              <MenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  aria-label="Appearance"
                  leading={mode === "dark" ? <Moon /> : mode === "light" ? <Sun /> : <Monitor />}
                />
              </MenuTrigger>
              <MenuContent align="end">
                <MenuRadioGroup value={mode} onValueChange={(value) => setMode(value as ThemeMode)}>
                  <MenuRadioItem value="light">Light</MenuRadioItem>
                  <MenuRadioItem value="dark">Dark</MenuRadioItem>
                  <MenuRadioItem value="system">Match system</MenuRadioItem>
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
                  <MenuItem icon={<TicketCheck />}>
                    <NavLink to="/tickets">My requests</NavLink>
                  </MenuItem>
                  <MenuItem icon={<UserRound />}>
                    <NavLink to="/account">Profile & preferences</NavLink>
                  </MenuItem>
                  <MenuSeparator />
                  <MenuItem icon={<LogOut />} destructive onSelect={() => { void logout.mutate(undefined); window.location.assign("/portal/sign-in"); }}>
                    Sign out
                  </MenuItem>
                </MenuContent>
              </Menu>
            ) : (
              <Button variant="secondary" size="sm">
                Sign in
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
          <p>© 2026 Northwind Cloud</p>
          <nav aria-label="Footer" className="flex gap-4 sm:ml-auto">
            {Array.isArray(portal.features.footer_links) &&
              portal.features.footer_links.map((link) => {
                if (!link || typeof link !== "object") return null;
                const item = link as { href?: unknown; label?: unknown };
                if (typeof item.href !== "string" || typeof item.label !== "string") return null;
                return <a key={item.href} href={item.href} className="transition-colors hover:text-fg">{item.label}</a>;
              })}
          </nav>
        </div>
      </footer>
    </div>
  );
}
