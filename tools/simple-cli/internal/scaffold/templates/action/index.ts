import type { Request } from '@simpleplatform/sdk'
import simple from '@simpleplatform/sdk'

/**
 * Handler function for the {{.ActionName}} action.
 * This is exported for testing purposes.
 */
export async function handler(_req: Request) {
  // Your action logic here
  return { message: 'Hello, World!' }
}

// Register the handler with the Simple Platform runtime
simple.Handle(handler)
