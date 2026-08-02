/**
 * Evaluate the condition shape used by public form fields.
 *
 * This deliberately mirrors the server's supported operators so the builder
 * preview and the widget do not disagree about which fields are visible.
 */
export function formConditionApplies(
  condition: Record<string, unknown> | null | undefined,
  values: Record<string, unknown>,
): boolean {
  if (!condition || Object.keys(condition).length === 0) return true;
  const field = typeof condition.field === "string" ? condition.field : typeof condition.key === "string" ? condition.key : "";
  if (!field) return true;

  const operator = typeof condition.operator === "string" ? condition.operator : "";
  const actual = values[field];
  const expected = condition.value;
  const hasValue = Object.prototype.hasOwnProperty.call(values, field);
  const isBlank = actual == null || (typeof actual === "string" && actual.trim() === "") || (Array.isArray(actual) && actual.length === 0);

  switch (operator) {
    case "equals":
    case "is":
      return hasValue && String(actual ?? "") === String(expected ?? "");
    case "not_equals":
    case "is_not":
      return !hasValue || String(actual ?? "") !== String(expected ?? "");
    case "contains":
      return Array.isArray(actual)
        ? actual.some((value) => String(value) === String(expected ?? ""))
        : String(actual ?? "").includes(String(expected ?? ""));
    case "is_set":
      return !isBlank;
    case "is_not_set":
      return isBlank;
    default:
      // Compatibility with early public form payloads that used condition
      // keys instead of the standardized operator/value pair.
      if ("equals" in condition) return actual === condition.equals;
      if ("not_equals" in condition) return actual !== condition.not_equals;
      return true;
  }
}
