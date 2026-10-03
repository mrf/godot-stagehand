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
    files: ['tools/claude-mod/**/*.mjs'],
    extends: [js.configs.recommended],
    languageOptions: {
      globals: { process: 'readonly', console: 'readonly' },
    },
  },
)
