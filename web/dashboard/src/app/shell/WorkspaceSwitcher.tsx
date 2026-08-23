import {
  Avatar,
  Menu,
  MenuContent,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  MenuTrigger,
  Tooltip,
} from "@hubchat/shared";
import { Check, Plus, Settings2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useWorkspace } from "../workspace-context";

/**
 * §6.1 — a user may belong to several workspaces and switch without signing
 * out. The switcher lives at the very top of the rail because getting the
 * wrong tenant is the most expensive navigation mistake available here.
 */
export function WorkspaceSwitcher() {
  const { workspace, workspaces, switchWorkspace } = useWorkspace();
  const navigate = useNavigate();

  return (
    <Menu>
      <Tooltip content={workspace.name} side="right">
        <MenuTrigger asChild>
          <button
            type="button"
            aria-label={`Workspace: ${workspace.name}. Switch workspace`}
            className="grid size-9 place-items-center rounded-lg transition-transform hover:scale-105 active:scale-95"
          >
            <Avatar
              name={workspace.name}
              seed={workspace.id}
              shape="square"
              size="md"
              kind="company"
            />
          </button>
        </MenuTrigger>
      </Tooltip>

      <MenuContent align="start" side="right" sideOffset={8} className="w-64">
        <MenuLabel>Workspaces</MenuLabel>
        {workspaces.map((item) => (
          <MenuItem
            key={item.id}
            onSelect={() => switchWorkspace(item.id)}
            icon={
              <Avatar name={item.name} seed={item.id} shape="square" size="xs" kind="company" />
            }
            description={`${item.slug}.hubchat.app`}
          >
            <span className="flex items-center gap-2">
              {item.name}
              {item.id === workspace.id && (
                <Check aria-hidden="true" className="size-3 text-accent-text" />
              )}
            </span>
          </MenuItem>
        ))}

        <MenuSeparator />
        <MenuItem icon={<Settings2 />} onSelect={() => navigate("/settings/general")}>
          Workspace settings
        </MenuItem>
        <MenuItem icon={<Plus />} onSelect={() => navigate("/workspaces/new")}>
          Create workspace
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}
