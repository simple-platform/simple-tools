import antfu from '@antfu/eslint-config'

export default antfu({
  formatters: {
    css: true,
    html: true,
    markdown: 'prettier',
  },

  ignores: [
    'node_modules/',
    'dist/',
    // Written by the Simple CLI and rewritten by it, so not ours to format.
    '.agent/',
    '.simple/',
  ],

  rules: {
    'perfectionist/sort-objects': 'error',
  },

  stylistic: true,
  typescript: true,
})
