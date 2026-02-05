defmodule SCLParser do
  @moduledoc """
  A line-aware SCL parser that includes line/col info in the final AST.

  ## Overview

  This parser has three main stages:

  1. **Tokenization** – Converts raw input string into a list of tokens (with line/col).
  2. **Parsing** – Converts the list of tokens into a "raw" AST that reflects the SCL structure (blocks, key-value lines, etc.).
  3. **Interpretation** – Converts the raw AST into a final typed AST.

  ### Highlights

  - **Typed Values** in the final AST:
    - Booleans (`true` / `false`)
    - Numbers (floats/ints)
    - Atoms (`:atom`)
    - Strings (quoted `"..."`, triple ```...```, single `'...'`, backtick `` `...` ``, or unquoted)
  - **Block Names** can have multiple attributes (e.g., `config 1, true, "extra"`), stored as `[1, true, "extra"]` in the final AST.
  - **Line/Column** info is included for every key, block, and value in the AST.

  ### Final AST Form

  - **Key-Value** => `{{:key_atom, lineK, colK}, {value, lineV, colV}}`
    - If multiple comma-separated values appear, the value is a list of tuples instead:
      `{{:key_atom, lineK, colK}, [{val1, l1, c1}, {val2, l2, c2}, ... ]}`.
  - **Block** => `{{:block_atom, lineB, colB}, {block_name_or_list, lineN, colN}, block_contents}`

  ## Usage

      iex> SCLParser.parse("foo 42")
      {:ok, [{{:foo, 1, 1}, {42, 1, 5}}]}

      iex> SCLParser.parse("my_block 3.14 { nested true }")
      {:ok,
       [
         {
           {:my_block, 1, 1},
           {3.14, 1, 15},
           [
             {{:nested, 1, 17}, {true, 1, 24}}
           ]
         }
       ]}

  """

  @doc """
  Parses `input` (an SCL document) into an AST with line/col info.

  ## Returns

  - `{:ok, ast}` on success
  - `{:error, {message, line, col}}` on failure
  """
  @spec parse(String.t()) :: {:ok, list()} | {:error, {String.t(), integer(), integer()}}
  def parse(input) when is_binary(input) do
    with {:ok, tokens} <- tokenize(input),
         {:ok, raw_ast} <- parse_tokens(tokens) do
      {:ok, interpret(raw_ast)}
    else
      {:error, {msg, line, col}} ->
        {:error, {msg, line, col}}
    end
  end

  # ============================================================================
  # 1. TOKENIZER
  #
  #    Produces tokens like:
  #      {:boolean, "true", line, col}
  #      {:number, "3.14", line, col}
  #      {:colon_atom, "my_atom", line, col}
  #      {:unquoted_string, "config", line, col}
  #      {:single_quoted_string, "some text", line, col}
  #      {:quoted_string, "some text", line, col}
  #      {:triple_string, "multi\\nline", line, col}
  #      {:backtick_string, "some text", line, col}
  #      {:comma, ",", line, col}
  #      {:lbrace, "{", line, col}
  #      {:rbrace, "}", line, col}
  #      {:newline, "", line, col}
  # ============================================================================
  @doc """
  Tokenizes the input string into a list of tokens (including line/col info).

  Returns `{:ok, tokens}` or `{:error, {message, line, col}}`.
  """
  @spec tokenize(String.t()) :: {:ok, list(tuple())} | {:error, {String.t(), integer(), integer()}}
  def tokenize(input) do
    do_tokenize(input, 1, 1, [])
  end

  # -------------------------------------------------------------------
  # Primary tokenize function, scanning character by character
  # -------------------------------------------------------------------
  defp do_tokenize(<<>>, _line, _col, acc),
    do: {:ok, Enum.reverse(acc)}

  # --- Newlines ------------------------------------------------------
  defp do_tokenize(<<"\r\n", rest::binary>>, line, _col, acc),
    do: do_tokenize(rest, line + 1, 1, [{:newline, "", line, 1} | acc])

  defp do_tokenize(<<"\n", rest::binary>>, line, _col, acc),
    do: do_tokenize(rest, line + 1, 1, [{:newline, "", line, 1} | acc])

  defp do_tokenize(<<"\r", rest::binary>>, line, _col, acc),
    do: do_tokenize(rest, line + 1, 1, [{:newline, "", line, 1} | acc])

  # --- Whitespace => skip --------------------------------------------
  defp do_tokenize(<<c, rest::binary>>, line, col, acc) when c in [?\s, ?\t],
    do: do_tokenize(rest, line, col + 1, acc)

  # --- Comment => skip -----------------------------------------------
  defp do_tokenize(<<"#", rest::binary>>, line, col, acc) do
    case skip_to_line_end(rest) do
      {:found_newline, new_rest, new_line, new_col} ->
        do_tokenize(new_rest, new_line, new_col, [{:newline, "", line, col} | acc])

      :no_newline ->
        {:ok, Enum.reverse(acc)}
    end
  end

  # --- Braces --------------------------------------------------------
  defp do_tokenize(<<"{", rest::binary>>, line, col, acc),
    do: do_tokenize(rest, line, col + 1, [{:lbrace, "{", line, col} | acc])

  defp do_tokenize(<<"}", rest::binary>>, line, col, acc),
    do: do_tokenize(rest, line, col + 1, [{:rbrace, "}", line, col} | acc])

  # --- Comma ---------------------------------------------------------
  defp do_tokenize(<<",", rest::binary>>, line, col, acc),
    do: do_tokenize(rest, line, col + 1, [{:comma, ",", line, col} | acc])

  # --- Triple-backtick -----------------------------------------------
  defp do_tokenize(<<"```", rest::binary>>, line, col, acc) do
    start_line = line
    start_col = col

    case consume_triple_backtick_string(rest, line, col + 3, []) do
      {:ok, txt, new_rest, new_line, new_col} ->
        token = {:triple_string, txt, start_line, start_col}
        do_tokenize(new_rest, new_line, new_col, [token | acc])

      {:error, reason, el, ec} ->
        {:error, {reason, el, ec}}
    end
  end

  # --- Backtick-quoted string ----------------------------------------
  defp do_tokenize(<<"`", rest::binary>>, line, col, acc) do
    start_line = line
    start_col = col

    case consume_backtick_string(rest, line, col + 1, []) do
      {:ok, txt, new_rest, new_line, new_col} ->
        token = {:backtick_string, txt, start_line, start_col}
        do_tokenize(new_rest, new_line, new_col, [token | acc])

      {:error, reason, el, ec} ->
        {:error, {reason, el, ec}}
    end
  end

  # --- Double-quoted string ------------------------------------------
  defp do_tokenize(<<"\"", rest::binary>>, line, col, acc) do
    start_line = line
    start_col = col

    case consume_quoted_string(rest, line, col + 1, []) do
      {:ok, txt, new_rest, new_line, new_col} ->
        token = {:quoted_string, txt, start_line, start_col}
        do_tokenize(new_rest, new_line, new_col, [token | acc])

      {:error, reason, el, ec} ->
        {:error, {reason, el, ec}}
    end
  end

  # --- Single-quoted string ------------------------------------------
  defp do_tokenize(<<"'", rest::binary>>, line, col, acc) do
    start_line = line
    start_col = col

    case consume_single_quoted_string(rest, line, col + 1, []) do
      {:ok, txt, new_rest, new_line, new_col} ->
        token = {:single_quoted_string, txt, start_line, start_col}
        do_tokenize(new_rest, new_line, new_col, [token | acc])

      {:error, reason, el, ec} ->
        {:error, {reason, el, ec}}
    end
  end

  # --- Colon-atom => :something --------------------------------------
  defp do_tokenize(<<":", rest::binary>>, line, col, acc) do
    start_line = line
    start_col = col

    {atom_str, rest2} = consume_name_chars(rest, [])

    if atom_str == "" do
      {:error, {"invalid or empty atom", line, col}}
    else
      token = {:colon_atom, atom_str, start_line, start_col}
      new_col = start_col + 1 + String.length(atom_str)
      do_tokenize(rest2, line, new_col, [token | acc])
    end
  end

  # --- Unquoted string or numeric/boolean word ------------------------
  #
  # Allow *any* character that is not a known separator or wrapper
  # (space, tab, newline, braces, comma, double-quote, comment #,
  # or backtick). Everything else is valid start for an unquoted string.
  defp do_tokenize(<<c, _rest::binary>> = bin, line, col, acc)
       when c not in [?\s, ?\t, ?\r, ?\n, ?{, ?}, ?,, ?", ?#, ?`, ?'] do
    {txt, rest2} = consume_unquoted(bin, [])
    token_type = classify_unquoted(txt)
    token = {token_type, txt, line, col}
    do_tokenize(rest2, line, col + String.length(txt), [token | acc])
  end

  # --- Fallback => error ---------------------------------------------
  # defp do_tokenize(<<ch, _::binary>>, line, col, _acc),
  #   do: {:error, {"unexpected character `#{<<ch>>}`", line, col}}

  # -------------------------------------------------------------------
  # Skips chars until newline or end of file (for comments)
  # -------------------------------------------------------------------
  defp skip_to_line_end(<<>>), do: :no_newline
  defp skip_to_line_end(<<"\r\n", rest::binary>>), do: {:found_newline, rest, 0, 0}
  defp skip_to_line_end(<<"\n", rest::binary>>), do: {:found_newline, rest, 0, 0}
  defp skip_to_line_end(<<"\r", rest::binary>>), do: {:found_newline, rest, 0, 0}
  defp skip_to_line_end(<<_c, rest::binary>>), do: skip_to_line_end(rest)

  # -------------------------------------------------------------------
  # Consumes triple-backtick until next ``` or end
  # -------------------------------------------------------------------
  defp consume_triple_backtick_string(<<"```", rest::binary>>, line, col, acc) do
    text = IO.iodata_to_binary(Enum.reverse(acc))
    {:ok, text, rest, line, col + 3}
  end

  defp consume_triple_backtick_string(<<>>, line, col, _acc),
    do: {:error, "unterminated triple-backtick string", line, col}

  defp consume_triple_backtick_string(<<"\r\n", rest::binary>>, line, _col, acc),
    do: consume_triple_backtick_string(rest, line + 1, 1, [?\n | acc])

  defp consume_triple_backtick_string(<<"\n", rest::binary>>, line, _col, acc),
    do: consume_triple_backtick_string(rest, line + 1, 1, [?\n | acc])

  defp consume_triple_backtick_string(<<"\r", rest::binary>>, line, _col, acc),
    do: consume_triple_backtick_string(rest, line + 1, 1, [?\n | acc])

  defp consume_triple_backtick_string(<<c, rest::binary>>, line, col, acc),
    do: consume_triple_backtick_string(rest, line, col + 1, [c | acc])

  # -------------------------------------------------------------------
  # Consumes a backtick-quoted string, handling escapes
  # -------------------------------------------------------------------
  defp consume_backtick_string(<<"`", rest::binary>>, line, col, acc) do
    txt = IO.iodata_to_binary(Enum.reverse(acc))
    {:ok, txt, rest, line, col + 1}
  end

  defp consume_backtick_string(<<"\\`", rest::binary>>, line, col, acc),
    do: consume_backtick_string(rest, line, col + 2, [?` | acc])

  defp consume_backtick_string(<<>>, line, col, _acc),
    do: {:error, "unterminated backtick-quoted string", line, col}

  defp consume_backtick_string(<<"\r\n", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in backtick-quoted string", line, col}

  defp consume_backtick_string(<<"\n", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in backtick-quoted string", line, col}

  defp consume_backtick_string(<<"\r", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in backtick-quoted string", line, col}

  defp consume_backtick_string(<<c, rest::binary>>, line, col, acc),
    do: consume_backtick_string(rest, line, col + 1, [c | acc])

  # -------------------------------------------------------------------
  # Consumes a double-quoted string, handling escapes
  # -------------------------------------------------------------------
  defp consume_quoted_string(<<"\"", rest::binary>>, line, col, acc) do
    txt = IO.iodata_to_binary(Enum.reverse(acc))
    {:ok, txt, rest, line, col + 1}
  end

  defp consume_quoted_string(<<"\\\"", rest::binary>>, line, col, acc),
    do: consume_quoted_string(rest, line, col + 2, [?" | acc])

  defp consume_quoted_string(<<>>, line, col, _acc),
    do: {:error, "unterminated double-quoted string", line, col}

  defp consume_quoted_string(<<"\r\n", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in double-quoted string", line, col}

  defp consume_quoted_string(<<"\n", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in double-quoted string", line, col}

  defp consume_quoted_string(<<"\r", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in double-quoted string", line, col}

  defp consume_quoted_string(<<c, rest::binary>>, line, col, acc),
    do: consume_quoted_string(rest, line, col + 1, [c | acc])

  # -------------------------------------------------------------------
  # Consumes a single-quoted string, handling escapes
  # -------------------------------------------------------------------
  defp consume_single_quoted_string(<<"'", rest::binary>>, line, col, acc) do
    txt = IO.iodata_to_binary(Enum.reverse(acc))
    {:ok, txt, rest, line, col + 1}
  end

  defp consume_single_quoted_string(<<"\\'", rest::binary>>, line, col, acc),
    do: consume_single_quoted_string(rest, line, col + 2, [?' | acc])

  defp consume_single_quoted_string(<<>>, line, col, _acc),
    do: {:error, "unterminated single-quoted string", line, col}

  defp consume_single_quoted_string(<<"\r\n", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in single-quoted string", line, col}

  defp consume_single_quoted_string(<<"\n", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in single-quoted string", line, col}

  defp consume_single_quoted_string(<<"\r", _rest::binary>>, line, col, _acc),
    do: {:error, "unexpected newline in single-quoted string", line, col}

  defp consume_single_quoted_string(<<c, rest::binary>>, line, col, acc),
    do: consume_single_quoted_string(rest, line, col + 1, [c | acc])

  # -------------------------------------------------------------------
  # Consumes valid name chars after a colon (for atoms)
  # -------------------------------------------------------------------
  @name_regex ~r/^[a-z][a-z0-9_]*$/
  defp consume_name_chars(<<c, rest::binary>>, acc) when c in ?a..?z or c in ?0..?9 or c == ?_ do
    consume_name_chars(rest, [c | acc])
  end

  defp consume_name_chars(rest, acc) do
    {IO.iodata_to_binary(Enum.reverse(acc)), rest}
  end

  # -------------------------------------------------------------------
  # Consumes unquoted text until whitespace or delimiter
  # -------------------------------------------------------------------
  defp consume_unquoted(<<>>, acc),
    do: {IO.iodata_to_binary(Enum.reverse(acc)), <<>>}

  defp consume_unquoted(<<"```", _rest::binary>> = remain, acc),
    do: {IO.iodata_to_binary(Enum.reverse(acc)), remain}

  defp consume_unquoted(<<c, _rest::binary>> = remain, acc)
       when c in [?\s, ?\t, ?\r, ?\n, ?{, ?}, ?,, ?", ?#, ?'] do
    {IO.iodata_to_binary(Enum.reverse(acc)), remain}
  end

  defp consume_unquoted(<<ch, rest::binary>>, acc),
    do: consume_unquoted(rest, [ch | acc])

  # -------------------------------------------------------------------
  # Classifies an unquoted string into a token type
  # -------------------------------------------------------------------
  @int_regex ~r/^[0-9]+$/
  @float_regex ~r/^[0-9]+\.[0-9]+$/

  defp classify_unquoted("true"), do: :boolean
  defp classify_unquoted("false"), do: :boolean

  defp classify_unquoted(str) do
    cond do
      Regex.match?(@int_regex, str) -> :number
      Regex.match?(@float_regex, str) -> :number
      true -> :unquoted_string
    end
  end

  # ============================================================================
  # 2. PARSER: from tokens => raw AST
  # ============================================================================
  @doc """
  Converts a list of `tokens` into a raw internal AST that retains
  line/col info.

  Returns `{:ok, raw_ast}` or `{:error, {message, line, col}}`.
  """
  @spec parse_tokens(list(tuple())) :: {:ok, list()} | {:error, {String.t(), integer(), integer()}}
  def parse_tokens(tokens),
    do: parse_root_statements(tokens, [])

  # -------------------------------------------------------------------
  # Parse multiple root-level statements in a head/tail fashion
  # -------------------------------------------------------------------
  defp parse_root_statements([], acc),
    do: {:ok, Enum.reverse(acc)}

  defp parse_root_statements([{:newline, "", _line, _col} | rest], acc) do
    parse_root_statements(rest, acc)
  end

  defp parse_root_statements([{:rbrace, "}", line, col} | _] = _tokens, _acc) do
    {:error, {"unexpected `}` at root level", line, col}}
  end

  defp parse_root_statements([token | rest], acc) do
    with {:ok, stmt, new_rest} <- parse_root_statement([token | rest]) do
      parse_root_statements(new_rest, [stmt | acc])
    end
  end

  # -------------------------------------------------------------------
  # Root statement => either a block or a key-value line
  # -------------------------------------------------------------------
  defp parse_root_statement([token | _] = tokens) do
    case consume_name_token(tokens) do
      :no_name ->
        {:error, unexpected_token(token, "block or key name")}

      {:ok, {name_atom, nline, ncol}, remaining} ->
        parse_rest_of_root_stmt(remaining, {name_atom, nline, ncol})
    end
  end

  defp parse_rest_of_root_stmt(tokens, {name_atom, nline, ncol}) do
    # We call a wrapper function (without default) that calls parse_comma_enforced_values/3
    case parse_comma_enforced_values(tokens, []) do
      {:ok, vals, {:block, next_tokens}, _just_saw_value} ->
        # next token is '{'
        [{:lbrace, "{", block_line, block_col} | rest] = next_tokens
        parse_block_body(rest, {name_atom, nline, ncol}, vals, block_line, block_col)

      {:ok, vals, {:end_of_statement, new_rest}, _just_saw_value} ->
        {:ok, {:kv_line, {name_atom, nline, ncol}, vals}, new_rest}

      {:error, reason} ->
        {:error, reason}
    end
  end

  # -------------------------------------------------------------------
  # Parse the body of a block after seeing an opening '{'
  # -------------------------------------------------------------------
  defp parse_block_body(tokens, {block_atom, bline, bcol}, name_vals, brace_line, brace_col) do
    block_name_info =
      case name_vals do
        [] ->
          {"", brace_line, brace_col}

        [{tok_type, txt, l, c}] ->
          {{tok_type, txt, l, c}, brace_line, brace_col}

        multi ->
          {multi, brace_line, brace_col}
      end

    parse_block_statements(tokens, [], {block_atom, bline, bcol}, block_name_info)
  end

  # -------------------------------------------------------------------
  # Parse statements inside a block until we see '}'
  # -------------------------------------------------------------------
  defp parse_block_statements([], _acc, {block_atom, lineA, colA}, _block_name_info) do
    {:error,
     {"unterminated block `#{block_atom}` started on line #{lineA}, column #{colA} (missing `}`)",
      lineA, colA}}
  end

  defp parse_block_statements(
         [{:rbrace, "}", _rl, _rc} | rest],
         acc,
         {block_atom, lineA, colA},
         block_name_info
       ) do
    {:ok, {:block, {block_atom, lineA, colA}, block_name_info, Enum.reverse(acc)}, rest}
  end

  defp parse_block_statements([{:newline, "", _l, _c} | rest], acc, block_info, block_name_info) do
    parse_block_statements(rest, acc, block_info, block_name_info)
  end

  defp parse_block_statements([token | rest], acc, block_info, block_name_info) do
    with {:ok, stmt, new_rest} <- parse_block_statement([token | rest], block_info) do
      parse_block_statements(new_rest, [stmt | acc], block_info, block_name_info)
    end
  end

  # -------------------------------------------------------------------
  # A statement inside a block => either nested block or key-value line
  # -------------------------------------------------------------------
  defp parse_block_statement([token | _] = tokens, _block_info) do
    case consume_name_token(tokens) do
      :no_name ->
        {:error, unexpected_token(token, "block statement name")}

      {:ok, {stmt_name, sline, scol}, remaining} ->
        parse_block_after_name(stmt_name, sline, scol, remaining)
    end
  end

  defp parse_block_after_name(stmt_name, sline, scol, remaining) do
    case parse_comma_enforced_values(remaining, []) do
      {:ok, vals, {:block, after_vals}, _} ->
        parse_after_block_val(stmt_name, sline, scol, vals, after_vals)

      {:ok, vals, {:end_of_statement, new_rest}, _} ->
        {:ok, {:kv_line, {stmt_name, sline, scol}, vals}, new_rest}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp parse_after_block_val(
         stmt_name,
         sline,
         scol,
         vals,
         [{:lbrace, "{", block_line, block_col} | rest_of_block]
       ) do
    parse_block_body(
      rest_of_block,
      {stmt_name, sline, scol},
      vals,
      block_line,
      block_col
    )
  end

  # -------------------------------------------------------------------
  # PARSE COMMA-ENFORCED VALUES
  #
  # We define a small wrapper function to avoid the compiler warning
  # about default arguments in multiple clauses.
  # -------------------------------------------------------------------

  # This first clause has no default. It simply calls the 3-arg version
  # with just_saw_value = false
  defp parse_comma_enforced_values(tokens, acc),
    do: parse_comma_enforced_values(tokens, acc, false)

  # Main function with 3 args (no default).
  #
  # Returns:
  #   {:ok, values, {:block, tokens_after_block}, just_saw_value}
  #   {:ok, values, {:end_of_statement, tokens_after}, just_saw_value}
  #   {:error, reason}
  defp parse_comma_enforced_values([], acc, _just_saw_value) do
    # No more tokens => end_of_statement
    {:ok, Enum.reverse(acc), {:end_of_statement, []}, false}
  end

  defp parse_comma_enforced_values([{:lbrace, "{", l, c} | rest], acc, just_saw_value) do
    # We found a block start
    {:ok, Enum.reverse(acc), {:block, [{:lbrace, "{", l, c} | rest]}, just_saw_value}
  end

  defp parse_comma_enforced_values([{:newline, "", _l, _c} = nl | rest], acc, just_saw_value) do
    # newline => end_of_statement
    {:ok, Enum.reverse(acc), {:end_of_statement, [nl | rest]}, just_saw_value}
  end

  defp parse_comma_enforced_values([{:rbrace, "}", _l, _c} = rb | rest], acc, just_saw_value) do
    # rbrace => end_of_statement
    {:ok, Enum.reverse(acc), {:end_of_statement, [rb | rest]}, just_saw_value}
  end

  defp parse_comma_enforced_values([{:comma, ",", _l, _c} | rest], acc, _just_saw_value) do
    # reset just_saw_value => expect another value
    parse_comma_enforced_values(rest, acc, false)
  end

  defp parse_comma_enforced_values([token | _rest], _acc, true)
       when elem(token, 0) in [
              :boolean,
              :number,
              :quoted_string,
              :triple_string,
              :backtick_string,
              :single_quoted_string,
              :unquoted_string,
              :colon_atom
            ] do
    # We already saw a value and do not see a comma => error
    {_tok_type, text, line, col} = token
    {:error, {"expected comma before #{inspect(text)}", line, col}}
  end

  defp parse_comma_enforced_values([token | rest], acc, false)
       when elem(token, 0) in [
              :boolean,
              :number,
              :quoted_string,
              :triple_string,
              :backtick_string,
              :single_quoted_string,
              :unquoted_string,
              :colon_atom
            ] do
    {tok_type, text, line, col} = token
    parse_comma_enforced_values(rest, [{tok_type, text, line, col} | acc], true)
  end

  # -------------------------------------------------------------------
  # Tries to read an unquoted_string token that matches @name_regex
  # -------------------------------------------------------------------
  defp consume_name_token([{:unquoted_string, text, line, col} | rest]) do
    if Regex.match?(@name_regex, text) do
      {:ok, {String.to_atom(text), line, col}, rest}
    else
      :no_name
    end
  end

  defp consume_name_token(_), do: :no_name

  # ============================================================================
  # 3. INTERPRETER: raw AST => final typed AST
  # ============================================================================
  @doc """
  Converts the raw AST from `parse_tokens/1` into a final typed AST with
  booleans, numbers, atoms, etc. in the correct Elixir data types.
  """
  @spec interpret(list()) :: list()
  def interpret(raw_ast),
    do: Enum.map(raw_ast, &interpret_stmt/1)

  # -------------------------------------------------------------------
  # Interpret top-level statements: either :kv_line or :block
  # -------------------------------------------------------------------
  defp interpret_stmt({:kv_line, {key_atom, kline, kcol}, values}),
    do: interpret_kv_line({key_atom, kline, kcol}, values)

  defp interpret_stmt(
         {:block, {block_atom, b_line, b_col}, {block_name_raw, bn_line, bn_col}, subitems}
       ) do
    interpreted_name = interpret_block_name(block_name_raw)
    interpreted_sub = Enum.map(subitems, &interpret_stmt/1)

    {
      {block_atom, b_line, b_col},
      {interpreted_name, bn_line, bn_col},
      interpreted_sub
    }
  end

  # -------------------------------------------------------------------
  # Interpret a key-value line, possibly with multiple comma values
  # -------------------------------------------------------------------
  defp interpret_kv_line({key_atom, kline, kcol}, []) do
    {{key_atom, kline, kcol}, {nil, kline, kcol}}
  end

  defp interpret_kv_line({key_atom, kline, kcol}, [{tok_type, txt, vline, vcol}]) do
    value = convert_value(tok_type, txt)
    {{key_atom, kline, kcol}, {value, vline, vcol}}
  end

  defp interpret_kv_line({key_atom, kline, kcol}, multi) do
    list_vals =
      Enum.map(multi, fn {tok_type, txt, l, c} ->
        {convert_value(tok_type, txt), l, c}
      end)

    {{key_atom, kline, kcol}, list_vals}
  end

  # -------------------------------------------------------------------
  # Interpret the block name (empty, single token, or list of tokens)
  # -------------------------------------------------------------------
  defp interpret_block_name("") do
    ""
  end

  defp interpret_block_name({tok_type, txt, _l, _c}),
    do: convert_value(tok_type, txt)

  defp interpret_block_name(multi) when is_list(multi) do
    Enum.map(multi, fn {tok_type, txt, _l, _c} ->
      convert_value(tok_type, txt)
    end)
  end

  # -------------------------------------------------------------------
  # Convert token types to final typed values
  # -------------------------------------------------------------------
  defp convert_value(:boolean, "true"), do: true
  defp convert_value(:boolean, "false"), do: false

  defp convert_value(:number, txt) do
    if String.contains?(txt, ".") do
      String.to_float(txt)
    else
      String.to_integer(txt)
    end
  end

  defp convert_value(:colon_atom, txt), do: String.to_atom(txt)
  defp convert_value(:quoted_string, txt), do: txt
  defp convert_value(:single_quoted_string, txt), do: txt
  defp convert_value(:triple_string, txt), do: txt
  defp convert_value(:unquoted_string, txt), do: txt
  defp convert_value(:backtick_string, txt), do: txt

  # ============================================================================
  # ERROR HANDLING
  # ============================================================================
  defp unexpected_token({tok_type, text, line, col}, expected) do
    {"unexpected #{String.replace("#{tok_type}", "_", " ")} #{inspect(text)}; expected #{expected}",
     line, col}
  end
end
