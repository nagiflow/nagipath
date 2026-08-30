import type { ComponentType } from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { composeStories, setProjectAnnotations } from '@storybook/react-vite'
import { afterEach, expect, test } from 'vitest'
import * as projectAnnotations from '../../.storybook/preview'
import { takeMissedFixtures } from '../stories/support/mockApi'

// One check for the whole Storybook: every story renders, and every /api call it
// makes is answered by a fixture. That is what keeps the fixtures honest — a
// renamed endpoint or a story that forgot a fixture fails here rather than
// showing an error panel in a story nobody opens.
setProjectAnnotations(projectAnnotations)

afterEach(cleanup)

type StoriesModule = Parameters<typeof composeStories>[0]

const modules = import.meta.glob<StoriesModule>('../**/*.stories.tsx', { eager: true })

for (const [path, mod] of Object.entries(modules)) {
  // Composed stories are components that need no props; the generic loses that
  // when the module comes from a glob.
  const stories = composeStories(mod) as Record<string, ComponentType>

  for (const [name, Story] of Object.entries(stories)) {
    test(`${path.replace('../', '')} · ${name}`, async () => {
      const { container } = render(<Story />)
      // The fixture fetch resolves in a microtask; this lets react-query hand
      // the data to the page before we look at what it rendered.
      await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)) })
      expect(takeMissedFixtures()).toEqual([])
      expect(container.textContent).not.toContain('no story fixture')
    })
  }
}
