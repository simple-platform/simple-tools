defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Debug logging to diagnose CI issues
    mode = Application.get_env(:scl_parser_cli, :mode)
    IO.puts("DEBUG: App starting. Mode: #{inspect(mode)}")
    IO.puts("DEBUG: Mix loaded? #{Code.ensure_loaded?(Mix)}")

    # Check configuration mode to determine if we should run the CLI logic
    # :cli -> Run the CLI (Production/Release)
    # :test -> Skip CLI execution (Test/Dev/IEx)
    if mode == :cli do
      IO.puts("DEBUG: Entering CLI mode")
      # In CLI mode, we expect Burrito to be available
      if Code.ensure_loaded?(Burrito.Util.Args) do
        args = Burrito.Util.Args.argv()
        IO.puts("DEBUG: Burrito args: #{inspect(args)}")
        SCLParserCLI.main(args)
      else
        IO.puts("DEBUG: Burrito module NOT loaded!")
        # Fallback to init args
        args = :init.get_plain_arguments() |> Enum.map(&List.to_string/1)
        IO.puts("DEBUG: Fallback args: #{inspect(args)}")
        SCLParserCLI.main(args)
      end
    else
      IO.puts("DEBUG: Skipping CLI logic (likely test mode)")
    end

    # Return a minimal supervisor

    # Return a minimal supervisor
    # In :cli mode, main/1 halts the system so we never reach here
    # In :test mode, we reach here and start the supervisor normally
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
