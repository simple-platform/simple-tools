defmodule SCLParser.ExpressionParserTest do
  use ExUnit.Case, async: true

  doctest SCLParser.ExpressionParser

  alias SCLParser.ExpressionParser, as: Parser

  describe "Positive Parsing Scenarios" do
    test "single function call with single string param" do
      assert {:ok, [%{fn: "var", params: ["foo"]}]} =
               Parser.parse("$var('foo')")
    end

    test "single function call with double quoted string param" do
      assert {:ok, [%{fn: "var", params: ["foo bar"]}]} =
               Parser.parse("$var(\"foo bar\")")
    end

    test "single function call with backtick quoted string param" do
      assert {:ok, [%{fn: "var", params: ["foo `bar`"]}]} =
               Parser.parse("$var(`foo \\`bar\\``)")
    end

    test "single function call with integer param" do
      assert {:ok, [%{fn: "num", params: [123]}]} =
               Parser.parse("$num(123)")
    end

    test "single function call with float param" do
      assert {:ok, [%{fn: "num", params: [123.45]}]} =
               Parser.parse("$num(123.45)")
    end

    test "single function call with boolean true param" do
      assert {:ok, [%{fn: "bool", params: [true]}]} =
               Parser.parse("$bool(true)")
    end

    test "single function call with boolean false param" do
      assert {:ok, [%{fn: "bool", params: [false]}]} =
               Parser.parse("$bool(false)")
    end

    test "single function call with no params" do
      assert {:ok, [%{fn: "get", params: []}]} =
               Parser.parse("$get()")
    end

    test "single function call with multiple params (string, number, boolean)" do
      assert {:ok, [%{fn: "multi", params: ["hello", 42, true]}]} =
               Parser.parse("$multi('hello', 42, true)")
    end

    test "single function call with multiple params (different quotes)" do
      assert {:ok, [%{fn: "quotes", params: ["double", "single", "backtick"]}]} =
               Parser.parse("$quotes(\"double\", 'single', `backtick`)")
    end

    test "single function call with JQ string param" do
      assert {:ok, [%{fn: "jq", params: [".properties[] | select(.key == \"test\") | .id"]}]} =
               Parser.parse("$jq('.properties[] | select(.key == \"test\") | .id')")
    end

    test "piped function calls" do
      assert {:ok, [%{fn: "var", params: ["input"]}, %{fn: "upper", params: []}]} =
               Parser.parse("$var('input') |> $upper()")
    end

    test "piped function calls with params" do
      assert {:ok, [%{fn: "num", params: [10]}, %{fn: "add", params: [5]}]} =
               Parser.parse("$num(10) |> $add(5)")
    end

    test "piped function calls - three stages" do
      assert {:ok,
              [
                %{fn: "get", params: []},
                %{fn: "filter", params: [":active"]},
                %{fn: "count", params: []}
              ]} =
               Parser.parse("$get() |> $filter(':active') |> $count()")
    end

    test "string param with commas" do
      assert {:ok, [%{fn: "csv", params: ["a,b,c"]}]} =
               Parser.parse("$csv('a,b,c')")
    end

    test "string param with parentheses" do
      assert {:ok, [%{fn: "msg", params: ["hello (world)"]}]} =
               Parser.parse("$msg('hello (world)')")
    end

    test "string param with dollar sign" do
      assert {:ok, [%{fn: "money", params: ["$100"]}]} =
               Parser.parse("$money('$100')")
    end

    test "string param with pipe" do
      assert {:ok, [%{fn: "txt", params: ["a |> b"]}]} =
               Parser.parse("$txt('a |> b')")
    end

    test "string param with escaped single quote" do
      assert {:ok, [%{fn: "str", params: ["it's escaped"]}]} =
               Parser.parse("$str('it\\'s escaped')")
    end

    test "string param with escaped double quote" do
      assert {:ok, [%{fn: "str", params: ["say \"hi\""]}]} =
               Parser.parse(~S|$str("say \"hi\"")|)
    end

    test "string param with escaped backtick" do
      assert {:ok, [%{fn: "str", params: ["code `here`"]}]} =
               Parser.parse("$str(`code \\`here\\``)")
    end

    test "string param with escaped newline" do
      assert {:ok, [%{fn: "str", params: ["line1\nline2"]}]} =
               Parser.parse("$str('line1\\nline2')")
    end

    test "string param with escaped tab" do
      assert {:ok, [%{fn: "str", params: ["col1\tcol2"]}]} =
               Parser.parse("$str('col1\\tcol2')")
    end

    test "string param with escaped backslash" do
      assert {:ok, [%{fn: "str", params: ["path\\to\\file"]}]} =
               Parser.parse("$str('path\\\\to\\\\file')")
    end

    test "function name with underscores" do
      assert {:ok, [%{fn: "my_func", params: []}]} =
               Parser.parse("$my_func()")
    end

    test "function name with numbers" do
      assert {:ok, [%{fn: "func123", params: []}]} =
               Parser.parse("$func123()")
    end

    test "whitespace around operators and params" do
      assert {:ok, [%{fn: "var", params: ["input"]}, %{fn: "add", params: [1]}]} =
               Parser.parse("  $var ( 'input' )  |>  $add ( 1 )  ")
    end

    test "identifier param (like JQ)" do
      assert {:ok, [%{fn: "jq", params: [".foo.bar"]}]} =
               Parser.parse("$jq(.foo.bar)")
    end

    test "identifier param with other types" do
      assert {:ok, [%{fn: "mixed", params: [".foo", 1, true]}]} =
               Parser.parse("$mixed(.foo, 1, true)")
    end

    test "complex identifier param (like JQ filter)" do
      assert {:ok, [%{fn: "jq", params: ["[?(@.name==\"test\")].id"]}]} =
               Parser.parse("$jq('[?(@.name==\"test\")].id')")
    end

    test "empty single-quoted string param" do
      assert {:ok, [%{fn: "str", params: [""]}]} = Parser.parse("$str('')")
    end

    test "empty double-quoted string param" do
      assert {:ok, [%{fn: "str", params: [""]}]} = Parser.parse("$str(\"\")")
    end

    test "empty backtick-quoted string param" do
      assert {:ok, [%{fn: "str", params: [""]}]} = Parser.parse("$str(``)")
    end

    test "identifier starting with underscore" do
      assert {:ok, [%{fn: "get", params: ["_internal"]}]} = Parser.parse("$get(_internal)")
    end

    test "identifier starting with dot" do
      assert {:ok, [%{fn: "get", params: [".path"]}]} = Parser.parse("$get(.path)")
    end

    test "whitespace variations (CRLF, CR)" do
      assert {:ok, [%{fn: "var", params: ["a"]}, %{fn: "trim", params: []}]} =
               Parser.parse("  $var('a') \r\n |> \r $trim()  ")
    end

    test "whitespace after comma, before param" do
      assert {:ok, [%{fn: "add", params: [1, 2]}]} = Parser.parse("$add(1,  \t 2)")
    end

    test "whitespace before closing parenthesis" do
      assert {:ok, [%{fn: "add", params: [1, 2]}]} = Parser.parse("$add(1, 2 \t )")
    end

    test "whitespace before pipe" do
      assert {:ok, [%{fn: "get", params: []}, %{fn: "trim", params: []}]} =
               Parser.parse("$get() \t |> $trim()")
    end

    # --- Nested Expression Tests ---

    test "simple nested expression as parameter" do
      input = "$outer($inner('hello'))"
      expected = {:ok, [%{fn: "outer", params: [%{fn: "inner", params: ["hello"]}]}]}
      assert expected == Parser.parse(input)
    end

    test "nested expression with multiple outer parameters" do
      input = "$outer(1, $inner(true), 'end')"
      expected = {:ok, [%{fn: "outer", params: [1, %{fn: "inner", params: [true]}, "end"]}]}
      assert expected == Parser.parse(input)
    end

    test "multiple nested expressions as parameters" do
      input = "$sum($var('a'), $var('b'))"

      expected =
        {:ok, [%{fn: "sum", params: [%{fn: "var", params: ["a"]}, %{fn: "var", params: ["b"]}]}]}

      assert expected == Parser.parse(input)
    end

    test "nested expression piped to another function" do
      input = "$sum($const(5), $const(3)) |> $mul(10)"

      expected =
        {:ok,
         [
           %{fn: "sum", params: [%{fn: "const", params: [5]}, %{fn: "const", params: [3]}]},
           %{fn: "mul", params: [10]}
         ]}

      assert expected == Parser.parse(input)
    end

    test "deeply nested expression" do
      input = "$a($b($c(1)))"
      expected = {:ok, [%{fn: "a", params: [%{fn: "b", params: [%{fn: "c", params: [1]}]}]}]}
      assert expected == Parser.parse(input)
    end

    test "piped call inside nested expression parameter" do
      input = "$outer($inner(1) |> $add(2))"

      # Note: The current implementation parses the nested part FIRST.
      # It consumes `$inner(1)` as the parameter, then the `|>` causes an error
      # because it's not expected after the outer function call's parameters.
      # To support pipes *inside* parameters would require significant changes.
      # Let's assert the current expected error behavior.
      assert {:error, "Expected ',' or ')' after parameter in 'outer', got pipe: \"|>\""} ==
               Parser.parse(input)
    end
  end

  describe "Negative Parsing Scenarios" do
    test "input does not start with $" do
      assert {:error, "Expression must start with '$'"} = Parser.parse("var('foo')")
    end

    test "empty input" do
      assert {:error, "Expression cannot be empty"} = Parser.parse("")
    end

    test "only whitespace" do
      assert {:error, "Expression cannot be empty"} = Parser.parse("   ")
    end

    test "missing function name" do
      assert {:error, "Expected function name after '$', got lparen: \"(\""} = Parser.parse("$()")
    end

    test "invalid function name start" do
      assert {:error, "Expected function name after '$', got number: 123"} =
               Parser.parse("$123()")
    end

    test "missing opening parenthesis" do
      assert {:error, "Expected '(' after function name 'var', got end of input"} =
               Parser.parse("$var")
    end

    test "missing closing parenthesis" do
      assert {:error, "Unterminated parameter list (missing ')') after parameter in 'var'"} =
               Parser.parse("$var('foo'")
    end

    test "unterminated single quote string" do
      assert {:error, "Unterminated single-quoted string in parameter list"} =
               Parser.parse("$var('foo")
    end

    test "unterminated double quote string" do
      assert {:error, "Unterminated double-quoted string in parameter list"} =
               Parser.parse("$var(\"foo")
    end

    test "unterminated backtick quote string" do
      assert {:error, "Unterminated backtick-quoted string in parameter list"} =
               Parser.parse("$var(`foo")
    end

    test "missing comma between params" do
      assert {:error, "Expected ',' or ')' after parameter in 'multi', got number: 42"} =
               Parser.parse("$multi('hello' 42)")
    end

    test "trailing comma" do
      assert {:error, "Unexpected ')' after comma in 'multi'"} = Parser.parse("$multi('hello', )")
    end

    test "comma before first param" do
      assert {:error, "Unexpected comma before first parameter in 'multi'"} =
               Parser.parse("$multi(, 'hello')")
    end

    test "unexpected token in param list" do
      # Assuming '|' is invalid within param list unless quoted
      assert {:error, "Invalid character '|' in expression"} = Parser.parse("$func(a | b)")
    end

    test "trailing pipe" do
      assert {:error, "Expected '$' after '|>'"} = Parser.parse("$var() |> ")
    end

    test "pipe without following $" do
      assert {:error, "Expected '$' after '|>', got token: add"} = Parser.parse("$var() |> add()")
    end

    test "invalid character in expression" do
      assert {:error, "Invalid character '@' in expression"} = Parser.parse("$var(@)")
    end

    test "raw newline within single quotes" do
      input = "$var('line1\nline2')"

      assert {:error,
              "Raw newline not allowed in single-quoted parameter string in parameter list"} =
               Parser.parse(input)
    end

    test "raw newline within double quotes" do
      input = "$var(\"line1\nline2\")"

      assert {:error,
              "Raw newline not allowed in double-quoted parameter string in parameter list"} =
               Parser.parse(input)
    end

    test "raw newline within backticks" do
      input = "$var(`line1\nline2`)"

      assert {:error, "Newline not allowed in backtick-quoted parameter string in parameter list"} =
               Parser.parse(input)
    end

    test "unexpected closing paren" do
      assert {:error, "Expected '|>' or end of expression after function call, got token: )"} =
               Parser.parse("$var())")
    end

    test "unexpected token after function call" do
      assert {:error, "Expected '|>' or end of expression after function call, got token: extra"} =
               Parser.parse("$var() extra")
    end

    test "pipe immediately after $" do
      assert {:error, "Expected function name after '$', got pipe: \"|>\""} =
               Parser.parse("$ |> $next()")
    end

    test "missing param after comma" do
      assert {:error, "Unexpected ')' after comma in 'func'"} = Parser.parse("$func(a, )")
    end

    test "double comma" do
      assert {:error, "Unexpected comma after comma in 'func'"} = Parser.parse("$func(a, , b)")
    end

    test "multiple adjacent pipes" do
      assert {:error, "Invalid character '|' in expression"} = Parser.parse("$var() ||> $add(1)")
    end

    test "expression starting with pipe" do
      assert {:error, "Expression must start with '$'"} = Parser.parse("|> $var()")
    end

    test "invalid char - curly brace" do
      assert {:error, "Invalid character '{' in expression"} = Parser.parse("$var({")
    end

    test "unexpected token after pipe" do
      assert {:error, "Invalid character '{' in expression"} = Parser.parse("$var() |> {")
    end

    test "EOF after $" do
      assert {:error, "Expected function name after '$', got end of input"} = Parser.parse("$")
    end

    test "EOF after function name" do
      assert {:error, "Expected '(' after function name 'abc', got end of input"} =
               Parser.parse("$abc")
    end

    test "EOF after opening parenthesis" do
      assert {:error, "Unterminated parameter list (missing ')') for function 'abc'"} =
               Parser.parse("$abc(")
    end

    test "EOF after parameter (missing comma or closing paren)" do
      assert {:error, "Unterminated parameter list (missing ')') after parameter in 'abc'"} =
               Parser.parse("$abc('param1'")
    end

    test "EOF after comma" do
      assert {:error, "Expected parameter after comma in 'abc', got end of input"} =
               Parser.parse("$abc('param1', ")
    end

    test "invalid token after function name (expecting paren)" do
      assert {:error, "Expected '(' after function name 'abc', got comma: \",\""} =
               Parser.parse("$abc,")
    end

    test "invalid token after parameters (expecting closing paren)" do
      assert {:error, "Expected parameter after comma in 'abc', got end of input"} =
               Parser.parse("$abc('p1', 'p2' , ")
    end

    test "invalid token type passed as parameter" do
      # Simulate an internal error where a non-parameter token reaches parse_one_param
      # This requires crafting tokens manually, so we test the state indirectly
      # Example: $func(|>) - tokenizer produces pipe token
      assert {:error, "Invalid token in parameter list for 'func': pipe: \"|>\""} =
               Parser.parse("$func(|>)")
    end

    test "Raw newline in single quotes (re-verify)" do
      input = "$var('line1\nline2')"

      assert {:error,
              "Raw newline not allowed in single-quoted parameter string in parameter list"} =
               Parser.parse(input)
    end

    test "Invalid char after parameter (expecting comma or paren)" do
      assert {:error, "Expected ',' or ')' after parameter in 'func', got lparen: \"(\""} =
               Parser.parse("$func('a' (")
    end

    test "EOF inside single-quoted string" do
      assert {:error, "Unterminated single-quoted string in parameter list"} =
               Parser.parse("$func('")
    end

    test "EOF inside double-quoted string" do
      assert {:error, "Unterminated double-quoted string in parameter list"} =
               Parser.parse("$func(\"")
    end

    test "EOF inside backtick-quoted string" do
      assert {:error, "Unterminated backtick-quoted string in parameter list"} =
               Parser.parse("$func(`")
    end

    test "Invalid char - closing square bracket" do
      assert {:error, "Invalid character ']' in expression"} = Parser.parse("$var(])")
    end

    test "Invalid char - semicolon" do
      assert {:error, "Invalid character ';' in expression"} = Parser.parse("$var(;")
    end

    test "Two function calls without pipe" do
      assert {:error, "Expected '|>' or end of expression after function call, got token: $"} =
               Parser.parse("$var() $add()")
    end

    test "EOF after escape in single quote" do
      assert {:error, "Unterminated single-quoted string in parameter list"} =
               Parser.parse("$func('abc\\")
    end

    test "EOF after escape in double quote" do
      assert {:error, "Unterminated double-quoted string in parameter list"} =
               Parser.parse("$func(\"abc\\")
    end

    test "EOF after escape in backtick quote" do
      assert {:error, "Unterminated backtick-quoted string in parameter list"} =
               Parser.parse("$func(`abc\\")
    end

    test "invalid token immediately after params (expecting closing paren)" do
      assert {:error, "Expected '|>' or end of expression after function call, got token: ,"} =
               Parser.parse("$func('a') , ")
    end

    test "Raw newline inside single quotes" do
      assert {:error,
              "Raw newline not allowed in single-quoted parameter string in parameter list"} =
               Parser.parse("$var('line1\nline2')")
    end

    test "Raw newline inside double quotes" do
      assert {:error,
              "Raw newline not allowed in double-quoted parameter string in parameter list"} =
               Parser.parse("$var(\"line1\nline2\")")
    end

    test "Raw newline inside backticks" do
      assert {:error, "Newline not allowed in backtick-quoted parameter string in parameter list"} =
               Parser.parse("$var(`line1\nline2`)")
    end

    test "error within nested expression" do
      input = "$outer($inner('unclosed)"

      # The error occurs during tokenization of the inner string, before recursive parsing adds context.
      assert {:error, "Unterminated single-quoted string in parameter list"} ==
               Parser.parse(input)
    end

    test "missing closing paren for nested expression" do
      input = "$outer($inner(1)"
      # Assert the error originates from the inner parse and is prefixed by the outer context
      assert {:error, "Unterminated parameter list (missing ')') after parameter in 'outer'"} ==
               Parser.parse(input)
    end
  end
end
