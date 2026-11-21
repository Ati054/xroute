export const SUPPORTED_LOCALES = ["ru", "en", "fa", "zh-Hans", "zh-Hant"] as const;

export type Locale = (typeof SUPPORTED_LOCALES)[number];

export const LOCALE_DETAILS: Record<Locale, { name: string; short: string; direction: "ltr" | "rtl"; format: string }> = {
  ru: { name: "Русский", short: "RU", direction: "ltr", format: "ru-RU" },
  en: { name: "English", short: "EN", direction: "ltr", format: "en-GB" },
  fa: { name: "فارسی", short: "FA", direction: "rtl", format: "fa-IR" },
  "zh-Hans": { name: "简体中文", short: "简体", direction: "ltr", format: "zh-CN" },
  "zh-Hant": { name: "繁體中文", short: "繁體", direction: "ltr", format: "zh-TW" },
};

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && SUPPORTED_LOCALES.some((locale) => locale === value);
}

export function browserLocale(language: string): Locale {
  const parts = language.toLowerCase().replaceAll("_", "-").split("-");
  if (parts[0] === "ru") return "ru";
  if (parts[0] === "fa") return "fa";
  if (parts[0] === "zh") {
    return parts.some((part) => ["hant", "tw", "hk"].includes(part)) ? "zh-Hant" : "zh-Hans";
  }
  return "en";
}

export function localeFormat(locale: Locale): string {
  return LOCALE_DETAILS[locale].format;
}
