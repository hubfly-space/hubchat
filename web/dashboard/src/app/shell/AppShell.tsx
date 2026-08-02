import { api, useDashboardPreferences, useHotkey, useQuery, type Paginated, type Conversation } from "@hubchat/shared";
import { useEffect, useRef, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { useWorkspace } from "../workspace-context";
import { InboxSidebar } from "../../pages/inbox/InboxSidebar";
import { GlobalSearch } from "./GlobalSearch";
import { NavRail } from "./NavRail";
import { SectionSidebar } from "./SectionSidebar";
import { TopBar } from "./TopBar";
import { sectionForPath } from "./nav-config";

/**
 * Application chrome.
 *
 * Geometry, left to right:
 *   rail (56)  ·  section sidebar (232, optional)  ·  page
 * with a 48px top bar spanning everything right of the rail.
 *
 * The shell owns exactly three things: navigation, global search, and the
 * app-wide hotkeys. It never knows what a page contains.
 */
export function AppShell() {
  const { pathname } = useLocation();
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { isLive } = useWorkspace();
  const { preferences } = useDashboardPreferences();
  const conversations = useQuery<Paginated<Conversation>>(["shell", "conversations"], (signal) => api.get("/conversations?limit=50", { signal }));

  useEffect(() => {
    document.documentElement.toggleAttribute("data-reduce-motion", preferences.reduceMotion);
    return () => document.documentElement.removeAttribute("data-reduce-motion");
  }, [preferences.reduceMotion]);

  const section = sectionForPath(pathname);
  const unreadCount = conversations.data?.data.filter((conversation) => conversation.unread).length ?? 0;

  useHotkey("mod+k", () => setSearchOpen(true), { allowInInput: true });
  useHotkey("/", () => setSearchOpen(true));
  useGoToChords();

  return (
    <div className="flex h-dvh w-full gap-[var(--hc-shell-gap)] overflow-hidden bg-canvas p-[var(--hc-shell-pad)] text-fg transition-[gap,padding] duration-base ease-out">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-3 focus:top-3 focus:z-[var(--z-toast)] focus:rounded-md focus:bg-accent focus:px-3 focus:py-2 focus:text-sm focus:text-accent-fg"
      >
        Skip to content
      </a>

      <NavRail unreadCount={unreadCount} />

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-[var(--hc-float-radius)] border border-transparent bg-canvas shadow-[var(--hc-float-shadow)] transition-[border-color,border-radius,box-shadow] duration-base ease-out compact:border-line">
        <TopBar onOpenSearch={() => setSearchOpen(true)} connection={isLive ? "connected" : "reconnecting"} />

        <div className="flex min-h-0 flex-1 gap-[var(--hc-shell-gap)] transition-[gap] duration-base ease-out">
          {section?.id === "inbox" ? (
            <InboxSidebar />
          ) : section ? (
            <SectionSidebar section={section} />
          ) : null}

          <main id="main" className="min-w-0 flex-1 overflow-hidden">
            <Outlet />
          </main>
        </div>
      </div>

      <GlobalSearch
        open={searchOpen}
        onOpenChange={setSearchOpen}
        query={query}
        onQueryChange={setQuery}
      />
    </div>
  );
}

/**
 * `g`-prefixed jumps — the convention every keyboard-driven tool shares.
 * `g i` inbox, `g t` tickets, `g c` customers, `g r` reports.
 *
 * The chord's first keystroke is held in a ref rather than state: recording it
 * must not re-render the entire shell on every stray `g`.
 */
const GO_TO: Record<string, string> = {
  i: "/inbox",
  t: "/tickets",
  c: "/customers",
  r: "/reports",
};

const CHORD_TIMEOUT_MS = 800;

function useGoToChords() {
  const navigate = useNavigate();
  const armedAt = useRef(0);

  useHotkey("g", () => {
    armedAt.current = Date.now();
  });

  // One listener for every destination, rather than one hook per key, so the
  // hook count stays fixed regardless of how the map grows.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const destination = GO_TO[event.key.toLowerCase()];
      if (!destination) return;
      if (Date.now() - armedAt.current > CHORD_TIMEOUT_MS) return;
      if (event.metaKey || event.ctrlKey || event.altKey) return;

      const target = event.target;
      if (
        target instanceof HTMLElement &&
        (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)
      ) {
        return;
      }

      event.preventDefault();
      armedAt.current = 0;
      navigate(destination);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [navigate]);
}
