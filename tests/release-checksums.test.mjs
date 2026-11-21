import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

test("packaging preserves both architectures and refuses corrupt prior assets", (t) => {
  const result = spawnSync("pwsh", ["-NoProfile", "-File", fileURLToPath(new URL("./release-checksums.test.ps1", import.meta.url))], { encoding: "utf8", timeout: 30000 });
  if (result.error?.code === "ENOENT") return t.skip("PowerShell 7 is not installed");
  assert.equal(result.status, 0, result.stderr || result.error?.message);
  assert.match(result.stdout, /MULTI_ARCH_CHECKSUMS_PASS/);
});
