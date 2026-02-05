defmodule SCLParserCLITest do
  use ExUnit.Case
  import ExUnit.CaptureIO

  @moduletag :tmp_dir

  @moduledoc """
  Integration tests for the SCL Parser CLI.

  These tests verify that the CLI binary logic works as expected when invoked with arguments.
  We capture standard output and error to assert the CLI's behavior.
  """

  # Test Case: Missing arguments
  test "shows usage when no args provided", %{tmp_dir: _tmp_dir} do
    stderr =
      capture_io(:stderr, fn ->
        result = SCLParserCLI.run([])
        assert {:error, _} = result
      end)

    assert stderr =~ "Usage: scl-parser <file.scl>"
  end

  # Test Case: Valid usage
  # Verifies that a valid SCL file is read, parsed, and converted to the expected JSON format.
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

    # Simple check for JSON structure
    assert output =~ "\"key\":\"name\""
    assert output =~ "\"value\":\"test-app\""
  end

  # Test Case: Invalid SCL syntax
  # Verifies that the parser error is correctly bubbled up to stderr as a JSON error object.
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

  # Test Case: File not found
  # Verifies that non-existent files result in a user-friendly error message.
  test "outputs error for non-existent file" do
    stderr =
      capture_io(:stderr, fn ->
        result = SCLParserCLI.run(["/nonexistent/file.scl"])
        assert {:error, _} = result
      end)

    assert stderr =~ "File not found"
    assert stderr =~ "Please check the file path"
  end
end
