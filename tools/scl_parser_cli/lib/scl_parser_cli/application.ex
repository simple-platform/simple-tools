defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Check configuration mode to determine if we should run the CLI logic
    # :cli -> Run the CLI (Production/Release)
    # :test -> Skip CLI execution (Test/Dev/IEx)
    if Application.get_env(:scl_parser_cli, :mode) == :cli do
      # In CLI mode, we expect Burrito to be available
      args = Burrito.Util.Args.argv()
      SCLParserCLI.main(args)
    end

    # Return a minimal supervisor
    # In :cli mode, main/1 halts the system so we never reach here
    # In :test mode, we reach here and start the supervisor normally
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
