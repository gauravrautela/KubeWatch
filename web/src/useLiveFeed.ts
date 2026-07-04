import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { InfiniteData } from '@tanstack/react-query'
import { eventsKey } from './api/hooks'
import { encodeCursor } from './api/cursor'
import { fetchEvents } from './api/client'
import type { Filters, Page } from './types'

const POLL_MS = 5000

// useLiveFeed polls for events newer than the newest row currently in the feed
// cache and prepends any new ones (newest-first). A since-response is ASC and may
// re-include the boundary row, so we dedupe against ids already present.
export function useLiveFeed(filters: Filters): void {
  const queryClient = useQueryClient()

  useEffect(() => {
    const key = eventsKey(filters)

    const tick = async () => {
      const data = queryClient.getQueryData<InfiniteData<Page>>(key)
      const newest = data?.pages?.[0]?.events?.[0]
      if (!newest) return

      let page: Page
      try {
        page = await fetchEvents(filters, { since: encodeCursor(newest) })
      } catch {
        return
      }
      if (page.events.length === 0) return

      const existing = new Set(data!.pages.flatMap((p) => p.events.map((e) => e.event_id)))
      const fresh = page.events.filter((e) => !existing.has(e.event_id))
      if (fresh.length === 0) return

      // since-response is ASC (oldest-new first); reverse for newest-first display
      const prepend = [...fresh].reverse()
      queryClient.setQueryData<InfiniteData<Page>>(key, (old) => {
        if (!old) return old
        const pages = old.pages.slice()
        pages[0] = { ...pages[0], events: [...prepend, ...pages[0].events] }
        return { ...old, pages }
      })
    }

    const id = setInterval(tick, POLL_MS)
    return () => clearInterval(id)
  }, [filters, queryClient])
}
