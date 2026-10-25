import type { SimpleClient, SpaceContext } from '@simpleplatform/sdk/space'
import type { ListPreset, TableRecordSource } from '@simpleplatform/ui-kit/contracts'
import { connect } from '@simpleplatform/sdk/space'
import { ListView } from '@simpleplatform/ui-kit/react'
import { useEffect, useState } from 'react'

interface Application {
  id: string
  display_name: string
  version: string
}

const GET_APPLICATIONS = `
query GetApplications {
  application_tables: dev_simple_system__tables(
    where: { application_id: { _eq: "dev.simple.system" }, name: { _eq: "application" } }
    limit: 1
  ) {
    id
  }
  applications: dev_simple_system__applications {
    id
    display_name
    version
  }
}
`

function applicationsSource(tableId: string): TableRecordSource {
  return {
    appId: 'dev.simple.system',
    kind: 'table',
    tableId,
  }
}

const APPLICATIONS_PRESET: ListPreset = {
  config: {
    tile: {
      detailFieldNames: ['version'],
      presentation: 'standard',
      titleFieldName: 'display_name',
      version: 1,
    },
  },
  id: 'applications-list',
  kind: 'list',
  orderBy: [{ direction: 'asc', fieldName: 'display_name' }],
  title: 'Applications',
}

const spaceConnection = Promise.resolve().then(() => {
  if (!document.referrer) {
    throw new Error('Open this Space from the platform or configure a trusted host origin.')
  }
  return connect({ targetOrigin: new URL(document.referrer).origin })
})

export default function App() {
  const [apps, setApps] = useState<Application[]>([])
  const [applicationTableId, setApplicationTableId] = useState('')
  const [context, setContext] = useState<SpaceContext | null>(null)
  const [simple, setSimple] = useState<SimpleClient | null>(null)
  const [errorMessage, setErrorMessage] = useState('Failed to load data.')
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')

  useEffect(() => {
    void spaceConnection
      .then(async (client) => {
        setSimple(client)
        setContext(client.context)
        const data = await client.data.query<{ application_tables?: Array<{ id: string }>, applications?: Application[] }>(GET_APPLICATIONS)
        const tableId = data.application_tables?.[0]?.id
        if (!tableId)
          throw new Error('The applications table is unavailable.')
        setApplicationTableId(tableId)
        setApps(data.applications ?? [])
        setStatus('ready')
      })
      .catch((error: unknown) => {
        if (error instanceof Error)
          setErrorMessage(error.message)
        setStatus('error')
      })
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
      {status === 'error' && <p className="text-destructive">{errorMessage}</p>}
      {status === 'ready' && (
        <>
          <section className="mb-6">
            <h2 className="text-lg font-semibold mb-2">Record View (ListView)</h2>
            {simple && (
              <ListView
                client={simple}
                preset={APPLICATIONS_PRESET}
                source={applicationsSource(applicationTableId)}
              />
            )}
          </section>

          <section className="mb-6">
            <h2 className="text-lg font-semibold mb-2">Direct GraphQL Query Demo</h2>
            <ul className="list-none p-0">
              {apps.map(app => (
                <li key={app.id} className="flex items-center justify-between py-2 border-b border-border">
                  <span className="font-medium">{app.display_name}</span>
                  <span className="text-sm text-muted-foreground">{app.version}</span>
                </li>
              ))}
              {apps.length === 0 && <li className="text-muted-foreground">No applications found.</li>}
            </ul>
          </section>
        </>
      )}

      <p className="text-sm text-muted-foreground mt-6">
        GraphQL:
        {' '}
        {status === 'loading' ? '⏳' : status === 'ready' ? '✅ Connected' : '❌ Error'}
      </p>
    </div>
  )
}
