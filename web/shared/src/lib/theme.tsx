import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from "react";
import { useLocalStorage } from "./hooks";

export type ThemeMode = "dark" | "light" | "system";
export type ResolvedTheme = "dark" | "light";
export type Density = "comfortable" | "compact";

type ThemeContextValue = {
  /** What the user chose, including "system". */
  mode: ThemeMode;
  /** What is actually painted right now. */
  theme: ResolvedTheme;
  density: Density;
  setMode: (mode: ThemeMode) => void;
  setDensity: (density: Density) => void;
  toggleTheme: () => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export type ThemeProviderProps = {
  children: ReactNode;
  /** Namespaced so the dashboard, portal, and widget do not fight over one key. */
  storageKey?: string;
  defaultMode?: ThemeMode;
  defaultDensity?: Density;
  /**
   * Where the `data-theme` attribute lands. Defaults to <html>. The widget
   * passes its own shadow-root host so it never restyles the customer's page.
   */
  target?: () => HTMLElement | null;
};

/**
 * Applies theme and density as attributes rather than classes, because
 * theme.css keys off `[data-theme]` / `[data-density]` and those nest — a light
 * widget preview can sit inside a dark dashboard without any override CSS.
 */
export function ThemeProvider({
  children,
  storageKey = "hubchat.appearance",
  defaultMode = "dark",
  defaultDensity = "comfortable",
  target,
}: ThemeProviderProps) {
  const [mode, setMode] = useLocalStorage<ThemeMode>(`${storageKey}.mode`, defaultMode);
  const [density, setDensity] = useLocalStorage<Density>(`${storageKey}.density`, defaultDensity);

  const theme = useSystemResolvedTheme(mode);

  useEffect(() => {
    const element = target?.() ?? document.documentElement;
    if (!element) return;
    element.setAttribute("data-theme", theme);
    element.setAttribute("data-density", density);
  }, [theme, density, target]);

  const toggleTheme = useCallback(() => {
    setMode(theme === "dark" ? "light" : "dark");
  }, [theme, setMode]);

  const value = useMemo<ThemeContextValue>(
    () => ({ mode, theme, density, setMode, setDensity, toggleTheme }),
    [mode, theme, density, setMode, setDensity, toggleTheme],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("useTheme must be used within a <ThemeProvider>");
  return context;
}

function useSystemResolvedTheme(mode: ThemeMode): ResolvedTheme {
  const [systemTheme, setSystemTheme] = useLocalStorage<ResolvedTheme>(
    "hubchat.system-theme",
    "dark",
  );

  useEffect(() => {
    if (mode !== "system") return;
    const query = window.matchMedia("(prefers-color-scheme: light)");
    const apply = () => setSystemTheme(query.matches ? "light" : "dark");
    apply();
    query.addEventListener("change", apply);
    return () => query.removeEventListener("change", apply);
  }, [mode, setSystemTheme]);

  return mode === "system" ? systemTheme : mode;
}

/**
 * Inline script for the document <head>. Sets the theme attribute before first
 * paint so a dark-mode agent never gets a white flash on reload.
 */
export const themeBootstrapScript = (storageKey = "hubchat.appearance") => `
(function(){
  try {
    var mode = JSON.parse(localStorage.getItem(${JSON.stringify(`${storageKey}.mode`)}) || '"dark"');
    var density = JSON.parse(localStorage.getItem(${JSON.stringify(`${storageKey}.density`)}) || '"comfortable"');
    if (mode === 'system') {
      mode = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    }
    document.documentElement.setAttribute('data-theme', mode);
    document.documentElement.setAttribute('data-density', density);
  } catch (e) {
    document.documentElement.setAttribute('data-theme', 'dark');
  }
})();
`;
