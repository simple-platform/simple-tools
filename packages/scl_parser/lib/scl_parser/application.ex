defmodule SCLParser.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Get CLI args from Burrito wrapper
    args = Burrito.Util.Args.argv()

    # Run the CLI with the args
    SCLParser.CLI.main(args)

    # The CLI calls System.halt, so we won't reach here in normal operation
    # But we need to return a valid supervisor for Application behavior
    children = []
    opts = [strategy: :one_for_one, name: SCLParser.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
