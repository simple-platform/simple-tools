import type { Request } from '@simpleplatform/sdk'
import simple from '@simpleplatform/sdk'

/**
 * Handler function for the {{.ActionName}} action.
 * This is exported for testing purposes.
{{if .Tool}}{{printf " * @tool\n * @shortdesc TODO(simple): Say in one line what this tool gives an agent.\n"}}{{end}}
 */
export async function handler(_req: Request) {
  // Your action logic here
  return { message: 'Hello, World!' }
}

// Register the handler with the Simple Platform runtime
simple.Handle(handler)
