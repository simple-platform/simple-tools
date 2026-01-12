defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  # Capture Mix.env at compile time since Mix isn't available at runtime in releases
  @env Mix.env()

  @impl true
  def start(_type, _args) do
    # Only run CLI in a compiled prod release (not during tests, dev, or iex)
    if @env == :prod do
      # In prod, always run the CLI (Burrito context)
      args =
        if Code.ensure_loaded?(Burrito.Util.Args) do
          Burrito.Util.Args.argv()
        else
          []
        end

      # Always call CLI in prod - it will show usage if no args
      SCLParserCLI.main(args)
    end

    # Return a minimal supervisor (won't reach here in prod due to System.halt)
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  rescue
    # If anything goes wrong, just start normally
    _ -> Supervisor.start_link([], strategy: :one_for_one, name: SCLParserCLI.Supervisor)
  end
end
