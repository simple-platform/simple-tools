defmodule SCLParserCLITest do
  use ExUnit.Case
  import ExUnit.CaptureIO

  @moduletag :tmp_dir

  test "shows usage when no args provided", %{tmp_dir: _tmp_dir} do
    stderr =
      capture_io(:stderr, fn ->
        result = SCLParserCLI.run([])
        assert {:error, _} = result
      end)

    assert stderr =~ "Usage: scl-parser <file.scl>"
  end

  test "reads file, parses, and outputs JSON", %{tmp_dir: tmp_dir} do
    scl_content = """
    name "test-app"
    version "1.0.0"
    """

    file_path = Path.join(tmp_dir, "test.scl")
    File.write!(file_path, scl_content)

    output =
      capture_io(fn ->
        assert :ok = SCLParserCLI.run([file_path])
      end)

    assert output =~ "\"key\":\"name\""
    assert output =~ "\"value\":\"test-app\""
  end

  test "outputs error on invalid file", %{tmp_dir: tmp_dir} do
    # Truly invalid SCL - unclosed block
    bad_content = """
    table users {
      id integer
    """

    file_path = Path.join(tmp_dir, "bad.scl")
    File.write!(file_path, bad_content)

    stderr =
      capture_io(:stderr, fn ->
        result = SCLParserCLI.run([file_path])
        assert {:error, _} = result
      end)

    assert stderr =~ "error"
  end

  test "outputs error for non-existent file" do
    stderr =
      capture_io(:stderr, fn ->
        result = SCLParserCLI.run(["/nonexistent/file.scl"])
        assert {:error, _} = result
      end)

    assert stderr =~ "File not found"
  end
end
