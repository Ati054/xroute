import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// These exclusion patterns are constants, not interpolated RouterOS strings.
function decodeLiteral(raw) {
  const escapes = { "\\": "\\", '"': '"', $: "$", n: "\n", r: "\r", t: "\t" };
  let decoded = "";
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] === "\\") {
      const next = raw[++i];
      assert.ok(Object.hasOwn(escapes, next), "Unsupported RouterOS string escape");
      decoded += escapes[next];
    } else {
      assert.notEqual(raw[i], "$", "A literal regex anchor must be escaped in RouterOS source");
      decoded += raw[i];
    }
  }
  return decoded;
}

for (const name of ["preflight.rsc", "uninstall.rsc"]) {
  test(`${name}: CHR bare-directory exclusions remain static, anchored RouterOS regexes`, async () => {
    const source = await readFile(new URL(`../routeros/${name}`, import.meta.url), "utf8");
    const exclusions = source.split("\n").find((line) => line.includes("select a dedicated project directory"));
    assert.ok(exclusions, "Missing dedicated-directory validation");
    const patterns = [...exclusions.matchAll(/~\s*"((?:\\.|[^"\\])*)"/g)]
      .map((match) => new RegExp(decodeLiteral(match[1])));
    assert.ok(patterns.length >= 2, "Missing bare-directory exclusions");
    const excluded = (value) => patterns.some((pattern) => pattern.test(value));

    for (const value of ["disk1", "usb12", "slot2", "sata1", "nvme3", "skins", "pub", "hotspot", "web-proxy"]) {
      assert.ok(excluded(value), `A dedicated project directory is required instead of ${value}`);
    }
    for (const value of ["xroute", "containers", "my-disk1", "disk1-xroute", "usb-backup", "public", "skins-xroute"]) {
      assert.equal(excluded(value), false, `Unexpected exclusion of project directory ${value}`);
    }
  });
}
