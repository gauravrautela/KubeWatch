import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { fetchActivity, fetchEvent, fetchEvents, fetchFacets } from './client'
import type { Filters } from '../types'

const FEED_LIMIT = 50

// eventsKey is the shared query key for the feed. The live poller (Task 5) uses
// it to read and mutate the same cache entry.
export function eventsKey(filters: Filters) {
  return ['events', filters] as const
}

export function useEventsFeed(filters: Filters) {
  return useInfiniteQuery({
    queryKey: eventsKey(filters),
    queryFn: ({ pageParam }) =>
      fetchEvents(filters, { cursor: pageParam || undefined, limit: FEED_LIMIT }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  })
}

export function useEvent(id: string | undefined) {
  return useQuery({
    queryKey: ['event', id],
    queryFn: () => fetchEvent(id!),
    enabled: !!id,
  })
}

export function useActivity(filters: Filters, bucket: string) {
  return useQuery({
    queryKey: ['activity', filters, bucket],
    queryFn: () => fetchActivity(filters, bucket),
  })
}

export function useFacets() {
  return useQuery({ queryKey: ['facets'], queryFn: fetchFacets })
}
