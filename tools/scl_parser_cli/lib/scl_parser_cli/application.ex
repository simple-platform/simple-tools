defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    alias Burrito.Util.Args
    argv = Args.get_arguments()
    SCLParserCLI.main(argv)
    System.halt(0)
  end
end
