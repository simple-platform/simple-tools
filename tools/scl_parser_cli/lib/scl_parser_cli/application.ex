defmodule SCLParserCLI.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    argv = Burrito.Util.Args.get_arguments()
    SCLParserCLI.main(argv)
    System.halt(0)
  end
end
