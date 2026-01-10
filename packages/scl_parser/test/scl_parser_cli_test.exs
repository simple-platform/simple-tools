defmodule SCLParser.CLITest do
  use ExUnit.Case, async: false
  import ExUnit.CaptureIO

  alias SCLParser.CLI

  @tag :tmp_dir
  test "reads file, parses, and outputs JSON", %{tmp_dir: tmp_dir} do
    file_path = Path.join(tmp_dir, "test.scl")
    File.write!(file_path, "foo bar")

    output =
      capture_io(fn ->
        assert :ok == CLI.run([file_path])
      end)

    assert {:ok, json} = JSON.decode(output)
    assert is_list(json)
    # Check structure
    assert [%{"key" => "foo", "value" => "bar", "type" => "kv"}] = json
  end

  @tag :tmp_dir
  test "outputs useful error on invalid file", %{tmp_dir: tmp_dir} do
    file_path = Path.join(tmp_dir, "bad.scl")
    # unterminated
    File.write!(file_path, "table {")

    # Capture stderr?
    # CLI uses IO.puts(:stderr, ...)
    # capture_io(:stderr, fn -> ... end)

    output =
      capture_io(:stderr, fn ->
        assert {:error, _} = CLI.run([file_path])
      end)

    assert output =~ "unterminated block"
    assert {:ok, json} = JSON.decode(output)
    assert json["error"] =~ "unterminated block"
  end

  test "usage error if no args" do
    output =
      capture_io(:stderr, fn ->
        assert {:error, "Usage: scl_parser <file.scl>"} = CLI.run([])
      end)

    assert output =~ "Usage: scl_parser <file.scl>"
  end
end
