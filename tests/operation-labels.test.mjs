import assert from "node:assert/strict";
import test from "node:test";
import { operationLabel, operationOutcomeTone } from "../app/operation-labels.ts";
import { projectRootFromContainer } from "../app/storage-root.ts";

test("successful Apply outcomes stay successful and known audit codes are localized", () => {
  for (const status of ["applied", "ok", "completed", "healthy"]) assert.equal(operationOutcomeTone(status), "ok");
  assert.equal(operationOutcomeTone("failed"), "danger");
  assert.equal(operationOutcomeTone("denied"), "danger");
  assert.equal(operationOutcomeTone("staged"), "info");
  assert.equal(operationOutcomeTone("future-status"), "muted");
  const translate = (key) => `translated:${key}`;
  assert.equal(operationLabel("draft.save", translate), "translated:Сохранение черновика");
  assert.equal(operationLabel("routeros.import", translate), "translated:Импорт конфигурации RouterOS");
  assert.equal(operationLabel("auth.bootstrap", translate), "translated:Создание пароля панели");
  assert.equal(operationLabel("recovery.create", translate), "translated:Создание резервной копии");
  assert.equal(operationLabel("local_clients.create", translate), "translated:Добавлен объект конфигурации");
  assert.equal(operationLabel("policies.update", translate), "translated:Настройки объекта изменены");
  assert.equal(operationLabel("tls.import", translate), "translated:Импорт TLS-профиля");
  assert.equal(operationLabel("constructor", translate), "constructor");
  assert.equal(operationLabel("wan-direct", translate), "translated:Напрямую через WAN");
  assert.equal(operationLabel("future.action", translate), "future.action");
});

test("project directory prefill handles SSD and CHR roots without accepting traversal", () => {
  assert.equal(projectRootFromContainer("/usb1/xroute/root-1.7.2"), "usb1/xroute");
  assert.equal(projectRootFromContainer("xroute/root-1.7.2-amd64"), "xroute");
  assert.equal(projectRootFromContainer("pcie1/sb-gateway/root"), "pcie1/sb-gateway");
  for (const root of ["usb1/xroute/other", "usb1/../root-1.7.2", "flash/xroute/root", "root", "usb1//root", undefined]) assert.equal(projectRootFromContainer(root), "");
});
