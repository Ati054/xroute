const labels: Record<string, string> = {
  ok: "Успешно",
  applied: "Применено",
  completed: "Завершено",
  healthy: "Работает",
  failed: "Ошибка",
  error: "Ошибка",
  pending: "Ожидание",
  running: "Выполняется",
  scheduled: "Запланировано",
  denied: "Отказано в доступе",
  rejected: "Отклонено",
  staged: "Подготовлено",
  unchanged: "Без изменений",
  rolled_back: "Выполнен откат",
  candidate_rejected: "Отклонено",
  success: "Успешно",
  schedule_unconfirmed: "Запуск не подтверждён",
  confirmation_pending: "Ожидает подтверждения",
  unconfirmed: "Не подтверждено",
  persistent_storage_read_only: "Диск доступен только для чтения",
  recovery_archive_required: "Нужна резервная копия",
  recovery_create_failed: "Ошибка",
  "wan-direct": "Напрямую через WAN",
  drop: "Блокировка",
  apply: "Применение настроек",
  "draft.save": "Сохранение черновика",
  "draft.patch": "Сохранение черновика",
  "draft.reset": "Сброс черновика",
  rollback: "Откат конфигурации",
  "routeros.import": "Импорт конфигурации RouterOS",
  "routeros.credentials.provision": "Настройка доступа к RouterOS",
  "auth.bootstrap": "Создание пароля панели",
  "auth.login": "Вход в панель",
  "auth.logout": "Выход из панели",
  "auth.password_changed": "Смена пароля панели",
  "recovery.create": "Создание резервной копии",
  "recovery.backup": "Создание резервной копии",
  "recovery.upload": "Загрузка резервной копии",
  "recovery.restore": "Восстановление из копии",
  "recovery.mirror_reconciled": "Зеркало резервов синхронизировано",
  "recovery.backup_reconciled": "Резервная копия синхронизирована",
  "recovery.backup_scheduled": "Запланировано резервное копирование",
  "subscriptions.refresh": "Обновление подписок",
  "subscriptions.runtime": "Применение изменений подписки",
  "cdn-feeds.refresh": "Обновление IP-списков CDN",
  "service-packs.resolve": "Загрузка списка сервисов",
  "container.memory": "Настройка памяти контейнера",
  "container.image_uploaded": "Загрузка образа контейнера",
  "container.image_update": "Обновление контейнера",
  "container.image_update_schedule": "Подготовка обновления контейнера",
  "container.image_update_scheduled": "Обновление контейнера запланировано",
  "container.full_uninstall_scheduled": "Удаление XRoute запланировано",
  "tls.export": "Экспорт TLS-профиля",
  "tls.import": "Импорт TLS-профиля",
  "tls.acme.configure": "Настройка ACME",
  "tls.acme.disable": "Отключение ACME",
  "remote_users.export": "Скачан клиентский профиль",
  "remote_users.subscription_link": "Открыта ссылка клиентской подписки",
  "remote_users.subscription_link_rotated": "Обновлена ссылка клиентской подписки",
  "reverse_vless_exits.client_config": "Скачана конфигурация Reverse VLESS",
  "xray.logging.debug_timeout": "Завершение подробного журнала Xray",
};

const auditCollections = new Set([
  "clients", "local_clients", "networks", "policies", "remote_users", "reverse_vless_exits",
  "subscription_reserves", "subscriptions", "tls_profiles", "transports",
]);

export function operationLabel(value: unknown, translate: (source: string) => string): string {
  if (typeof value !== "string" || !value) return "—";
  if (Object.hasOwn(labels, value)) return translate(labels[value]);
  const [collection, mutation, extra] = value.split(".");
  if (!extra && auditCollections.has(collection)) {
    if (mutation === "create") return translate("Добавлен объект конфигурации");
    if (mutation === "update") return translate("Настройки объекта изменены");
    if (mutation === "delete") return translate("Объект конфигурации удалён");
  }
  return value;
}

export function operationOutcomeTone(value: unknown): "ok" | "danger" | "info" | "muted" {
  if (["ok", "applied", "completed", "healthy", "success", "unchanged"].includes(String(value))) return "ok";
  if (["error", "failed", "rejected", "denied", "candidate_rejected", "recovery_create_failed", "persistent_storage_read_only", "recovery_archive_required"].includes(String(value))) return "danger";
  if (["pending", "running", "scheduled", "staged", "confirmation_pending", "schedule_unconfirmed", "unconfirmed"].includes(String(value))) return "info";
  return "muted";
}
