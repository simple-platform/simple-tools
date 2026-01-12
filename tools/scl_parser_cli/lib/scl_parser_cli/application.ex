defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # When running in a release (Burrito binary), the Mix module is not available.
    # When running 'mix test' or 'mix run', the Mix module IS available.
    # We use this to detect if we should run the CLI logic.
    unless Code.ensure_loaded?(Mix) do
      # We are in a release.
      # Retrieve args from Burrito wrapper and run the CLI.
      args = Burrito.Util.Args.argv()
      SCLParserCLI.main(args)
    end

    # Return a minimal supervisor.
    # In a release, SCLParserCLI.main/1 calls System.halt/1, so we won't reach here.
    # In tests, we reach here and start the supervisor normally.
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
