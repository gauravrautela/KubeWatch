import '@testing-library/jest-dom'

// jsdom has no ResizeObserver; recharts' ResponsiveContainer requires one to mount.
// Recharts renders at size 0 under jsdom regardless (see task brief), so a minimal
// no-op stub is sufficient to let components mount without crashing.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
}
