// ESLint for the godot-stagehand mod. Run from the repository root with
// `-c tools/claude-mod/eslint.config.mjs` (see package.json's lint script),
// because the mod lives in integrations/claude-code/hooks, outside this folder.
import js from '@eslint/js'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  {
    files: ['integrations/claude-code/hooks/**/*.ts'],
    extends: [js.configs.recommended, ...tseslint.configs.strictTypeChecked, ...tseslint.configs.stylisticTypeChecked],
    languageOptions: {
      parserOptions: {
        project: './tools/claude-mod/tsconfig.json',
        tsconfigRootDir: import.meta.dirname + '/../..',
      },
    },
  },
  {
    // Deliberate relaxation, for the two files that call `on`. For each call,
    // no-misused-promises' checksVoidReturn.arguments reads the callee's
    // signatures unresolved. For the engine's generic `On` that means
    // HookFor<Pattern>, the hook type of every event. Against Claude Code
    // 2.1.296's declarations the rule took 819 s on register.ts alone (30 s
    // against 2.1.288's), and register.test.ts is as slow. Against 2.1.302's
    // the two files had not finished after 300 s. This turns off that
    // one check for every call in these files. The rest of the rule stays on
    // here, and the other hooks files keep all of it.
    files: ['integrations/claude-code/hooks/register.ts', 'integrations/claude-code/hooks/register.test.ts'],
    rules: {
      '@typescript-eslint/no-misused-promises': ['error', { checksVoidReturn: { arguments: false } }],
    },
  },
  {
    files: ['tools/claude-mod/**/*.mjs'],
    extends: [js.configs.recommended],
    languageOptions: {
      globals: { process: 'readonly', console: 'readonly' },
    },
  },
)
