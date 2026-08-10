# Localization

The widget currently ships translated interface text for English, French,
Spanish, and Arabic. Unknown locales fall back to English. The portal uses the
viewer language when available and otherwise uses the portal default language.

Arabic and other configured RTL locales switch document direction. New
customer-facing strings must be added to every supported widget message table
and must not concatenate user-visible sentences in a way that prevents
translation.

When adding a locale, verify:

- loading, empty, error, retry, and upload states;
- forms, validation, and long labels;
- dates, numbers, and timezone labels;
- keyboard focus and screen-reader names;
- narrow mobile widths;
- RTL layout where applicable.

The dashboard remains deployment-language driven for now; dashboard content is
not silently translated based on the visitor's browser locale.
