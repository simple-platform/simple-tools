# SCL Parser CLI

A command-line interface for parsing SCL (Simple Configuration Language) files and outputting JSON.

## Installation

Download the appropriate binary for your platform from the [releases page](https://github.com/simple-platform/simple-tools/releases).

### macOS
```bash
curl -L -o scl-parser https://github.com/simple-platform/simple-tools/releases/latest/download/scl-parser-macos
chmod +x scl-parser
```

### Linux
```bash
curl -L -o scl-parser https://github.com/simple-platform/simple-tools/releases/latest/download/scl-parser-linux
chmod +x scl-parser
```

### Windows
Download `scl-parser-windows.exe` from the releases page.

## Usage

```bash
scl-parser <file.scl>
```

### Example

```bash
$ cat example.scl
name "my-app"
version "1.0.0"

table users {
  id integer
  name string
}

$ scl-parser example.scl
[{"type":"kv","key":"name","value":"my-app",...},...]
```

## Output Format

The CLI outputs JSON with the following structure:

- **Key-Value pairs**: `{"type": "kv", "key": "...", "value": "...", "key_loc": {...}, "value_loc": {...}}`
- **Blocks**: `{"type": "block", "key": "...", "name": "...", "children": [...], ...}`

## Development

### Prerequisites

- Elixir 1.18+
- OTP 27+
- Zig 0.15.2 (for Burrito builds)

### Building

```bash
cd tools/scl_parser_cli
mix deps.get
MIX_ENV=prod mix release --overwrite
```

Binaries will be output to `burrito_out/`.

## License

Apache-2.0
