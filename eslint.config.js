import antfu from '@antfu/eslint-config'
import globals from 'globals'

export default antfu({
  formatters: {
    markdown: 'prettier',
  },

  ignores: [
    'node_modules/',
    'dist/',
    // TestGeneratedWorkspacePassesLint judges these with their shipped config.
    'tools/simple-cli/internal/scaffold/templates/',
  ],

  languageOptions: {
    globals: {
      ...globals.node,
    },
  },

  rules: {
    'import/order': ['off'],
    'perfectionist/sort-objects': 'error',
  },

  stylistic: true,
  typescript: true,
})
