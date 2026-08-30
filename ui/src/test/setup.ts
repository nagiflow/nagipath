import '@testing-library/jest-dom/vitest'

// jsdom has no layout, so it ships no scrollIntoView; SnapshotFilePage calls it
// to jump to the highlighted line. ponytail: a no-op is the whole polyfill —
// nothing here asserts on scroll position.
Element.prototype.scrollIntoView ??= () => {}
