import {
  Avatar,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuRadioGroup,
  MenuRadioItem,
  MenuSeparator,
  MenuSub,
  MenuTrigger,
  StatusDot,
  useTheme,
  type ThemeMode,
  type Density,
} from "@hubchat/shared";
import {
  BookOpen,
  CircleDot,
  Keyboard,
  LogOut,
  Monitor,
  Moon,
  Rows3,
  Sun,
  UserRound,
} from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../workspace-context";

export function AccountMenu() {
  const { viewer } = useWorkspace();
  const { mode, setMode, density, setDensity } = useTheme();
  const navigate = useNavigate();

  return (
    <Menu>
      <MenuTrigger asChild>
        <button
          type="button"
          aria-label={`Account: ${viewer.name}`}
          className="grid size-9 place-items-center rounded-md transition-colors hover:bg-fill"
        >
          <Avatar
            name={viewer.name}
            seed={viewer.id}
            size="sm"
            status={viewer.presence}
          />
        </button>
      </MenuTrigger>

      <MenuContent align="end" side="right" sideOffset={8} className="w-60">
        <div className="flex items-center gap-2.5 px-2 py-2">
          <Avatar name={viewer.name} seed={viewer.id} size="md" />
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-fg">{viewer.name}</p>
            <p className="truncate text-xs text-fg-muted">{viewer.email}</p>
          </div>
        </div>

        <MenuSeparator />

        {/* Availability drives round-robin and least-active routing (§6.12), so
            it belongs one click away rather than buried in preferences. */}
        <MenuSub
          label="Availability"
          icon={<StatusDot status={viewer.presence} className="ml-0.5" />}
        >
          <MenuRadioGroup value={viewer.presence}>
            <MenuRadioItem value="online">Online — accepting work</MenuRadioItem>
            <MenuRadioItem value="busy">Busy — no new assignments</MenuRadioItem>
            <MenuRadioItem value="away">Away</MenuRadioItem>
            <MenuRadioItem value="offline">Appear offline</MenuRadioItem>
          </MenuRadioGroup>
        </MenuSub>

        <MenuSub label="Appearance" icon={<CircleDot />}>
          <MenuLabel>Theme</MenuLabel>
          <MenuRadioGroup value={mode} onValueChange={(value) => setMode(value as ThemeMode)}>
            <MenuRadioItem value="dark">
              <Moon className="mr-2 size-3.5" /> Dark
            </MenuRadioItem>
            <MenuRadioItem value="light">
              <Sun className="mr-2 size-3.5" /> Light
            </MenuRadioItem>
            <MenuRadioItem value="system">
              <Monitor className="mr-2 size-3.5" /> Match system
            </MenuRadioItem>
          </MenuRadioGroup>

          <MenuSeparator />
          <MenuLabel>Density</MenuLabel>
          <MenuRadioGroup value={density} onValueChange={(value) => setDensity(value as Density)}>
            <MenuRadioItem value="comfortable">
              <Rows3 className="mr-2 size-3.5" /> Comfortable
            </MenuRadioItem>
            <MenuRadioItem value="compact">
              <Rows3 className="mr-2 size-3.5" /> Compact
            </MenuRadioItem>
          </MenuRadioGroup>
        </MenuSub>

        <MenuSeparator />

        <MenuItem icon={<UserRound />} onSelect={() => navigate("/account/profile")}>
          Profile & preferences
        </MenuItem>
        <MenuItem icon={<Keyboard />} shortcut="?">
          Keyboard shortcuts
        </MenuItem>
        <MenuItem icon={<BookOpen />}>Documentation</MenuItem>

        <MenuSeparator />
        <MenuItem icon={<LogOut />} destructive onSelect={() => navigate("/login")}>
          Sign out
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}
