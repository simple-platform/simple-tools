defmodule SCLParser.ExpressionParser do
  @moduledoc """
  Parses SCL expression strings like `$var('foo') |> $jq('.bar')`.

  These expressions typically appear within backtick strings in SCL files.
  The parser supports:
  - Function calls prefixed with `$`
  - Pipe operator `|>` for chaining functions
  - String literals (single, double, backtick quoted)
  - Number and boolean literals
  - Nested expressions
  """

  @int_regex ~r/^[0-9]+$/
  @float_regex ~r/^[0-9]+\.[0-9]+$/

  @doc """
  Parses an SCL expression string.

  Handles function calls starting with `$` and allows piping with `|>`.
  Parameters can be strings (single, double, backtick quoted), numbers, or booleans.

  ## Examples

      iex> SCLParser.ExpressionParser.parse("$var('foo')")
      {:ok, [%{fn: "var", params: ["foo"]}]}

      iex> SCLParser.ExpressionParser.parse("$var(123) |> $add(10)")
      {:ok, [%{fn: "var", params: [123]}, %{fn: "add", params: [10]}]}

      iex> SCLParser.ExpressionParser.parse("not an expression")
      {:error, "Expression must start with '$'"}

      iex> SCLParser.ExpressionParser.parse("$func(unterminated 'string)")
      {:error, "Unterminated single-quoted string in parameter list"}

      iex> SCLParser.ExpressionParser.parse("$func() |> ")
      {:error, "Expected '$' after '|>'"}


  ## Returns

  - `{:ok, list_of_maps}` where each map is `%{fn: String.t(), params: list()}`.
  - `{:error, reason :: String.t()}` on failure.
  """
  @spec parse(String.t()) :: {:ok, list(map())} | {:error, String.t()}
  def parse(input) when is_binary(input) do
    case tokenize_expression(input) do
      {:ok, tokens} -> parse_expression_tokens(tokens)
      {:error, reason} -> {:error, reason}
    end
  end

  # -------------------------------------------------------------------
  # Expression Tokenizer
  # -------------------------------------------------------------------

  # Entry point
  defp tokenize_expression(input), do: do_tokenize_expression(input, [])

  # End of input
  defp do_tokenize_expression(<<>>, acc), do: {:ok, Enum.reverse(acc)}

  # Whitespace => skip
  defp do_tokenize_expression(<<c, rest::binary>>, acc) when c in [?\s, ?\t],
    do: do_tokenize_expression(rest, acc)

  # Newlines (treat as whitespace)
  defp do_tokenize_expression(<<"\r\n", rest::binary>>, acc),
    do: do_tokenize_expression(rest, acc)

  defp do_tokenize_expression(<<"\n", rest::binary>>, acc), do: do_tokenize_expression(rest, acc)
  defp do_tokenize_expression(<<"\r", rest::binary>>, acc), do: do_tokenize_expression(rest, acc)

  # Pipe operator
  defp do_tokenize_expression(<<"|>", rest::binary>>, acc),
    do: do_tokenize_expression(rest, [{:pipe, "|>"} | acc])

  # Dollar sign
  defp do_tokenize_expression(<<"$", rest::binary>>, acc),
    do: do_tokenize_expression(rest, [{:dollar, "$"} | acc])

  # Parentheses
  defp do_tokenize_expression(<<"(", rest::binary>>, acc),
    do: do_tokenize_expression(rest, [{:lparen, "("} | acc])

  defp do_tokenize_expression(<<")", rest::binary>>, acc),
    do: do_tokenize_expression(rest, [{:rparen, ")"} | acc])

  # Comma
  defp do_tokenize_expression(<<",", rest::binary>>, acc),
    do: do_tokenize_expression(rest, [{:comma, ","} | acc])

  # Backtick string parameter
  defp do_tokenize_expression(<<"`", rest::binary>>, acc) do
    case consume_expr_backtick_string(rest, []) do
      {:ok, txt, new_rest} ->
        do_tokenize_expression(new_rest, [{:string, txt} | acc])

      {:error, reason} ->
        {:error, reason <> " in parameter list"}
    end
  end

  # Double-quoted string parameter
  defp do_tokenize_expression(<<"\"", rest::binary>>, acc) do
    case consume_expr_quoted_string(rest, []) do
      {:ok, txt, new_rest} ->
        do_tokenize_expression(new_rest, [{:string, txt} | acc])

      {:error, reason} ->
        {:error, reason <> " in parameter list"}
    end
  end

  # Single-quoted string parameter
  defp do_tokenize_expression(<<"'", rest::binary>>, acc) do
    case consume_expr_single_quoted_string(rest, []) do
      {:ok, txt, new_rest} ->
        do_tokenize_expression(new_rest, [{:string, txt} | acc])

      {:error, reason} ->
        {:error, reason <> " in parameter list"}
    end
  end

  # Identifier (function name or boolean) or number
  defp do_tokenize_expression(<<c, _rest::binary>> = bin, acc)
       when c in ?a..?z or c in ?A..?Z or c in ?0..?9 or c == ?_ or c == ?. do
    {txt, rest2} = consume_expr_identifier_or_literal(bin, [])

    if txt != "" do
      token = classify_expr_literal(txt)
      do_tokenize_expression(rest2, [token | acc])
    else
      {:error, "Invalid character '#{<<c>>}' in expression"}
    end
  end

  # Fallback => invalid character
  defp do_tokenize_expression(<<c, _::binary>>, _acc),
    do: {:error, "Invalid character '#{<<c>>}' in expression"}

  # Helper: Consume backtick string
  defp consume_expr_backtick_string(<<"`", rest::binary>>, acc),
    do: {:ok, IO.iodata_to_binary(Enum.reverse(acc)), rest}

  defp consume_expr_backtick_string(<<"\\`", rest::binary>>, acc),
    do: consume_expr_backtick_string(rest, [?` | acc])

  defp consume_expr_backtick_string(<<>>, _acc),
    do: {:error, "Unterminated backtick-quoted string"}

  defp consume_expr_backtick_string(<<"\n", _rest::binary>>, _acc),
    do: {:error, "Newline not allowed in backtick-quoted parameter string"}

  defp consume_expr_backtick_string(<<c, rest::binary>>, acc),
    do: consume_expr_backtick_string(rest, [c | acc])

  # Helper: Consume double-quoted string
  defp consume_expr_quoted_string(<<"\"", rest::binary>>, acc),
    do: {:ok, IO.iodata_to_binary(Enum.reverse(acc)), rest}

  defp consume_expr_quoted_string(<<"\\\"", rest::binary>>, acc),
    do: consume_expr_quoted_string(rest, [?" | acc])

  defp consume_expr_quoted_string(<<"\\n", rest::binary>>, acc),
    do: consume_expr_quoted_string(rest, [?\n | acc])

  defp consume_expr_quoted_string(<<"\\t", rest::binary>>, acc),
    do: consume_expr_quoted_string(rest, [?\t | acc])

  defp consume_expr_quoted_string(<<"\\\\", rest::binary>>, acc),
    do: consume_expr_quoted_string(rest, [?\\ | acc])

  defp consume_expr_quoted_string(<<>>, _acc), do: {:error, "Unterminated double-quoted string"}

  defp consume_expr_quoted_string(<<"\n", _rest::binary>>, _acc),
    do: {:error, "Raw newline not allowed in double-quoted parameter string"}

  defp consume_expr_quoted_string(<<c, rest::binary>>, acc),
    do: consume_expr_quoted_string(rest, [c | acc])

  # Helper: Consume single-quoted string
  defp consume_expr_single_quoted_string(<<"'", rest::binary>>, acc),
    do: {:ok, IO.iodata_to_binary(Enum.reverse(acc)), rest}

  defp consume_expr_single_quoted_string(<<"\\'", rest::binary>>, acc),
    do: consume_expr_single_quoted_string(rest, [?' | acc])

  defp consume_expr_single_quoted_string(<<"\\n", rest::binary>>, acc),
    do: consume_expr_single_quoted_string(rest, [?\n | acc])

  defp consume_expr_single_quoted_string(<<"\\t", rest::binary>>, acc),
    do: consume_expr_single_quoted_string(rest, [?\t | acc])

  defp consume_expr_single_quoted_string(<<"\\\\", rest::binary>>, acc),
    do: consume_expr_single_quoted_string(rest, [?\\ | acc])

  defp consume_expr_single_quoted_string(<<>>, _acc),
    do: {:error, "Unterminated single-quoted string"}

  defp consume_expr_single_quoted_string(<<"\n", _rest::binary>>, _acc),
    do: {:error, "Raw newline not allowed in single-quoted parameter string"}

  defp consume_expr_single_quoted_string(<<c, rest::binary>>, acc),
    do: consume_expr_single_quoted_string(rest, [c | acc])

  # Helper: Consume identifier chars or numeric literals
  defp consume_expr_identifier_or_literal(<<c, rest::binary>>, acc)
       when c in ?a..?z or c in ?A..?Z or c in ?0..?9 or c == ?_ or c == ?.,
       do: consume_expr_identifier_or_literal(rest, [c | acc])

  defp consume_expr_identifier_or_literal(rest, acc),
    do: {IO.iodata_to_binary(Enum.reverse(acc)), rest}

  # Helper: Classify unquoted literal
  defp classify_expr_literal("true"), do: {:boolean, true}
  defp classify_expr_literal("false"), do: {:boolean, false}

  defp classify_expr_literal(str) do
    cond do
      Regex.match?(@int_regex, str) -> {:number, String.to_integer(str)}
      Regex.match?(@float_regex, str) -> {:number, String.to_float(str)}
      true -> {:identifier, str}
    end
  end

  # -------------------------------------------------------------------
  # Expression Parser (from tokens)
  # -------------------------------------------------------------------

  defp parse_expression_tokens(tokens) do
    case tokens do
      [{:dollar, "$"} | _] -> parse_expression_segments(tokens, [])
      [] -> {:error, "Expression cannot be empty"}
      _ -> {:error, "Expression must start with '$'"}
    end
  end

  # Parse segments separated by pipes
  defp parse_expression_segments([], acc), do: {:ok, Enum.reverse(acc)}

  defp parse_expression_segments([{:pipe, "|>"} | rest], acc),
    do: parse_expression_segments(rest, acc)

  defp parse_expression_segments(tokens, acc) do
    case parse_single_segment(tokens) do
      {:ok, segment_ast, [{:pipe, "|>"} | remaining_tokens]} ->
        case remaining_tokens do
          [{:dollar, "$"} | _] ->
            parse_expression_segments(remaining_tokens, [segment_ast | acc])

          [] ->
            {:error, "Expected '$' after '|>'"}

          [{_, next} | _] ->
            {:error, "Expected '$' after '|>', got token: #{next}"}
        end

      {:ok, segment_ast, []} ->
        {:ok, Enum.reverse([segment_ast | acc])}

      {:ok, _segment_ast, [{_, next} | _]} ->
        {:error, "Expected '|>' or end of expression after function call, got token: #{next}"}

      {:error, reason} ->
        {:error, reason}
    end
  end

  # Parse a single $fn(...) segment
  defp parse_single_segment([{:dollar, "$"} | rest]) do
    parse_single_segment_after_dollar(rest)
  end

  defp parse_single_segment([{type, val} | _]),
    do: {:error, "Expression segment must start with '$', got #{type}: #{inspect(val)}"}

  defp parse_single_segment_after_dollar([{:identifier, fn_name} | rest]) do
    parse_single_segment_after_fn_name(fn_name, rest)
  end

  defp parse_single_segment_after_dollar([{type, val} | _]),
    do: {:error, "Expected function name after '$', got #{type}: #{inspect(val)}"}

  defp parse_single_segment_after_dollar([]),
    do: {:error, "Expected function name after '$', got end of input"}

  defp parse_single_segment_after_fn_name(fn_name, [{:lparen, "("} | rest]) do
    case parse_params(rest, [], fn_name) do
      {:ok, params, [{:rparen, ")"} | remaining_tokens]} ->
        {:ok, %{fn: fn_name, params: Enum.reverse(params)}, remaining_tokens}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp parse_single_segment_after_fn_name(fn_name, [{type, val} | _]),
    do: {:error, "Expected '(' after function name '#{fn_name}', got #{type}: #{inspect(val)}"}

  defp parse_single_segment_after_fn_name(fn_name, []),
    do: {:error, "Expected '(' after function name '#{fn_name}', got end of input"}

  # Parse comma-separated parameters within parentheses
  defp parse_params([{:rparen, ")"} = rparen | rest], acc, _fn_name),
    do: {:ok, acc, [rparen | rest]}

  defp parse_params([], _acc, fn_name),
    do: {:error, "Unterminated parameter list (missing ')') for function '#{fn_name}'"}

  defp parse_params([{:comma, ","} | _], [], fn_name),
    do: {:error, "Unexpected comma before first parameter in '#{fn_name}'"}

  # First param or param after comma
  defp parse_params([token | rest], acc, fn_name) do
    # Pass the rest of the tokens to parse_one_param
    case parse_one_param(token, rest, fn_name) do
      {:ok, val, remaining_tokens} ->
        # Use remaining_tokens returned by parse_one_param
        parse_after_param(remaining_tokens, [val | acc], fn_name)

      {:error, reason} ->
        {:error, reason}
    end
  end

  # After parsing a parameter, expect comma or rparen
  defp parse_after_param([{:rparen, ")"} = rparen | rest], acc, _fn_name),
    do: {:ok, acc, [rparen | rest]}

  defp parse_after_param([{:comma, ","} | rest], acc, fn_name),
    do: parse_params_after_comma(rest, acc, fn_name)

  defp parse_after_param([], _acc, fn_name),
    do: {:error, "Unterminated parameter list (missing ')') after parameter in '#{fn_name}'"}

  defp parse_after_param([{type, val} | _], _acc, fn_name),
    do:
      {:error,
       "Expected ',' or ')' after parameter in '#{fn_name}', got #{type}: #{inspect(val)}"}

  # After a comma, expect the next parameter
  defp parse_params_after_comma([token | rest], acc, fn_name) do
    case token do
      {:comma, ","} ->
        {:error, "Unexpected comma after comma in '#{fn_name}'"}

      {:rparen, ")"} ->
        {:error, "Unexpected ')' after comma in '#{fn_name}'"}

      _ ->
        # Pass the rest of the tokens to parse_one_param
        case parse_one_param(token, rest, fn_name) do
          {:ok, val, remaining_tokens} ->
            # Use remaining_tokens returned by parse_one_param
            parse_after_param(remaining_tokens, [val | acc], fn_name)

          {:error, reason} ->
            {:error, reason}
        end
    end
  end

  defp parse_params_after_comma([], _acc, fn_name),
    do: {:error, "Expected parameter after comma in '#{fn_name}', got end of input"}

  # Helper to parse a single parameter token
  # Now takes `rest_tokens` and returns `{:ok, value, remaining_tokens}` or `{:error, reason}`
  defp parse_one_param({token_type, val}, rest_tokens, fn_name) do
    case token_type do
      :string ->
        {:ok, val, rest_tokens}

      :number ->
        {:ok, val, rest_tokens}

      :boolean ->
        {:ok, val, rest_tokens}

      # Allows JQ strings etc.
      :identifier ->
        {:ok, val, rest_tokens}

      # Handle nested expression
      :dollar ->
        # Re-include the dollar token for parse_single_segment
        original_tokens = [{:dollar, "$"} | rest_tokens]

        case parse_single_segment(original_tokens) do
          {:ok, nested_ast, remaining_after_nested} ->
            # The nested AST map is the value
            {:ok, nested_ast, remaining_after_nested}

          {:error, reason} ->
            # Propagate error, possibly prefixing context?
            {:error, "Error parsing nested expression within '#{fn_name}': #{reason}"}
        end

      _ ->
        {:error,
         "Invalid token in parameter list for '#{fn_name}': #{token_type}: #{inspect(val)}"}
    end
  end
end
