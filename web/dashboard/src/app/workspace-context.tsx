import type { Capability, Company, Customer, Member, Tag, Team, Workspace } from "@hubchat/shared";
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import {
  companies,
  currentMember,
  currentWorkspace,
  customers,
  members,
  tags,
  teams,
  workspaces,
} from "../data/fixtures";

/**
 * Workspace scope for the whole dashboard.
 *
 * Two jobs:
 *   1. hold the active workspace and viewer, so nothing has to thread them
 *      through props;
 *   2. expose `can()` — the client-side capability check.
 *
 * `can()` is a *UI affordance* only. It decides whether to render a button, not
 * whether an action is permitted. §11.3 puts real authorization in the Go
 * service layer, and every mutation is re-checked there. Hiding a control the
 * server would reject is courtesy; relying on that hiding would be a defect.
 */
type WorkspaceContextValue = {
  workspace: Workspace;
  workspaces: Workspace[];
  viewer: Member;
  members: Member[];
  teams: Team[];
  can: (capability: Capability) => boolean;
  switchWorkspace: (id: string) => void;

  // Denormalised lookups. The real client will resolve these from a normalised
  // entity cache; the signatures are what components depend on.
  memberById: (id: string | null) => Member | undefined;
  customerById: (id: string | null) => Customer | undefined;
  companyById: (id: string | null) => Company | undefined;
  tagById: (id: string) => Tag | undefined;
  tags: Tag[];
};

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [activeId, setActiveId] = useState(currentWorkspace.id);

  const workspace = workspaces.find((item) => item.id === activeId) ?? currentWorkspace;

  const can = useCallback(
    (capability: Capability) => currentMember.capabilities.includes(capability),
    [],
  );

  const value = useMemo<WorkspaceContextValue>(
    () => ({
      workspace,
      workspaces,
      viewer: currentMember,
      members,
      teams,
      can,
      switchWorkspace: setActiveId,
      memberById: (id) => (id ? members.find((member) => member.id === id) : undefined),
      customerById: (id) => (id ? customers.find((customer) => customer.id === id) : undefined),
      companyById: (id) => (id ? companies.find((company) => company.id === id) : undefined),
      tagById: (id) => tags.find((tag) => tag.id === id),
      tags,
    }),
    [workspace, can],
  );

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace(): WorkspaceContextValue {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error("useWorkspace must be used within a <WorkspaceProvider>");
  return context;
}
