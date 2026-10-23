import type { SpaceContext } from '@simpleplatform/sdk/space'
import type { SimpleStatusBadgeTone } from '@simpleplatform/ui-kit'
import { connect } from '@simpleplatform/sdk/space'
import { StatusBadge } from '@simpleplatform/ui-kit/react'
import { useEffect, useState } from 'react'

interface Application {
  id: string
  display_name: string
  version: string
}

const GET_APPLICATIONS = `
query GetApplications {
  applications: dev_simple_system__applications {
    id
    display_name
    version
  }
}
`

// The connection's state, shown with the UI Kit's StatusBadge rather than a
// hand-made pill, so it matches every other status in Simple.
const CONNECTION: Record<'loading' | 'ready' | 'error', { label: string, tone: SimpleStatusBadgeTone }> = {
  error: { label: 'Not connected', tone: 'danger' },
  loading: { label: 'Connecting', tone: 'info' },
  ready: { label: 'Connected', tone: 'success' },
}

// Trusted configuration is required when referrer is unavailable.
const spaceConnection = connect({
  targetOrigin: new URL(document.referrer).origin,
})

export default function App() {
  const [apps, setApps] = useState<Application[]>([])
  const [context, setContext] = useState<SpaceContext | null>(null)
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    void spaceConnection
      .then(async (simple) => {
        setContext(simple.context)
        const data = await simple.data.query<{ applications: Application[] }>(GET_APPLICATIONS)
        setApps(data.applications ?? [])
        setStatus('ready')
      })
      .catch(() => setStatus('error'))
  }, [])

  return (
    <div className="p-8 font-sans">
      <h1 className="text-2xl font-bold mb-4">{'{{.DisplayName}}'}</h1>

      {context && (
        <p className="text-sm text-muted-foreground mb-4">
          {`Space context: ${context.kind}`}
        </p>
      )}

      {status === 'loading' && <p className="text-muted-foreground">Loading applications…</p>}
      {status === 'error' && <p className="text-destructive">Failed to load data.</p>}
      {status === 'ready' && (
        <ul className="m-0 list-none rounded-lg border border-border bg-card px-4 py-0">
          {apps.map(app => (
            <li key={app.id} className="flex items-center justify-between py-2 border-b border-border">
              <span className="font-medium">{app.display_name}</span>
              <span className="text-sm text-muted-foreground">{app.version}</span>
            </li>
          ))}
          {apps.length === 0 && <li className="text-muted-foreground">No applications found.</li>}
        </ul>
      )}

      <div className="mt-6 flex items-center gap-2 text-sm text-muted-foreground">
        GraphQL
        <StatusBadge label={CONNECTION[status].label} tone={CONNECTION[status].tone} />
      </div>
    </div>
  )
}
