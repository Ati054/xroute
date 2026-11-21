import assert from "node:assert/strict";
import test from "node:test";
import { browserLocale } from "../app/locales.ts";

test("first visit chooses a supported language from the browser locale", () => {
  for (const [language, expected] of [
    ["ru-RU", "ru"], ["ru-KZ", "ru"], ["fa-IR", "fa"], ["fa", "fa"],
    ["zh-TW", "zh-Hant"], ["zh-HK", "zh-Hant"], ["zh-Hant", "zh-Hant"],
    ["ZH_hant_HK", "zh-Hant"], ["zh-CN", "zh-Hans"], ["zh-SG", "zh-Hans"],
    ["zh", "zh-Hans"], ["en-US", "en"], ["de-DE", "en"], ["", "en"],
  ]) assert.equal(browserLocale(language), expected, language);
});
