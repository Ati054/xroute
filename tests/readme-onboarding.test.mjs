import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("both introductions put requirements and a live panel preview before technical details", async () => {
  for (const [name, requirements, technical] of [
    ["README.md", "## What you need", "## Development"],
    ["README-RU.md", "## Что понадобится", "## Разработка"],
  ]) {
    const body = await readFile(new URL(`../${name}`, import.meta.url), "utf8");
    assert.ok(body.indexOf(requirements) > 0);
    assert.ok(body.indexOf(technical) > body.indexOf(requirements));
    assert.ok(body.indexOf("docs/images/panel-routing") < body.indexOf(technical));
    assert.doesNotMatch(body.slice(0, body.indexOf(requirements)), /TPROXY|REALITY|XHTTP|peer/);
    assert.match(body, /releases\/latest/);
    assert.doesNotMatch(body, /sb-gateway-1\.6\.45-routeros-bundle/);
  }
  const image = await readFile(new URL("../docs/images/panel-routing.png", import.meta.url));
  assert.equal(image.subarray(1, 4).toString(), "PNG");
  assert.ok(image.readUInt32BE(16) >= 1000);
  assert.ok(image.readUInt32BE(20) >= 600);
});


test("README language links resolve and the global landing page uses English UI", async () => {
  const readmes = ["README.md", "README-RU.md", "README-FA.md", "README-ZH.md", "README-ZH-TW.md"];
  for (const name of readmes) {
    const body = await readFile(new URL("../" + name, import.meta.url), "utf8");
    const localeImages = { "README.md": "panel-routing-en.png", "README-RU.md": "panel-routing.png", "README-FA.md": "panel-routing-fa.png", "README-ZH.md": "panel-routing-zh-Hans.png", "README-ZH-TW.md": "panel-routing-zh-Hant.png" };
    assert.ok(body.includes("docs/images/" + localeImages[name]), name + ": use the matching UI language");
    const overviewImages = { "README.md": "panel-overview-en.png", "README-RU.md": "panel-overview-ru.png", "README-FA.md": "panel-overview-fa.png", "README-ZH.md": "panel-overview-zh-Hans.png", "README-ZH-TW.md": "panel-overview-zh-Hant.png" };
    assert.ok(body.includes("docs/images/" + overviewImages[name]), name + ": use the matching overview language");
    const previewEnd = body.indexOf("## ", body.indexOf("panel-routing"));
    const screenshotSection = body.slice(0, previewEnd);
    assert.doesNotMatch(screenshotSection, /<details>/, name + ": both screenshots stay visible");
    for (const imageName of [overviewImages[name], localeImages[name]]) {
      const image = await readFile(new URL("../docs/images/" + imageName, import.meta.url));
      assert.equal(image.subarray(1, 4).toString(), "PNG");
      assert.ok(image.readUInt32BE(16) >= 1000, imageName + ": desktop width");
      assert.ok(image.readUInt32BE(20) >= 600, imageName + ": complete view");
    }
    const firstLine = body.split(/\r?\n/)[0];
    for (const target of readmes) assert.ok(firstLine.includes("(" + target + ")"), name + ": missing language link " + target);
    assert.ok(body.includes("https://github.com/Ati054/xroute-openwrt"));
    assert.match(body, /ARM64/);
    assert.match(body, /x86-64 CHR|CHR.*x86-64/);
    for (const match of body.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/g)) {
      if (!match[1].startsWith("http") && !match[1].startsWith("#")) await readFile(new URL("../" + match[1], import.meta.url));
    }
  }
  const english = await readFile(new URL("../README.md", import.meta.url), "utf8");
  assert.doesNotMatch(english.split(/\r?\n/).slice(1).join("\n"), /[А-Яа-яЁё]/u);
  assert.ok(english.includes("docs/images/panel-routing-en.png"));
  assert.ok(!english.includes("docs/images/panel-routing.png"));
});
