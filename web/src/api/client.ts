import type { Bucket, Detail, Facets, Filters, IncidentResponse, Page } from '../types'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

function filtersToParams(f: Filters): URLSearchParams {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(f)) {
    if (v) p.set(k, v)
  }
  return p
}

async function getJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url)
  if (!resp.ok) {
    let msg = `request failed (${resp.status})`
    try {
      const body = await resp.json()
      if (body && typeof body.error === 'string') msg = body.error
    } catch {
      // non-JSON error body; keep default message
    }
    throw new ApiError(resp.status, msg)
  }
  return (await resp.json()) as T
}

export interface FeedOpts {
  cursor?: string
  since?: string
  limit?: number
}

export function fetchEvents(filters: Filters, opts: FeedOpts = {}): Promise<Page> {
  const p = filtersToParams(filters)
  if (opts.cursor) p.set('cursor', opts.cursor)
  if (opts.since) p.set('since', opts.since)
  if (opts.limit) p.set('limit', String(opts.limit))
  return getJSON<Page>(`/api/events?${p.toString()}`)
}

export function fetchEvent(id: string): Promise<Detail> {
  return getJSON<Detail>(`/api/events/${encodeURIComponent(id)}`)
}

export function fetchActivity(filters: Filters, bucket: string): Promise<Bucket[]> {
  const p = filtersToParams(filters)
  p.set('bucket', bucket)
  return getJSON<Bucket[]>(`/api/activity?${p.toString()}`)
}

export function fetchFacets(): Promise<Facets> {
  return getJSON<Facets>('/api/facets')
}

export interface IncidentQuery {
  cluster: string
  at?: string
  lookback?: string
  limit?: number
}

export function fetchIncident(q: IncidentQuery): Promise<IncidentResponse> {
  const p = new URLSearchParams({ cluster: q.cluster })
  if (q.at) p.set('at', q.at)
  if (q.lookback) p.set('lookback', q.lookback)
  if (q.limit) p.set('limit', String(q.limit))
  return getJSON<IncidentResponse>(`/api/incident?${p.toString()}`)
}
