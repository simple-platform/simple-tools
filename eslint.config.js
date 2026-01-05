import antfu from '@antfu/eslint-config'
import globals from 'globals'

export default antfu({
  formatters: {
    markdown: 'prettier',
  },

  ignores: [
    'node_modules/',
    'dist/',
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
