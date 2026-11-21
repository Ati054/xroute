// Display/prefill only. Lifecycle requests still verify the live owned
// container and validate its dedicated directory on the server.
export function projectRootFromContainer(value: unknown): string {
  if (typeof value !== "string") return "";
  const parts = value.replace(/^\/+|\/+$/g, "").split("/");
  const leaf = parts.pop() ?? "";
  if (!/^root(?:-[A-Za-z0-9._-]+)?$/.test(leaf) || !parts.length ||
      parts.some((part) => !/^[A-Za-z0-9._-]+$/.test(part) || part === "." || part === "..") ||
      parts[0].toLowerCase().startsWith("flash")) return "";
  return parts.join("/");
}
