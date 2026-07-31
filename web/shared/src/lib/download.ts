/** Download tabular data without sending customer data to a third party. */
export function downloadCSV(filename: string, columns: string[], rows: (string | number | null)[][]): void {
  const escape = (value: string | number | null): string => {
    const text = value === null ? "" : String(value);
    return /[",\n\r]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
  };
  const csv = [columns, ...rows].map((row) => row.map(escape).join(",")).join("\r\n");
  const url = URL.createObjectURL(new Blob([`\uFEFF${csv}\r\n`], { type: "text/csv;charset=utf-8" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

/** Download an authenticated API file while preserving the active workspace. */
export async function downloadFile(path: string, filename: string, workspaceId?: string): Promise<void> {
  const headers: Record<string, string> = {};
  if (workspaceId) headers["Hubchat-Workspace-Id"] = workspaceId;
  const response = await fetch(`/api/v1${path}`, { credentials: "include", headers });
  if (!response.ok) {
    const payload = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(payload?.error?.message ?? `Download failed with status ${response.status}`);
  }
  const url = URL.createObjectURL(await response.blob());
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
