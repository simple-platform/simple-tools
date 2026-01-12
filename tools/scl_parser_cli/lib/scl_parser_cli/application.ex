defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Runtime Check for Test Environment
    # If ExUnit is loaded, we are testing. Start Supervisor.
    # If ExUnit is NOT loaded, we are a Release. Run Script & Halt.
    if Code.ensure_loaded?(ExUnit) do
      children = []
      opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
      Supervisor.start_link(children, opts)
    else
      # Burrito CLI Mode

      # 1. Fetch Arguments safely
      argv =
        if Code.ensure_loaded?(Burrito.Util.Args) do
          Burrito.Util.Args.get_arguments()
        else
          :init.get_plain_arguments() |> Enum.map(&List.to_string/1)
        end

      # 2. Execute Main Logic
      SCLParserCLI.main(argv)

      # 3. Explicit Halt (if main hasn't already halted)
      System.halt(0)
    end
  end
end
