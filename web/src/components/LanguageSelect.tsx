import { useTranslation } from "react-i18next";

export function LanguageSelect() {
  const { i18n, t } = useTranslation();
  return (
    <label className="text-xs text-slate-400">
      <span className="sr-only">{t("language.label")}</span>
      <select
        className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-slate-200"
        value={i18n.resolvedLanguage === "zh-CN" ? "zh-CN" : "en"}
        onChange={(event) => void i18n.changeLanguage(event.target.value)}
      >
        <option value="zh-CN">{t("language.chinese")}</option>
        <option value="en">{t("language.english")}</option>
      </select>
    </label>
  );
}
