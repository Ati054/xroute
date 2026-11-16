import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";

function loadSource(name) {
  const filename = new URL(`../app/${name}`, import.meta.url);
  const source = readFileSync(filename, "utf8");
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  const exports = {};
  vm.runInNewContext(code, { exports, require: path => {
    if (path.endsWith(".json")) return { default: JSON.parse(readFileSync(new URL(`../app/${path}`, import.meta.url), "utf8")) };
    throw new Error(`Unexpected import: ${path}`);
  } }, { filename: String(filename) });
  return exports;
}
const catalog = loadSource("i18n-text.ts");
const { SUPPORTED_LOCALES, LOCALE_DETAILS, isLocale } = loadSource("locales.ts");
const placeholders = value => [...value.matchAll(/\{([A-Za-z0-9_]+)\}/g)].map(match => match[1]).sort();

test("dynamic card and editor metadata is present in every language catalog", () => {
  const fields = new Set(["label", "title", "name", "note", "description", "help", "hint", "category"]);
  const source = readFileSync(new URL("../app/console/shared.tsx", import.meta.url), "utf8");
  const tree = ts.createSourceFile("shared.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  function visit(node) {
    if (ts.isPropertyAssignment(node) && fields.has(node.name.getText(tree).replaceAll('"', ""))
      && ts.isStringLiteral(node.initializer) && /[А-Яа-яЁё]/u.test(node.initializer.text)) {
      const key = node.initializer.text;
      assert.ok(Object.hasOwn(catalog.englishText, key), `Missing dynamic metadata: ${key}`);
      for (const locale of SUPPORTED_LOCALES.filter(value => value !== "ru")) {
        assert.doesNotMatch(catalog.localizedText(key, locale), /[А-Яа-яЁё]/u, `${locale}: ${key}`);
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
});

test("operation result rows preserve server labels in every locale", () => {
  const source = readFileSync(new URL("../app/console/shared.tsx", import.meta.url), "utf8");
  const tree = ts.createSourceFile("shared.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const fn = tree.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === "resultRows");
  assert.ok(fn);
  const code = ts.transpileModule(fn.getText(tree), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  const exports = {};
  vm.runInNewContext(code, { exports, localizedText: catalog.localizedText, formatLocalizedText: catalog.formatLocalizedText });
  for (const locale of SUPPORTED_LOCALES) {
    const rows = exports.resultRows({ steps: [{ label: "DNS check", value: "OK" }, { type: "runtime", value: "Ready" }, { value: "fallback" }] }, locale);
    assert.equal(rows[0][0], "DNS check");
    assert.equal(rows[1][0], "runtime");
    assert.equal(rows[2][0], catalog.formatLocalizedText("Шаг {count}", locale, { count: 3 }));
  }
});

test("all supported translation catalogs preserve keys, placeholders and unique entries", () => {
  for (const locale of ["fa", "zh-Hans", "zh-Hant"]) {
    const raw = readFileSync(new URL(`../app/translations/${locale}.json`, import.meta.url), "utf8");
    const translated = JSON.parse(raw);
    const tree = ts.parseJsonText(`${locale}.json`, raw);
    const properties = tree.statements[0].expression.properties;
    assert.equal(properties.length, Object.keys(translated).length, `${locale}: duplicate keys`);
    assert.deepEqual(Object.keys(translated).sort(), Object.keys(catalog.englishText).sort(), `${locale}: catalog coverage`);
    for (const [key, value] of Object.entries(translated)) {
      assert.ok(typeof value === "string" && value.trim(), `${locale}: empty ${key}`);
      assert.doesNotMatch(value, /[А-Яа-яЁё]/u, `${locale}: Russian fallback`);
      assert.deepEqual(placeholders(value), placeholders(key), `${locale}: placeholders in ${key}`);
      assert.equal(catalog.localizedText(key, locale), value);
    }
  }
});

test("locale selection rejects invalid input and isolates substituted object names", () => {
  assert.equal(SUPPORTED_LOCALES.length, 5);
  for (const locale of SUPPORTED_LOCALES) {
    assert.equal(isLocale(locale), true);
    assert.equal(LOCALE_DETAILS[locale].direction, locale === "fa" ? "rtl" : "ltr");
    const text = catalog.formatLocalizedText("Имя {value1}", locale, { value1: "Device {value2}", value2: "WRONG" });
    assert.ok(text.includes("Device {value2}"), `${locale}: substitution must be single-pass`);
    assert.ok(!text.includes("WRONG"));
  }
  for (const value of ["de", "zh", "", null, {}, "__proto__"]) assert.equal(isLocale(value), false);
});

test("occupied image errors explain the refusal in every supported language", () => {
  const source = readFileSync(new URL("../app/console/shared.tsx", import.meta.url), "utf8");
  const tree = ts.createSourceFile("shared.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const fn = tree.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === "errorMessage");
  assert.ok(fn);
  const code = ts.transpileModule(fn.getText(tree), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  class ApiError extends Error { constructor(code) {super("Untranslated fallback");this.code = code;} }
  const exports = {};
  vm.runInNewContext(code, {exports, GatewayApiError: ApiError, asText: (value, fallback) => typeof value === "string" ? value : fallback, localizedText: catalog.localizedText, formatLocalizedText: catalog.formatLocalizedText});
  for (const locale of SUPPORTED_LOCALES) {
    assert.equal(exports.errorMessage(new ApiError("image_version_retained"), locale), catalog.localizedText("Эта версия сохранена для отката. Выберите другую версию.", locale));
    assert.equal(exports.errorMessage(new ApiError("image_root_in_use"), locale), catalog.localizedText("Каталог этого образа уже используется другим контейнером.", locale));
  }
});

test("translated labels cannot alter lifecycle confirmation tokens sent to the API", () => {
  for (const name of ["console/operations.tsx", "console/apply-dialog.tsx"]) {
    const source = readFileSync(new URL(`../app/${name}`, import.meta.url), "utf8");
    const tree = ts.createSourceFile(name, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    let checked = 0;
    function visit(node) {
      if (ts.isPropertyAssignment(node) && node.name.getText(tree) === "confirmation") {
        const expression = node.initializer.getText(tree);
        for (const locale of SUPPORTED_LOCALES) {
          for (const enabled of [true, false]) {
            const value = vm.runInNewContext(expression, { data: { get: () => enabled ? "on" : null }, tr: text => catalog.localizedText(text, locale) });
            assert.ok(["", "ОБНОВИТЬ", "ОТКАТИТЬ", "УДАЛИТЬ SB-GATEWAY"].includes(value));
            if (enabled) assert.notEqual(value, "");
          }
        }
        checked++;
      }
      ts.forEachChild(node, visit);
    }
    visit(tree);
    assert.ok(checked > 0, `${name}: confirmation expressions exercised`);
  }
});
