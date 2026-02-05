defmodule SCLParserTest do
  use ExUnit.Case, async: true

  doctest SCLParser

  alias SCLParser, as: Parser

  # ============================================================================
  # Positive Parsing Scenarios
  # ============================================================================
  # These tests cover valid SCL input that should produce a successful AST.
  # We test various combinations of:
  # - Root level key-values
  # - Nested blocks
  # - Comments (inline and full line)
  # - Quoted vs unquoted strings
  # - Basic data types (boolean, integer, float)
  # ============================================================================
  describe "Positive Parsing Scenarios" do
    test "empty string returns empty AST" do
      assert {:ok, []} = Parser.parse("")
    end

    test "only whitespace returns empty AST" do
      input = "   \t   \r\n  "
      assert {:ok, []} = Parser.parse(input)
    end

    test "only newlines returns empty AST" do
      input = "\n\n\n"
      assert {:ok, []} = Parser.parse(input)
    end

    test "single root key-value (unquoted)" do
      input = "foo bar\n"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"bar", _, _}}] = ast
    end

    test "root key-value with trailing whitespace" do
      input = "foo bar    \n  "
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"bar", _, _}}] = ast
    end

    test "root key-value with boolean" do
      input = "active true\n"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:active, _, _}, {true, _, _}}] = ast
    end

    test "root key-value with float" do
      input = "price 123.45\n"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:price, _, _}, {123.45, _, _}}] = ast
    end

    test "root key-value with colon atom" do
      input = "kind :test_atom"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:kind, _, _}, {:test_atom, _, _}}] = ast
    end

    test "multiple root lines with mixed data" do
      input = """
      foo bar
      version 2
      enabled false
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {{:foo, _, _}, {"bar", _, _}},
               {{:version, _, _}, {2, _, _}},
               {{:enabled, _, _}, {false, _, _}}
             ] = ast
    end

    test "root line with multiple unquoted values" do
      input = "foo bar, baz"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, [{"bar", _, _}, {"baz", _, _}]}] = ast
    end

    test "root line with multiple quoted values" do
      input = ~S|languages "elixir", "erlang"|
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:languages, _, _}, [{"elixir", _, _}, {"erlang", _, _}]}] = ast
    end

    test "root line with mixed numeric, atom, and string values" do
      input = "stuff 123, :atom, \"hello\""
      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:stuff, _, _},
                 [
                   {123, _, _},
                   {:atom, _, _},
                   {"hello", _, _}
                 ]
               }
             ] = ast
    end

    test "root line with booleans and float" do
      input = "flags true, 3.14, false"
      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:flags, _, _},
                 [
                   {true, _, _},
                   {3.14, _, _},
                   {false, _, _}
                 ]
               }
             ] = ast
    end

    test "block with single key-value line" do
      input = """
      table employees {
        name John
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"employees", _, _},
                 [
                   {{:name, _, _}, {"John", _, _}}
                 ]
               }
             ] = ast
    end

    test "multiple blocks at root" do
      input = """
      table first {
        key val
      }
      table second {
        key2 val2
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"first", _, _},
                 [
                   {{:key, _, _}, {"val", _, _}}
                 ]
               },
               {
                 {:table, _, _},
                 {"second", _, _},
                 [
                   {{:key2, _, _}, {"val2", _, _}}
                 ]
               }
             ] = ast
    end

    test "block with multiple lines" do
      input = """
      table employees {
        name John
        dept HR
        active true
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"employees", _, _},
                 [
                   {{:name, _, _}, {"John", _, _}},
                   {{:dept, _, _}, {"HR", _, _}},
                   {{:active, _, _}, {true, _, _}}
                 ]
               }
             ] = ast
    end

    test "nested block (one level)" do
      input = """
      table employees {
        info personal {
          phone "555-1234"
        }
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"employees", _, _},
                 [
                   {
                     {:info, _, _},
                     {"personal", _, _},
                     [
                       {{:phone, _, _}, {"555-1234", _, _}}
                     ]
                   }
                 ]
               }
             ] = ast
    end

    test "nested block (multiple levels)" do
      input = """
      root top {
        level1 middle {
          level2 inner {
            data :ok
          }
        }
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:root, _, _},
                 {"top", _, _},
                 [
                   {
                     {:level1, _, _},
                     {"middle", _, _},
                     [
                       {
                         {:level2, _, _},
                         {"inner", _, _},
                         [
                           {{:data, _, _}, {:ok, _, _}}
                         ]
                       }
                     ]
                   }
                 ]
               }
             ] = ast
    end

    test "block with multiple attributes (unquoted and colon-atom)" do
      input = """
      data foo, :bar, baz {
        key1 42
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:data, _, _},
                 {["foo", :bar, "baz"], _, _},
                 [
                   {{:key1, _, _}, {42, _, _}}
                 ]
               }
             ] = ast
    end

    test "block with multiple attributes (numbers, booleans, strings)" do
      input = """
      config 1, true, "extra" {
        setting "some_value"
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:config, _, _},
                 {[1, true, "extra"], _, _},
                 [
                   {{:setting, _, _}, {"some_value", _, _}}
                 ]
               }
             ] = ast
    end

    test "block with zero attributes (no name) and multiple lines" do
      input = """
      section {
        title "My Section"
        active true
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:section, _, _},
                 {"", _, _},
                 [
                   {{:title, _, _}, {"My Section", _, _}},
                   {{:active, _, _}, {true, _, _}}
                 ]
               }
             ] = ast
    end

    test "multiple no-attr blocks at root" do
      input = """
      table {
        name "first"
      }
      section {
        name "second"
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"", _, _},
                 [
                   {{:name, _, _}, {"first", _, _}}
                 ]
               },
               {
                 {:section, _, _},
                 {"", _, _},
                 [
                   {{:name, _, _}, {"second", _, _}}
                 ]
               }
             ] = ast
    end

    test "inline comment after root key-value" do
      input = "foo bar # this is a comment"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"bar", _, _}}] = ast
    end

    test "inline comment in a block line" do
      input = """
      table employees {
        name John # last name unknown
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"employees", _, _},
                 [
                   {{:name, _, _}, {"John", _, _}}
                 ]
               }
             ] = ast
    end

    test "full line comment before root statement" do
      input = """
      # This is a comment line
      foo bar
      """

      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"bar", _, _}}] = ast
    end

    test "full line comment inside block" do
      input = """
      table test {
        # This line is commented out
        key val
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"test", _, _},
                 [
                   {{:key, _, _}, {"val", _, _}}
                 ]
               }
             ] = ast
    end

    test "comment next to block opening" do
      input = """
      table test { # comment
        key val
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"test", _, _},
                 [
                   {{:key, _, _}, {"val", _, _}}
                 ]
               }
             ] = ast
    end

    test "multiple comment lines and trailing whitespace" do
      input = """
      # comment 1

      # comment 2
      foo bar  # inline comment
      """

      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"bar", _, _}}] = ast
    end

    test "root key-value with # in quoted string" do
      input = ~S|foo "hello #world"|
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"hello #world", _, _}}] = ast
    end

    test "comment after float in root" do
      input = "pi 3.1415 # approximate\n"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:pi, _, _}, {3.1415, _, _}}] = ast
    end

    test "comment consumes remainder of line even if text follows" do
      input = "foo hello#123"
      assert {:ok, ast} = Parser.parse(input)
      assert [{{:foo, _, _}, {"hello", _, _}}] = ast
    end

    test "triple backtick string at root level (multi-line)" do
      input = """
      description ```
      Line 1
      Line 2
      ```
      """

      assert {:ok, [{{:description, _, _}, {text, _, _}}]} = Parser.parse(input)
      assert text =~ "Line 1"
      assert text =~ "Line 2"
    end

    test "triple backtick string at root level with escaped backtick" do
      input = """
      info ```
      Some text
      \\` -> should become a real backtick here: `
      End
      ```
      """

      assert {:ok, [{{:info, _, _}, {txt, _, _}}]} = Parser.parse(input)
      assert txt =~ "Some text"
      assert txt =~ "should become a real backtick here: `"
    end

    test "triple backtick string in a block (multi-line + normal lines)" do
      input = """
      section intro {
        title "Welcome!"
        body ```
        This is a multi-line block.
        We can have blank lines, too.
        ```
      }
      """

      assert {:ok,
              [
                {
                  {:section, _, _},
                  {"intro", _, _},
                  [
                    {{:title, _, _}, {"Welcome!", _, _}},
                    {{:body, _, _}, {body_text, _, _}}
                  ]
                }
              ]} = Parser.parse(input)

      assert body_text =~ "multi-line block"
    end

    test "triple backtick empty string" do
      input = "notes ``````"
      assert {:ok, [{{:notes, _, _}, {"", _, _}}]} = Parser.parse(input)
    end

    test "triple backtick with no newline inside" do
      input = "greeting ```Hello world```"
      assert {:ok, [{{:greeting, _, _}, {"Hello world", _, _}}]} = Parser.parse(input)
    end

    test "mixed triple backtick and normal lines in a block" do
      input = """
      page front {
        subtitle short
        paragraph ```
        Multi-line text
        with backtick \\`
        and more
        ```
        footer "The End"
      }
      """

      assert {:ok,
              [
                {
                  {:page, _, _},
                  {"front", _, _},
                  [
                    {{:subtitle, _, _}, {"short", _, _}},
                    {{:paragraph, _, _}, {ptext, _, _}},
                    {{:footer, _, _}, {"The End", _, _}}
                  ]
                }
              ]} = Parser.parse(input)

      assert ptext =~ "Multi-line text"
      assert ptext =~ "with backtick"
    end

    test "carriage return (\\r) with no \\n" do
      input = "foo bar\rtable staff { name John }"
      assert {:ok, ast} = Parser.parse(input)

      assert [
               {{:foo, 1, 1}, {"bar", 1, 5}},
               {
                 {:table, 2, 1},
                 {"staff", 2, 13},
                 [
                   {{:name, 2, 15}, {"John", 2, 20}}
                 ]
               }
             ] = ast
    end

    test "triple-backtick with Windows line break (\\r\\n)" do
      input = "message ```\r\nlineA\r\nlineB\r\n```\n"
      assert {:ok, [{{:message, 1, 1}, {txt, _, _}}]} = Parser.parse(input)
      assert txt =~ "lineA\nlineB\n"
    end

    test "triple backtick with carriage-return inside content" do
      input = "note ```Line1\rLine2\r```"
      assert {:ok, [{{:note, 1, 1}, {txt, _, _}}]} = Parser.parse(input)
      assert txt == "Line1\nLine2\n"
    end

    test "double-quoted string with escaped quote" do
      input = ~S|foo "hello \"escaped\" world"|
      assert {:ok, [{{:foo, _, _}, {text, _, _}}]} = Parser.parse(input)
      assert text == ~S|hello "escaped" world|
    end

    test "root key-value with no values => nil" do
      input = "loner\n"
      assert {:ok, [{{:loner, _, _}, {nil, _, _}}]} = Parser.parse(input)
    end

    # Additional positive edge-case
    test "block statement with extra blank line" do
      input = """
      table staff {

        name John
      }
      """

      assert {:ok, ast} = Parser.parse(input)

      assert [
               {
                 {:table, _, _},
                 {"staff", _, _},
                 [
                   {{:name, _, _}, {"John", _, _}}
                 ]
               }
             ] = ast
    end

    test "empty block with no lines" do
      input = "empty_block {}"
      assert {:ok, [{{:empty_block, line, col}, {"", _, _}, []}]} = Parser.parse(input)
      assert line == 1
      assert col == 1
    end

    test "double-quoted empty string" do
      input = ~S|foo ""|
      assert {:ok, [{{:foo, _, _}, {"", _, _}}]} = Parser.parse(input)
    end

    test "unquoted string with special characters" do
      # Note we avoid ` # { } " , \t \r \n
      # Those are delimiters or comment triggers. Let's try something like &^%$!
      input = "misc &^%$!"
      assert {:ok, [{{:misc, _, _}, {"&^%$!", _, _}}]} = Parser.parse(input)
    end

    test "root key-value with zero" do
      input = "zero 0"
      assert {:ok, [{{:zero, _, _}, {0, _, _}}]} = Parser.parse(input)
    end

    test "root key-value with multiple zeros" do
      input = "count 000"
      # This should parse as integer 0
      assert {:ok, [{{:count, _, _}, {0, _, _}}]} = Parser.parse(input)
    end

    test "root key-value with negative integer recognized as unquoted string" do
      input = "neg -123"
      assert {:ok, [{{:neg, _, _}, {"-123", _, _}}]} = Parser.parse(input)
    end

    test "block with multiple comma-separated values in a line" do
      input = """
      multi_block {
        line_a val1, val2, val3
      }
      """

      assert {:ok,
              [
                {
                  {:multi_block, _, _},
                  {"", _, _},
                  [
                    {{:line_a, _, _}, [{"val1", _, _}, {"val2", _, _}, {"val3", _, _}]}
                  ]
                }
              ]} = Parser.parse(input)
    end

    test "block with triple-backtick as name attribute" do
      input = """
      stuff ```my stuff``` {
        key val
      }
      """

      assert {:ok,
              [
                {
                  {:stuff, _, _},
                  {"my stuff", _, _},
                  [
                    {{:key, _, _}, {"val", _, _}}
                  ]
                }
              ]} = Parser.parse(input)
    end

    test "single-quoted string at root level" do
      input = "message 'hello world'"
      assert {:ok, [{{:message, _, _}, {"hello world", _, _}}]} = Parser.parse(input)
    end

    test "single-quoted string with escaped quote" do
      input = "note 'It\\'s important'"
      assert {:ok, [{{:note, _, _}, {"It's important", _, _}}]} = Parser.parse(input)
    end

    test "empty single-quoted string" do
      input = "empty ''"
      assert {:ok, [{{:empty, _, _}, {"", _, _}}]} = Parser.parse(input)
    end

    test "root line with multiple single-quoted values" do
      input = "items 'one', 'two'"
      assert {:ok, [{{:items, _, _}, [{"one", _, _}, {"two", _, _}]}]} = Parser.parse(input)
    end

    test "root line with mixed quoted string types" do
      input = "mixed \"double\", 'single', ```triple```"

      assert {:ok, [{{:mixed, _, _}, [{"double", _, _}, {"single", _, _}, {"triple", _, _}]}]} =
               Parser.parse(input)
    end

    test "block with single-quoted string value" do
      input = """
      block {
        setting 'enabled'
      }
      """

      assert {:ok,
              [
                {
                  {:block, _, _},
                  {"", _, _},
                  [
                    {{:setting, _, _}, {"enabled", _, _}}
                  ]
                }
              ]} = Parser.parse(input)
    end

    test "block with single-quoted string attribute" do
      input = "block 'my name' { key val }"

      assert {:ok,
              [
                {
                  {:block, _, _},
                  {"my name", _, _},
                  [
                    {{:key, _, _}, {"val", _, _}}
                  ]
                }
              ]} = Parser.parse(input)
    end

    test "backtick-quoted string at root level" do
      input = "message `hello world`"
      assert {:ok, [{{:message, _, _}, {"hello world", _, _}}]} = Parser.parse(input)
    end

    test "backtick-quoted string with escaped backtick" do
      input = "note `It\\`s important`"
      assert {:ok, [{{:note, _, _}, {"It`s important", _, _}}]} = Parser.parse(input)
    end

    test "empty backtick-quoted string" do
      input = "empty ``"
      assert {:ok, [{{:empty, _, _}, {"", _, _}}]} = Parser.parse(input)
    end

    test "root line with multiple backtick-quoted values" do
      input = "items `one`, `two`"
      assert {:ok, [{{:items, _, _}, [{"one", _, _}, {"two", _, _}]}]} = Parser.parse(input)
    end

    test "root line with mixed quoted string types including backticks" do
      input = "mixed \"double\", 'single', ```triple```, `backtick`"

      assert {:ok,
              [
                {
                  {:mixed, _, _},
                  [{"double", _, _}, {"single", _, _}, {"triple", _, _}, {"backtick", _, _}]
                }
              ]} =
               Parser.parse(input)
    end

    test "block with backtick-quoted string value" do
      input = """
      block {
        setting `enabled`
      }
      """

      assert {:ok,
              [
                {
                  {:block, _, _},
                  {"", _, _},
                  [
                    {{:setting, _, _}, {"enabled", _, _}}
                  ]
                }
              ]} = Parser.parse(input)
    end

    test "block with backtick-quoted string attribute" do
      input = "block `my name` { key val }"

      assert {:ok,
              [
                {
                  {:block, _, _},
                  {"my name", _, _},
                  [
                    {{:key, _, _}, {"val", _, _}}
                  ]
                }
              ]} = Parser.parse(input)
    end
  end

  #
  # Negative / Error Tests
  #
  describe "Negative Parsing Scenarios" do
    test "unexpected character at root" do
      input = "\u0000"

      # The tokenizer converts unknown chars to unquoted strings unless specific fallback is added.
      # But current implementation treats them as part of unquoted string if not separators.
      # So `\u0000` becomes an unquoted string token.
      # It will try to parse `\u0000` as a name. If it doesn't match name regex, it fails.
      # \u0000 won't match name regex.
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unexpected unquoted string"
    end

    test "invalid atom format" do
      input = "key :InvalidAtom"
      # Regex `^[a-z][a-z0-9_]*$` won't match `InvalidAtom` (starts with uppercase)?
      # Wait, consume_name_chars accepts a..z, 0..9, _.
      # It stops at uppercase I. So parsing `:` then stuck.
      # Actually `consume_name_chars` stops at first non-matching char.
      # So it consumes empty string if first char is uppercase.
      assert {:error, {"invalid or empty atom", _, _}} = Parser.parse(input)
    end

    test "unexpected `}` at root" do
      input = "}"
      assert {:error, {"unexpected `}` at root level", _, _}} = Parser.parse(input)
    end

    # Removed invalid test "missing block name" since "block (" is valid KV.

    test "unterminated block (EOF)" do
      input = "table {"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unterminated block"
    end

    test "unterminated block (nested)" do
      input = """
      outer {
        inner {
      }
      """

      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unterminated block"
    end

    test "expected comma between values" do
      input = "nums 1 2"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "expected comma"
    end

    test "expected comma between values (mixed)" do
      input = "vals true false"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "expected comma"
    end

    test "expected comma in block name list" do
      input = "table 1 2 {"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "expected comma"
    end

    test "unterminated double-quoted string" do
      input = "foo \"bar"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unterminated double-quoted string"
    end

    test "unterminated triple-backtick string" do
      input = "foo ```bar"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unterminated triple-backtick string"
    end

    test "newline in double-quoted string" do
      input = "foo \"line1\nline2\""
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unexpected newline"
    end

    test "invalid key name (starts with number)" do
      input = "1foo bar"
      # `1foo` -> number `1`, unquoted `foo` (if split?)
      # `1foo` is unquoted string. `classify_unquoted` checks integers/floats.
      # `1foo` is `:unquoted_string`.
      # `consume_name_token` checks regex `^[a-z]...`
      # `1foo` fails regex.
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unexpected unquoted string"
    end

    test "unexpected token structure in block statement" do
      input = """
      block {
        123 val
      }
      """

      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unexpected number"
    end

    test "unterminated single-quoted string" do
      input = "foo 'bar"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unterminated single-quoted string"
    end

    test "newline in single-quoted string" do
      input = "foo 'line1\nline2'"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unexpected newline"
    end

    test "unterminated backtick-quoted string" do
      input = "foo `bar"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unterminated backtick-quoted string"
    end

    test "newline in backtick-quoted string" do
      input = "foo `line1\nline2`"
      assert {:error, {msg, _, _}} = Parser.parse(input)
      assert msg =~ "unexpected newline"
    end
  end
end
