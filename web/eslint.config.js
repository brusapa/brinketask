// ESLint flat configuration. Type-aware rules from typescript-eslint, the
// rules of hooks, and i18next's no-literal-string, which rejects text
// written straight into JSX: every user-facing string goes through the
// i18n layer (CLAUDE.md).
import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import i18next from "eslint-plugin-i18next";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default defineConfig(
  { ignores: ["dist", "src/api/schema.gen.ts"] },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: { allowDefaultProject: ["eslint.config.js", "icons/render.mjs"] },
        tsconfigRootDir: import.meta.dirname,
      },
    },
  },
  {
    // The service worker has its own tsconfig (WebWorker globals); the
    // project service only finds tsconfig.json.
    files: ["src/sw/sw.ts"],
    languageOptions: {
      parserOptions: { projectService: false, project: ["./tsconfig.sw.json"] },
    },
  },
  reactHooks.configs.flat.recommended,
  {
    files: ["src/**/*.tsx"],
    ignores: ["src/**/*.test.tsx", "src/test/**"],
    plugins: { i18next },
    rules: {
      // jsx-text-only: literals inside JSX text are errors; attributes such
      // as className or aria-hidden are not user-facing.
      "i18next/no-literal-string": ["error", { mode: "jsx-text-only" }],
    },
  },
  {
    rules: {
      // `const { a, ...rest } = x` is how JavaScript copies an object
      // without some fields; the dropped fields are unused by design.
      "@typescript-eslint/no-unused-vars": ["error", { ignoreRestSiblings: true }],
      // `onPress={() => close()}` is the usual React idiom for handlers.
      "@typescript-eslint/no-confusing-void-expression": ["error", { ignoreArrowShorthand: true }],
      // Template literals with numbers are common for CSS values and ids.
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
    },
  },
);
