import type { StorybookConfig } from '@storybook/react-vite'

// Storybook is the design workspace for this UI (see src/stories/*.mdx) and
// the single source of truth for how the product looks: every screen has a
// page story here that is both its implementation and its design record.
const config: StorybookConfig = {
  stories: ['../src/**/*.mdx', '../src/**/*.stories.tsx'],
  addons: ['@storybook/addon-docs'],
  framework: { name: '@storybook/react-vite', options: {} },
  staticDirs: ['../public'],
}

export default config
