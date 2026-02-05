defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # In test mode, we don't want to run the CLI logic which halts the system.
    # We return a dummy supervisor to satisfy the Application behavior.
    if function_exported?(Mix, :env, 0) && Mix.env() == :test do
      Supervisor.start_link([], strategy: :one_for_one)
    else
      alias Burrito.Util.Args
      argv = Args.get_arguments()
      SCLParserCLI.main(argv)
      System.halt(0)
    end
  end
end
