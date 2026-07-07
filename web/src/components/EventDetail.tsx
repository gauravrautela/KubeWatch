import { Link, useLocation, useParams } from 'react-router-dom'
import { useEvent } from '../api/hooks'
import { OperationBadge } from './OperationBadge'
import { DiffView } from './DiffView'

function Meta({ label, value }: { label: string; value: string }) {
  if (!value) return null
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-zinc-500">{label}</dt>
      <dd className="truncate text-zinc-200" title={value}>{value}</dd>
    </div>
  )
}

function CopyButton({ label, text }: { label: string; text: string }) {
  return (
    <button
      className="rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800"
      onClick={() => navigator.clipboard?.writeText(text)}
    >
      {label}
    </button>
  )
}

export function EventDetailPage() {
  const { id } = useParams()
  const location = useLocation()
  const { data, isLoading, isError, refetch } = useEvent(id)

  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <Link to={`/${location.search}`} className="text-sm text-sky-400 hover:text-sky-300">
        ← Events
      </Link>

      {isLoading && <div className="py-10 text-center text-sm text-zinc-500">Loading…</div>}
      {!isLoading && (isError || !data) && (
        <div className="py-10 text-center text-sm text-zinc-400">
          Failed to load event.{' '}
          <button className="text-sky-400 underline" onClick={() => refetch()}>
            Retry
          </button>
        </div>
      )}

      {data && (
        <>
          <header className="mt-4 flex flex-wrap items-center gap-3">
            <OperationBadge op={data.operation} />
            <h1 className="font-mono text-lg text-zinc-100">
              <span className="text-zinc-500">{data.kind}</span>{' '}
              {data.namespace ? `${data.namespace}/${data.name}` : data.name}
            </h1>
            {data.dry_run && (
              <span className="rounded border border-zinc-600 px-1.5 py-0.5 text-xs text-zinc-400">dry-run</span>
            )}
          </header>

          <dl className="mt-4 grid grid-cols-2 gap-x-8 gap-y-3 rounded-lg border border-zinc-800 bg-zinc-900/60 p-4 text-sm sm:grid-cols-3">
            <Meta label="cluster" value={data.cluster} />
            <Meta label="user" value={data.user_name} />
            <Meta label="groups" value={data.user_groups.join(', ')} />
            <Meta label="time" value={new Date(data.event_time).toLocaleString()} />
            <Meta label="api" value={`${data.api_group || 'core'}/${data.api_version}`} />
            <Meta label="sub-resource" value={data.sub_resource} />
            <Meta label="user agent" value={data.user_agent} />
            <Meta label="source" value={data.source} />
            <Meta label="event id" value={data.event_id} />
          </dl>

          <div className="mt-5 flex items-center gap-2">
            <h2 className="text-sm font-semibold text-zinc-300">Change</h2>
            <span className="flex-1" />
            <CopyButton label="Copy before" text={data.old_object} />
            <CopyButton label="Copy after" text={data.new_object} />
          </div>
          <div className="mt-2">
            <DiffView oldJson={data.old_object} newJson={data.new_object} />
          </div>
        </>
      )}
    </main>
  )
}
