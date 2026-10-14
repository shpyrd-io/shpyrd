// Replace declared editorial placeholders without evaluating expressions.
export function interpolate<T>(value: T, variables: Record<string, string | number>): T {
  if (typeof value === "string") {
    return value.replace(/\{\{([a-zA-Z][a-zA-Z0-9]*)\}\}/g, (_, name: string) => {
      if (!Object.hasOwn(variables, name)) throw new Error(`Unknown content placeholder: ${name}`);
      return String(variables[name]);
    }) as T;
  }
  if (Array.isArray(value)) return value.map((item) => interpolate(item, variables)) as T;
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, interpolate(item, variables)])) as T;
  }
  return value;
}
