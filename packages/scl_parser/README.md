# SCL Parser

A line-aware SCL (Simple Configuration Language) parser for Elixir.

## Features

- **Line/Column Tracking:** The AST includes line and column information for every key, block, and value.
- **Strict Parsing:** Enforces comma separation for values.
- **Typed Values:** Supports integers, floats, booleans, atoms, and various string formats (quoted, unquoted, triple-backtick, etc.).
- **Nested Blocks:** Supports arbitrarily nested blocks.
- **Expression Parsing:** Includes an `ExpressionParser` for parsing SCL expressions (e.g., `$func() |> $next()`).
- **Expression Parsing:** Includes an `ExpressionParser` for parsing SCL expressions (e.g., `$func() |> $next()`).

## Installation

Add `scl_parser` to your list of dependencies in `mix.exs`:

```elixir
def deps do
  [
    {:scl_parser, "~> 1.0.0"}
  ]
end
```

## Usage (Library)

```elixir
# Basic Parsing
{:ok, ast} = SCLParser.parse("key value")

# Expression Parsing
{:ok, expr_ast} = SCLParser.ExpressionParser.parse("$var('foo')")
```

The AST format is a list of tuples:
- Key-Value: `{{:key, line, col}, {value, line, col}}`
- Block: `{{:block_name, line, col}, {name_param, line, col}, [children...]}`

## Usage (CLI)

For command-line usage, please use the standalone **[SCL Parser CLI](../../tools/scl_parser_cli/)** tool.

## Development

```bash
# Run tests
mix test

# Run code analysis
mix credo --strict
```

## License

Apache 2.0
