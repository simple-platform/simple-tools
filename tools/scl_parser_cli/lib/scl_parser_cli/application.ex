defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Get mode from Application env (defined in mix.exs, overridden by config/test.exs)
    mode = Application.get_env(:scl_parser_cli, :mode)

    if mode == :cli do
      # Robustly fetch args
      args =
        if Code.ensure_loaded?(Burrito.Util.Args) do
          Burrito.Util.Args.argv()
        else
          :init.get_plain_arguments() |> Enum.map(&List.to_string/1)
        end

      SCLParserCLI.main(args)
    end

    # Return supervisor
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
