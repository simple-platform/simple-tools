defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Only run CLI if we're in a Burrito release (not during tests or IEx)
    if function_exported?(Burrito.Util.Args, :argv, 0) do
      case Burrito.Util.Args.argv() do
        [] ->
          # No args from Burrito means we're likely not in a release context
          :ok

        [_ | _] = args ->
          # We have args from Burrito, run the CLI
          SCLParserCLI.main(args)
      end
    end

    # Return a minimal supervisor
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
