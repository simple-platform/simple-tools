defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Only run CLI in a compiled prod release (not during tests, dev, or iex)
    # Burrito sets __BURRITO_IS_PROD env var during prod builds
    if Mix.env() == :prod and Code.ensure_loaded?(Burrito.Util.Args) do
      case Burrito.Util.Args.argv() do
        # Empty or nil means not running from Burrito wrapper
        args when is_list(args) and args != [] ->
          SCLParserCLI.main(args)

        _ ->
          :ok
      end
    end

    # Return a minimal supervisor
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  rescue
    # If anything goes wrong (e.g., Burrito not available), just start normally
    _ -> Supervisor.start_link([], strategy: :one_for_one, name: SCLParserCLI.Supervisor)
  end
end
