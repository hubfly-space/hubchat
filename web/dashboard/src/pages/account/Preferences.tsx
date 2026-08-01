import {
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  Kbd,
  Page,
  PageBody,
  PageHeader,
  Section,
  SegmentedControl,
  Select,
  SettingsRow,
  Switch,
  useTheme,
  type Density,
  type ThemeMode,
} from "@hubchat/shared";
import { Monitor, Moon, Rows2, Rows3, Sun } from "lucide-react";

const SHORTCUTS = [
  { group: "Global", items: [
    { keys: "mod+k", label: "Search and commands" },
    { keys: "g i", label: "Go to inbox" },
    { keys: "g t", label: "Go to tickets" },
    { keys: "g c", label: "Go to customers" },
    { keys: "?", label: "Show this list" },
  ]},
  { group: "Inbox", items: [
    { keys: "j", label: "Next conversation" },
    { keys: "k", label: "Previous conversation" },
    { keys: "a", label: "Assign to me" },
    { keys: "l", label: "Add a tag" },
    { keys: "e", label: "Resolve" },
    { keys: "s", label: "Snooze" },
    { keys: "n", label: "Switch to internal note" },
  ]},
  { group: "Composer", items: [
    { keys: "mod+enter", label: "Send" },
    { keys: "mod+/", label: "Saved replies and macros" },
  ]},
];

/** Personal preferences (§6.15, §21). */
export default function Preferences() {
  const { mode, setMode, density, setDensity } = useTheme();

  return (
    <Page>
      <PageHeader
        title="Preferences"
        description="Device-level settings. These follow your browser, not your account."
      />

      <PageBody width="narrow">
        <Section title="Appearance">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow label="Theme">
                <SegmentedControl
                  aria-label="Theme"
                  value={mode}
                  onValueChange={(value) => setMode(value as ThemeMode)}
                  size="md"
                  options={[
                    { value: "dark", label: "Dark", icon: <Moon /> },
                    { value: "light", label: "Light", icon: <Sun /> },
                    { value: "system", label: "System", icon: <Monitor /> },
                  ]}
                />
              </SettingsRow>

              <SettingsRow
                label="Dashboard density"
                description="Compact tightens chrome, narrows panels, and gives the dashboard a floating workspace feel."
              >
                <SegmentedControl
                  aria-label="Density"
                  value={density}
                  onValueChange={(value) => setDensity(value as Density)}
                  size="md"
                  options={[
                    { value: "comfortable", label: "Comfortable", icon: <Rows3 /> },
                    { value: "compact", label: "Compact", icon: <Rows2 /> },
                  ]}
                />
              </SettingsRow>

              <SettingsRow
                label="Reduce motion"
                description="Your system setting is respected automatically. This forces it on regardless."
              >
                <Switch aria-label="Reduce motion" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Inbox behaviour">
          <Card>
            <CardBody className="pt-0">
              <SettingsRow
                label="After resolving"
                description="Where you land once a conversation leaves your queue."
              >
                <Select
                  size="sm"
                  defaultValue="next"
                  aria-label="After resolving"
                  options={[
                    { value: "next", label: "Open the next conversation" },
                    { value: "list", label: "Return to the list" },
                    { value: "stay", label: "Stay on the resolved thread" },
                  ]}
                />
              </SettingsRow>

              <SettingsRow
                label="Mark as read on open"
                description="Off means you clear the unread flag yourself, which some agents prefer for triage."
              >
                <Switch defaultChecked aria-label="Mark as read on open" />
              </SettingsRow>

              <SettingsRow label="Play a sound on new conversations">
                <Switch aria-label="Sound on new conversations" />
              </SettingsRow>

              <SettingsRow
                label="Show the customer context panel"
                description="Hidden automatically below 1280px regardless of this setting."
              >
                <Switch defaultChecked aria-label="Show context panel" />
              </SettingsRow>
            </CardBody>
          </Card>
        </Section>

        <Section title="Notifications">
          <Card>
            <CardHeader
              title="Override workspace defaults"
              description="Unchecked rows fall back to whatever the workspace has configured."
            />
            <CardBody className="space-y-3">
              <Checkbox
                label="Email me when a conversation is assigned to me"
                description="Overrides the workspace default."
                defaultChecked
              />
              <Checkbox label="Email me on mentions" defaultChecked />
              <Checkbox label="Browser notifications for SLA warnings" defaultChecked />
              <Checkbox label="Daily summary of my queue" />
            </CardBody>
          </Card>
        </Section>

        <Section title="Keyboard shortcuts">
          <div className="grid gap-3 sm:grid-cols-2">
            {SHORTCUTS.map((group) => (
              <Card key={group.group}>
                <CardHeader title={group.group} />
                <CardBody className="p-0">
                  <ul className="divide-y divide-line-subtle">
                    {group.items.map((shortcut) => (
                      <li
                        key={shortcut.keys}
                        className="flex items-center justify-between gap-3 px-4 py-2"
                      >
                        <span className="min-w-0 truncate text-xs text-fg-secondary">
                          {shortcut.label}
                        </span>
                        <Kbd keys={shortcut.keys} />
                      </li>
                    ))}
                  </ul>
                </CardBody>
              </Card>
            ))}
          </div>

        </Section>
      </PageBody>
    </Page>
  );
}
