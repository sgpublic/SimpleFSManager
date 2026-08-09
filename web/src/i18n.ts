import i18n from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";
import zhCN from "./locales/zh-CN.json";

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: { en: { translation: en }, "zh-CN": { translation: zhCN } },
    fallbackLng: "en",
    supportedLngs: ["en", "zh-CN"],
    detection: { order: ["localStorage", "navigator"], caches: ["localStorage"], lookupLocalStorage: "simplefsmanager-language" },
    interpolation: { escapeValue: false },
  });

i18n.on("languageChanged", (language) => {
  document.documentElement.lang = language;
});

export default i18n;
