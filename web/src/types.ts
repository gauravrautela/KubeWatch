export type Operation = 'CREATE' | 'UPDATE' | 'DELETE'

export interface Row {
  event_id: string
  event_time: string
  ingested_at: string
  cluster: string
  source: string
  operation: Operation
  api_group: string
  api_version: string
  kind: string
  namespace: string
  name: string
  resource_uid: string
  sub_resource: string
  user_name: string
  user_groups: string[]
  dry_run: boolean
  diff: string
}

export interface Detail extends Row {
  old_object: string
  new_object: string
  user_uid: string
  user_agent: string
}

export interface Page {
  events: Row[]
  next_cursor: string
}

export interface Bucket {
  bucket_start: string
  count: number
}

export interface Facets {
  clusters: string[]
  namespaces: string[]
  kinds: string[]
  operations: string[]
}

export interface Filters {
  q?: string
  cluster?: string
  namespace?: string
  kind?: string
  name?: string
  user?: string
  operation?: string
  from?: string
  to?: string
  exclude_kinds?: string // comma-separated kind list
  exclude_namespaces?: string // comma-separated namespace list
  exclude_users?: string // comma-separated user list
  exclude_clusters?: string // comma-separated cluster list
  exclude_names?: string // comma-separated name list
  exclude_operations?: string // comma-separated operation list
}

export interface IncidentMeta {
  cluster: string
  at: string
  lookback: string
}

export interface SuspectEvent {
  event_id: string
  event_time: string
  operation: Operation
  classes: string[]
  actor_type: string
  user_name: string
}

export interface Suspect {
  namespace: string
  kind: string
  name: string
  score: number
  reasons: string[]
  event_count: number
  latest_event_time: string
  events: SuspectEvent[]
}

export interface IncidentResponse {
  incident: IncidentMeta
  suspects: Suspect[]
}
