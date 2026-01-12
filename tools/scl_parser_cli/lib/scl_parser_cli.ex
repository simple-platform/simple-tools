defmodule SCLParserCLI do
  @moduledoc """
  CLI entry point for parsing SCL files and outputting JSON.
  """

  # coveralls-ignore-start
  def main(args) do
    case run(args) do
      :ok -> System.halt(0)
      {:error, _} -> System.halt(1)
    end
  end

  # coveralls-ignore-stop

  @doc false
  def run(args) do
    case parse_args(args) do
      {:ok, filename} ->
        process_file(filename)

      {:error, msg} ->
        IO.puts(:stderr, "Error: #{msg}")
        {:error, msg}
    end
  end

  defp parse_args([filename]), do: {:ok, filename}
  defp parse_args(_), do: {:error, "Usage: scl-parser <file.scl>"}

  defp process_file(filename) do
    case File.read(filename) do
      {:ok, content} ->
        case SCLParser.parse(content) do
          {:ok, ast} ->
            json_friendly = convert_to_json_friendly(ast)
            IO.puts(JSON.encode!(json_friendly))
            :ok

          {:error, {msg, line, col}} ->
            error_json = JSON.encode!(%{error: msg, line: line, column: col})
            IO.puts(:stderr, error_json)
            {:error, msg}
        end

      {:error, reason} ->
        msg = "Error reading file: #{:file.format_error(reason)}"
        IO.puts(:stderr, msg)
        {:error, msg}
    end
  end

  # Convert AST (tuples) to JSON-friendly structure (Lists/Maps)
  defp convert_to_json_friendly(ast) when is_list(ast) do
    Enum.map(ast, &convert_to_json_friendly/1)
  end

  # Key-Value Pair
  defp convert_to_json_friendly({{key, kl, kc}, {val, vl, vc}}) do
    %{
      key: key,
      key_loc: %{line: kl, col: kc},
      value: convert_value(val),
      value_loc: %{line: vl, col: vc},
      type: "kv"
    }
  end

  # Key-MultiValue Pair
  defp convert_to_json_friendly({{key, kl, kc}, vals}) when is_list(vals) do
    %{
      key: key,
      key_loc: %{line: kl, col: kc},
      value: Enum.map(vals, fn {v, vl, vc} -> %{value: convert_value(v), line: vl, col: vc} end),
      type: "kv_multi"
    }
  end

  # Block
  defp convert_to_json_friendly({{key, kl, kc}, {name, nl, nc}, children}) do
    %{
      key: key,
      key_loc: %{line: kl, col: kc},
      name: convert_value(name),
      name_loc: %{line: nl, col: nc},
      children: convert_to_json_friendly(children),
      type: "block"
    }
  end

  # Fallback for simple values
  defp convert_value(v) when is_list(v), do: Enum.map(v, &convert_value/1)
  defp convert_value(v), do: v
end
