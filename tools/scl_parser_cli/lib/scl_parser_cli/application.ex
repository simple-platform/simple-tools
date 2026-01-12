defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # 1. Try to get explicit mode from config
    config_mode = Application.get_env(:scl_parser_cli, :mode)

    # 2. Heuristic detection: In release/prod, ExUnit is usually missing.
    #    In 'mix test', ExUnit is present.
    is_test_env = Code.ensure_loaded?(ExUnit)

    # 3. Determine final mode with fallback
    mode =
      case config_mode do
        :cli ->
          :cli

        :test ->
          :test

        nil ->
          # Fallback if config is missing:
          if is_test_env, do: :test, else: :cli

        _ ->
          :test
      end

    # Debug log (stderr)
    IO.puts(
      :stderr,
      "DEBUG: Start. Config: #{inspect(config_mode)}. ExUnit?: #{is_test_env}. Final: #{mode}"
    )

    if mode == :cli do
      IO.puts(:stderr, "DEBUG: Running CLI main...")
      args = get_args()
      SCLParserCLI.main(args)
    end

    # Return supervisor (for tests or if main failed/didn't halt)
    children = []
    opts = [strategy: :one_for_one, name: SCLParserCLI.Supervisor]
    Supervisor.start_link(children, opts)
  end

  defp get_args do
    if Code.ensure_loaded?(Burrito.Util.Args) do
      Burrito.Util.Args.argv()
    else
      IO.puts(:stderr, "DEBUG: Burrito missing. Using init args.")
      :init.get_plain_arguments() |> Enum.map(&List.to_string/1)
    end
  end
end
