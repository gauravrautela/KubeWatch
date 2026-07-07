import { diffLines } from 'diff'

export type LineType = 'add' | 'del' | 'ctx'

export interface DiffLine {
  type: LineType
  text: string
}

export type Segment =
  | { kind: 'lines'; lines: DiffLine[] }
  | { kind: 'fold'; lines: DiffLine[] }

const CONTEXT = 3 // visible ctx lines kept adjacent to a change
const FOLD_THRESHOLD = 6 // ctx runs longer than this get folded

function pretty(json: string): string {
  if (!json) return ''
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json // malformed payload: diff the raw text
  }
}

function toLines(value: string, type: LineType): DiffLine[] {
  const body = value.endsWith('\n') ? value.slice(0, -1) : value
  if (body === '') return []
  return body.split('\n').map((text) => ({ type, text }))
}

function pushLines(segments: Segment[], lines: DiffLine[]): void {
  if (lines.length === 0) return
  const last = segments[segments.length - 1]
  if (last && last.kind === 'lines') last.lines.push(...lines)
  else segments.push({ kind: 'lines', lines: [...lines] })
}

// buildUnifiedDiff pretty-prints both JSON payloads, line-diffs them, and folds
// long unchanged runs behind expander segments (3 lines of context kept at each
// edge that touches a change; document edges keep none).
export function buildUnifiedDiff(oldJson: string, newJson: string): Segment[] {
  const parts = diffLines(pretty(oldJson), pretty(newJson))
  const all: DiffLine[] = []
  for (const p of parts) {
    all.push(...toLines(p.value, p.added ? 'add' : p.removed ? 'del' : 'ctx'))
  }

  const segments: Segment[] = []
  let i = 0
  while (i < all.length) {
    const type = all[i].type
    let j = i
    while (j < all.length && all[j].type === type) j++
    const run = all.slice(i, j)
    if (type !== 'ctx' || run.length <= FOLD_THRESHOLD) {
      pushLines(segments, run)
    } else {
      const head = i === 0 ? 0 : CONTEXT
      const tail = j === all.length ? 0 : CONTEXT
      pushLines(segments, run.slice(0, head))
      segments.push({ kind: 'fold', lines: run.slice(head, run.length - tail) })
      pushLines(segments, run.slice(run.length - tail))
    }
    i = j
  }
  return segments
}
